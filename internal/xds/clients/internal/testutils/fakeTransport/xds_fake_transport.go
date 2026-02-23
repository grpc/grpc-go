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

// Package faketransport provides a fake implementation of the xDS client's
// transport layer. It implements the clients.TransportBuilder,
// clients.Transport and clients.Stream interfaces for testing purposes.
package faketransport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/protobuf/proto"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// This compile time check ensures that Builder implements clients.TransportBuilder.
var _ clients.TransportBuilder = &Builder{}

// Builder implements clients.TransportBuilder.
type Builder struct {
	mu               sync.Mutex
	ActiveTransports map[string]*FakeTransport // Tracks created transports for the fuzzer to interact with.
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{
		ActiveTransports: make(map[string]*FakeTransport),
	}
}

// Build creates a new Transport for the given server identifier.
func (b *Builder) Build(serverIdentifier clients.ServerIdentifier) (clients.Transport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if at, ok := b.ActiveTransports[serverIdentifier.ServerURI]; ok {
		return at, nil
	}

	ft := newFakeTransport()
	b.ActiveTransports[serverIdentifier.ServerURI] = ft
	return ft, nil
}

// GetTransport returns the active transport for a given server URI.
func (b *Builder) GetTransport(serverURI string) *FakeTransport {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			b.mu.Lock()
			if t, ok := b.ActiveTransports[serverURI]; ok && t.GetAdsStream() != nil {
				b.mu.Unlock()
				return t
			}
			b.mu.Unlock()
		}
	}
}

// This compile time check ensures that FakeTransport implements clients.Transport.
var _ clients.Transport = &FakeTransport{}

// FakeTransport implements clients.Transport.
type FakeTransport struct {
	mu              sync.Mutex
	activeAdsStream *FakeStream
	fakeServer      *FakeServerHandle
	closed          bool
}

func newFakeTransport() *FakeTransport {
	return &FakeTransport{}
}

// GetServerHandle returns a fake serverhandle for testing.
func (t *FakeTransport) GetServerHandle() *FakeServerHandle {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fakeServer
}

// GetAdsStream returns the active ADS stream for testing.
func (t *FakeTransport) GetAdsStream() *FakeStream {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activeAdsStream
}

// NewStream creates a new fake stream to the server.
func (t *FakeTransport) NewStream(ctx context.Context, _ string) (clients.Stream, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil, fmt.Errorf("transport is closed")
	}

	fs := newFakeStream(ctx)
	t.activeAdsStream, t.fakeServer = fs, &FakeServerHandle{fs: fs}
	return fs, nil
}

// Close closes the fake stream.
func (t *FakeTransport) Close() {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	if stream := t.GetAdsStream(); stream != nil {
		stream.Close()
	}
}

// This compile time check ensures that FakeStream implements clients.Stream.
var _ clients.Stream = &FakeStream{}

// FakeStream implements clients.Stream.
type FakeStream struct {
	ctx    context.Context
	cancel context.CancelFunc

	reqBuffer  *buffer.Unbounded
	respBuffer *buffer.Unbounded
}

func newFakeStream(ctx context.Context) *FakeStream {
	c, cancel := context.WithCancel(ctx)
	return &FakeStream{
		ctx:        c,
		cancel:     cancel,
		reqBuffer:  buffer.NewUnbounded(),
		respBuffer: buffer.NewUnbounded(),
	}
}

// Send sends the provided message on the stream. It puts the request into the
// reqBuffer for consumption.
func (s *FakeStream) Send(data []byte) error {
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	return s.reqBuffer.Put(data)
}

// Recv blocks until the next message is received on the stream. It blocks until
// a response is available in the respBuffer or the context is canceled.
func (s *FakeStream) Recv() ([]byte, error) {
	select {
	case data := <-s.respBuffer.Get():
		s.respBuffer.Load()
		return data.([]byte), nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// Close closes the fake stream.
func (s *FakeStream) Close() {
	s.respBuffer.Close()
	s.reqBuffer.Close()
	s.cancel()
}

// FakeServerHandle provides the server-side send/recv methods to interact with
// the fakeStream.
type FakeServerHandle struct {
	fs *FakeStream
}

// Recv reads the next request from the reqBuffer. It blocks until a
// request is available or the context expires. It returns an error if the
// context expires or if the request cannot be unmarshaled.
func (h *FakeServerHandle) Recv() (*v3discoverypb.DiscoveryRequest, error) {
	select {
	case data := <-h.fs.reqBuffer.Get():
		h.fs.reqBuffer.Load()
		req := &v3discoverypb.DiscoveryRequest{}
		if err := proto.Unmarshal(data.([]byte), req); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %v", err)
		}
		return req, nil
	case <-h.fs.ctx.Done():
		return nil, h.fs.ctx.Err()
	}
}

// Send simulates a server response. It marshals the provided
// DiscoveryResponse, puts it in the respBuffer to notify that a response
// is available for the client to Recv.
func (h *FakeServerHandle) Send(resp *v3discoverypb.DiscoveryResponse) error {
	data, err := proto.Marshal(resp)
	if err != nil {
		return err
	}
	return h.fs.respBuffer.Put(data)
}
