/*
 *
 * Copyright 2026 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package transport

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/stats"
)

func makeSettingsFrame(t *testing.T, settings ...http2.Setting) *http2.SettingsFrame {
	t.Helper()
	var buf bytes.Buffer
	fr := http2.NewFramer(&buf, &buf)
	if err := fr.WriteSettings(settings...); err != nil {
		t.Fatalf("WriteSettings failed: %v", err)
	}
	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}
	sf, ok := f.(*http2.SettingsFrame)
	if !ok {
		t.Fatalf("ReadFrame returned %T, want *http2.SettingsFrame", f)
	}
	return sf
}

// TestNewStreamFailsFastDuringCloseWithClosedQuotaChan verifies that a
// NewStream caller parked on streamsQuotaAvailable returns ErrConnClosing
// promptly when Close() starts (even if writing the outgoing GOAWAY stalls and
// t.cancel() has not run yet) and does not busy-spin when a concurrent SETTINGS
// frame closes the old streamsQuotaAvailable channel.
func (s) TestNewStreamFailsFastDuringCloseWithClosedQuotaChan(t *testing.T) {
	origGoAwayLoopyTimeout := goAwayLoopyWriterTimeout
	goAwayLoopyWriterTimeout = 2 * time.Second
	defer func() {
		goAwayLoopyWriterTimeout = origGoAwayLoopyTimeout
	}()

	const initMaxStreams = 1
	const newMaxStreams = 10

	server := setUpServerOnly(t, 0, &ServerConfig{
		BufferPool: mem.DefaultBufferPool(),
		MaxStreams: initMaxStreams,
	}, normal)
	defer server.stop()

	isGreetingDone := &atomic.Bool{}
	hangConn := make(chan struct{})
	var closeHangOnce sync.Once
	unblockConn := func() {
		closeHangOnce.Do(func() { close(hangConn) })
	}
	defer unblockConn()

	dialer := func(_ context.Context, addr string) (net.Conn, error) {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return nil, err
		}
		return &hangingConn{Conn: conn, hangConn: hangConn, startHanging: isGreetingDone}, nil
	}
	copts := ConnectOptions{
		Dialer:     dialer,
		BufferPool: mem.DefaultBufferPool(),
	}
	copts.ChannelzParent = channelzSubChannel(t)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	rawCT, err := NewHTTP2Client(ctx, ctx, resolver.Address{Addr: "localhost:" + server.port}, copts, func(GoAwayInfo) {})
	if err != nil {
		t.Fatalf("NewHTTP2Client failed: %v", err)
	}
	ct := rawCT.(*http2Client)

	// Occupy the single stream slot so streamQuota becomes 0.
	if _, err := ct.NewStream(ctx, &CallHdr{}, nil); err != nil {
		t.Fatalf("first NewStream failed: %v", err)
	}

	// Launch a second NewStream that blocks in select holding the initial
	// streamsQuotaAvailable channel.
	newStreamDone := make(chan error, 1)
	go func() {
		_, nerr := ct.NewStream(ctx, &CallHdr{}, nil)
		newStreamDone <- nerr
	}()

	// Wait until the second NewStream is registered as waiting on quota.
	waitDeadline := time.Now().Add(defaultTestTimeout)
	for {
		var waiting uint32
		ct.controlBuf.executeAndPut(func() bool {
			waiting = ct.waitingStreams
			return false
		}, nil)
		if waiting == 1 {
			break
		}
		if time.Now().After(waitDeadline) {
			t.Fatal("timed out waiting for NewStream to park on stream quota")
		}
		time.Sleep(time.Millisecond)
	}

	// Stall conn writes and start Close(). Close() sets state = closing and
	// activeStreams = nil, then waits on writerDone / goAwayLoopyWriterTimeout
	// before calling t.cancel().
	isGreetingDone.Store(true)
	closeDone := make(chan struct{})
	go func() {
		defer close(closeDone)
		ct.Close(errors.New("closing while writer is stalled"))
	}()

	for ct.stateForTesting() != closing {
		if time.Now().After(waitDeadline) {
			t.Fatal("timed out waiting for transport to enter closing state")
		}
		time.Sleep(time.Millisecond)
	}

	// Deliver SETTINGS increasing MaxConcurrentStreams. handleSettings closes
	// the old streamsQuotaAvailable channel and increases streamQuota.
	sf := makeSettingsFrame(t, http2.Setting{
		ID:  http2.SettingMaxConcurrentStreams,
		Val: newMaxStreams,
	})
	ct.handleSettings(sf, false)

	// NewStream must fail fast before goAwayLoopyWriterTimeout expires instead
	// of busy-spinning on the closed old channel until t.cancel() runs.
	var newStreamErr error
	select {
	case newStreamErr = <-newStreamDone:
	case <-time.After(500 * time.Millisecond):
		unblockConn()
		<-closeDone
		<-newStreamDone
		t.Fatal("NewStream did not fail fast while Close() was waiting on stalled writer")
	}

	unblockConn()
	<-closeDone

	var nse *NewStreamError
	if !errors.As(newStreamErr, &nse) || nse.Err != ErrConnClosing || !nse.AllowTransparentRetry {
		t.Fatalf("NewStream returned %v, want NewStreamError{Err: ErrConnClosing, AllowTransparentRetry: true}", newStreamErr)
	}
}

type pauseOutHeaderStatsHandler struct {
	inOutHeader      chan struct{}
	releaseOutHeader chan struct{}
}

func (h *pauseOutHeaderStatsHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h *pauseOutHeaderStatsHandler) HandleRPC(_ context.Context, s stats.RPCStats) {
	if _, ok := s.(*stats.OutHeader); ok {
		close(h.inOutHeader)
		<-h.releaseOutHeader
	}
}

func (h *pauseOutHeaderStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *pauseOutHeaderStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

// TestMaxStreamIDConcurrentNewStreamExceedsLimit verifies that a concurrent
// NewStream cannot allocate a stream ID > MaxStreamID in the window between
// releasing t.mu and calling t.GracefulClose().
func (s) TestMaxStreamIDConcurrentNewStreamExceedsLimit(t *testing.T) {
	server, ct, cancel := setUp(t, 0, normal)
	defer cancel()
	defer ct.Close(errors.New("closed manually by test"))
	defer server.stop()

	origMaxStreamID := MaxStreamID
	MaxStreamID = 3
	defer func() {
		MaxStreamID = origMaxStreamID
	}()

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	callHdr := &CallHdr{
		Host:   "localhost",
		Method: "foo.Small",
	}

	// Stream 1 gets id = 1 (nextID becomes 3 <= MaxStreamID).
	s1, err := ct.NewStream(ctx, callHdr, nil)
	if err != nil {
		t.Fatalf("first NewStream failed: %v", err)
	}
	if s1.id != 1 {
		t.Fatalf("s1.id = %d, want 1", s1.id)
	}

	// Stream 2 gets id = 3 (nextID becomes 5 > MaxStreamID), setting
	// transportDrainRequired = true. Pause deterministically in statsHandler
	// right before NewStream calls t.GracefulClose().
	handler := &pauseOutHeaderStatsHandler{
		inOutHeader:      make(chan struct{}),
		releaseOutHeader: make(chan struct{}),
	}
	type streamRes struct {
		s   *ClientStream
		err error
	}
	s2Done := make(chan streamRes, 1)
	go func() {
		st, nerr := ct.NewStream(ctx, callHdr, handler)
		s2Done <- streamRes{s: st, err: nerr}
	}()

	select {
	case <-handler.inOutHeader:
	case <-ctx.Done():
		t.Fatal("timed out waiting for stream 2 to reach statsHandler.HandleRPC")
	}

	// While stream 2 has already exhausted MaxStreamID but has not yet called
	// GracefulClose(), a concurrent NewStream must fail fast instead of
	// allocating stream ID 5 > MaxStreamID.
	s3, err3 := ct.NewStream(ctx, callHdr, nil)

	close(handler.releaseOutHeader)
	res2 := <-s2Done
	if res2.err != nil {
		t.Fatalf("second NewStream failed: %v", res2.err)
	}
	if res2.s.id != 3 {
		t.Fatalf("s2.id = %d, want 3", res2.s.id)
	}

	if err3 == nil && s3.id > MaxStreamID {
		t.Fatalf("concurrent NewStream succeeded with stream ID %d > MaxStreamID (%d) before GracefulClose ran",
			s3.id, MaxStreamID)
	}
	var nse *NewStreamError
	if !errors.As(err3, &nse) || nse.Err != ErrConnClosing || !nse.AllowTransparentRetry {
		t.Fatalf("concurrent NewStream returned %v, want NewStreamError{Err: ErrConnClosing, AllowTransparentRetry: true}", err3)
	}
}
