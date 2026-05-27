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

package transport_test

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/stubserver"
	transportinternal "google.golang.org/grpc/internal/transport/internal"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func overrideTimeNowFunc(t *testing.T, f func() int64) {
	orig := transportinternal.TimeNowFunc
	transportinternal.TimeNowFunc = f
	t.Cleanup(func() { transportinternal.TimeNowFunc = orig })
}

func verifyResultWithDelay(ctx context.Context, f func() (bool, error)) error {
	var lastErr error
	for ; ctx.Err() == nil; <-time.After(10 * time.Millisecond) {
		var ok bool
		if ok, lastErr = f(); ok {
			return nil
		}
	}
	return fmt.Errorf("Timeout when waiting for channelz to contain expected data. Last seen error: %v", lastErr)
}

func (s) TestChannelz_ServerSocketMetricsLastMessageTimestamps(t *testing.T) {
	channelz.TurnOn()
	defer internal.ChannelzTurnOffForTesting()

	// Override timeNowFunc for the duration of the test.
	var curTime atomic.Int64
	overrideTimeNowFunc(t, func() int64 {
		curTime.Add(1)
		return curTime.Load()
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a stub server that echoes back messages sent by the client. This
	// allows us to test both LastMessageReceivedTimestamp and
	// LastMessageSentTimestamp on the server side.
	var blockBeforeSend = make(chan struct{}, 1)
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				req, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				select {
				case <-blockBeforeSend:
				case <-ctx.Done():
					return ctx.Err()
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: req.GetPayload()}); err != nil {
					return err
				}
			}
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer ss.Stop()

	// Wait for the server to be up and registered with channelz.
	var svrID int64
	if err := verifyResultWithDelay(ctx, func() (bool, error) {
		ssrvs, _ := channelz.GetServers(0, 0)
		for _, s := range ssrvs {
			for id := range s.ListenSockets() {
				skt := channelz.GetSocket(id)
				if skt != nil && skt.LocalAddr.String() == ss.Address {
					svrID = s.ID
					return true, nil
				}
			}
		}
		return false, fmt.Errorf("could not find server with address %s in channelz", ss.Address)
	}); err != nil {
		t.Fatal(err)
	}

	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	// Wait for the message from the client to be received on the server and
	// timestamp updated.
	wantTimestamp := int64(1)
	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send(_) = %v, want <nil>", err)
	}
	if err := verifyResultWithDelay(ctx, func() (bool, error) {
		ns, _ := channelz.GetServerSockets(svrID, 0, 0)
		if len(ns) == 0 {
			return false, fmt.Errorf("no server sockets found")
		}
		sktData := &ns[0].SocketMetrics
		if sktData.MessagesReceived.Load() < 1 {
			return false, fmt.Errorf("server has not received any messages yet")
		}
		if gotTimestamp := sktData.LastMessageReceivedTimestamp.Load(); gotTimestamp != wantTimestamp {
			return false, fmt.Errorf("LastMessageReceivedTimestamp is %d, want %d", gotTimestamp, wantTimestamp)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	blockBeforeSend <- struct{}{}

	// Wait for the message to be sent by the server and timestamp updated.
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("stream.Recv() = %v, want <nil>", err)
	}
	wantTimestamp = int64(2)
	if err := verifyResultWithDelay(ctx, func() (bool, error) {
		ns, _ := channelz.GetServerSockets(svrID, 0, 0)
		sktData := &ns[0].SocketMetrics
		if sktData.MessagesSent.Load() == 0 {
			return false, fmt.Errorf("server has not sent any messages yet")
		}
		if gotTimestamp := sktData.LastMessageSentTimestamp.Load(); gotTimestamp != wantTimestamp {
			return false, fmt.Errorf("LastMessageSentTimestamp = %d, want = %d", gotTimestamp, wantTimestamp)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	// Send another message to verify that LastMessageReceivedTimestamp is
	// updated again.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send(_) = %v, want <nil>", err)
	}
	wantTimestamp = int64(3)
	if err := verifyResultWithDelay(ctx, func() (bool, error) {
		ns, _ := channelz.GetServerSockets(svrID, 0, 0)
		sktData := &ns[0].SocketMetrics
		if sktData.MessagesReceived.Load() < 2 {
			return false, fmt.Errorf("server has not received second message yet")
		}
		if gotTimestamp := sktData.LastMessageReceivedTimestamp.Load(); gotTimestamp != wantTimestamp {
			return false, fmt.Errorf("LastMessageReceivedTimestamp is %d, want %d", gotTimestamp, wantTimestamp)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
}
