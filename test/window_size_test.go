/*
 *
 * Copyright 2024 gRPC authors.
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

package test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const initialWindowSize = 65535

// Test verifies the functionality of server options that specify the initial
// window sizes for the server. It ensures BDP estimation is enabled when the
// non-static options are used, and that it its disabled when the static options
// are used.
//
// The test sends large payloads from the client and checks if the server's flow
// control window grows when BDP estimation is expected to be enabled.
func (s) TestInitialWindowSize_Server(t *testing.T) {
	channelz.TurnOn()

	tests := []struct {
		name         string
		serverOption grpc.ServerOption
		wantGrowth   bool
	}{
		{
			name:         "InitialWindowSize",
			serverOption: grpc.InitialWindowSize(initialWindowSize),
			wantGrowth:   true,
		},
		{
			name:         "InitialStreamWindowSize",
			serverOption: grpc.InitialStreamWindowSize(initialWindowSize),
			wantGrowth:   true,
		},
		{
			name:         "InitialConnWindowSize",
			serverOption: grpc.InitialConnWindowSize(initialWindowSize),
			wantGrowth:   true,
		},
		{
			name:         "StaticStreamWindowSize",
			serverOption: grpc.StaticStreamWindowSize(initialWindowSize),
			wantGrowth:   false,
		},
		{
			name:         "StaticConnWindowSize",
			serverOption: grpc.StaticConnWindowSize(initialWindowSize),
			wantGrowth:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ss := &stubserver.StubServer{
				FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
					for {
						in, err := stream.Recv()
						if err == io.EOF {
							return nil
						}
						if err != nil {
							return err
						}
						for _, p := range in.GetResponseParameters() {
							payload, err := newPayload(in.GetResponseType(), p.GetSize())
							if err != nil {
								return err
							}
							if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: payload}); err != nil {
								return err
							}
						}
					}
				},
			}
			if err := ss.Start([]grpc.ServerOption{tc.serverOption}); err != nil {
				t.Fatalf("Error starting stub server: %v", err)
			}
			defer ss.Stop()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			stream, err := ss.Client.FullDuplexCall(ctx)
			if err != nil {
				t.Fatalf("Failed to start FullDuplexCall: %v", err)
			}

			// Start sending large payloads from the client in a separate
			// goroutine to trigger flow control behavior on the server.
			doneCh := make(chan struct{})
			errCh := make(chan error, 1)
			go func() {
				req := &testpb.StreamingOutputCallRequest{
					Payload: &testpb.Payload{
						Type: testpb.PayloadType_COMPRESSABLE,
						Body: make([]byte, 1024*1024), // Large request payload to trigger flow control behavior
					},
					ResponseParameters: []*testpb.ResponseParameters{{Size: 1}}, // Small response just to acknowledge
				}
				for {
					select {
					case <-doneCh:
						errCh <- nil
						return
					default:
					}

					if err := stream.Send(req); err != nil {
						errCh <- fmt.Errorf("Send() failed: %v", err)
						return
					}
					if _, err := stream.Recv(); err != nil {
						errCh <- fmt.Errorf("Recv() failed: %v", err)
						return
					}
				}
			}()

			// Use channelz to find the server socket and monitor its flow
			// control window.
			servers, _ := channelz.GetServers(0, 0)
			if len(servers) != 1 {
				t.Fatalf("Expected 1 server, got %d", len(servers))
			}
			sockets, _ := channelz.GetServerSockets(servers[0].ID, 0, 0)
			if len(sockets) != 1 {
				t.Fatalf("Expected 1 socket, got %d", len(sockets))
			}

			// For the static case, we use a shorter deadline to verify no
			// growth happens.
			if !tc.wantGrowth {
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}

			// Verify if the window grows as expected based on the server option
			// used.
			if err := verifyWindowGrowthForSocket(ctx, sockets[0], tc.wantGrowth, doneCh); err != nil {
				t.Fatal(err)
			}

			if err := <-errCh; err != nil {
				t.Fatalf("Error sending large payloads: %v", err)
			}
		})
	}
}

// Test verifies the functionality of dial options that specify the initial
// window sizes for the client. It ensures BDP estimation is enabled when the
// non-static options are used, and that it its disabled when the static options
// are used.
//
// The test sends large payloads from the server and checks if the client's flow
// control window grows when BDP estimation is expected to be enabled.
func (s) TestInitialWindowSize_Client(t *testing.T) {
	channelz.TurnOn()

	tests := []struct {
		name       string
		dialOption grpc.DialOption
		wantGrowth bool
	}{
		{
			name:       "InitialWindowSize",
			dialOption: grpc.WithInitialWindowSize(initialWindowSize),
			wantGrowth: true,
		},
		{
			name:       "InitialStreamWindowSize",
			dialOption: grpc.WithInitialStreamWindowSize(initialWindowSize),
			wantGrowth: true,
		},
		{
			name:       "InitialConnWindowSize",
			dialOption: grpc.WithInitialConnWindowSize(initialWindowSize),
			wantGrowth: true,
		},
		{
			name:       "StaticStreamWindowSize",
			dialOption: grpc.WithStaticStreamWindowSize(initialWindowSize),
			wantGrowth: false,
		},
		{
			name:       "StaticConnWindowSize",
			dialOption: grpc.WithStaticConnWindowSize(initialWindowSize),
			wantGrowth: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ss := &stubserver.StubServer{
				FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
					in, err := stream.Recv()
					if err != nil {
						return err
					}
					// Server sends large payloads back to trigger client flow control.
					payload, err := newPayload(in.GetResponseType(), 1024*1024)
					if err != nil {
						return err
					}
					for {
						if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: payload}); err != nil {
							return err
						}
					}
				},
			}
			if err := ss.Start(nil, tc.dialOption); err != nil {
				t.Fatalf("Error starting stub server: %v", err)
			}
			defer ss.Stop()

			// Create a base context for the test.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			stream, err := ss.Client.FullDuplexCall(ctx)
			if err != nil {
				t.Fatalf("Failed to start FullDuplexCall: %v", err)
			}

			// Client sends a small message to start the stream.
			if err := stream.Send(&testpb.StreamingOutputCallRequest{
				ResponseType: testpb.PayloadType_COMPRESSABLE,
			}); err != nil {
				t.Fatalf("Send() failed: %v", err)
			}

			// Goroutine to keep receiving large payloads from server.
			doneCh := make(chan struct{})
			errCh := make(chan error, 1)
			go func() {
				for {
					if _, err := stream.Recv(); err != nil {
						errCh <- err
						return
					}
					select {
					case <-doneCh:
						errCh <- nil
						return
					default:
					}
				}
			}()

			// Find the client socket. We need to wait for the subchannel and
			// socket to be created and registered with channelz.
			var sockets []*channelz.Socket
			for {
				tChans, _ := channelz.GetTopChannels(0, 0)
				if len(tChans) == 1 {
					c := channelz.GetChannel(tChans[0].ID)
					for scID := range c.SubChans() {
						sc := channelz.GetSubChannel(scID)
						if skts := sc.Sockets(); len(skts) > 0 {
							for sktID := range skts {
								sockets = append(sockets, channelz.GetSocket(sktID))
							}
						}
					}
				}
				if len(sockets) > 0 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("Timed out waiting for sockets: %v", ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}

			if len(sockets) != 1 {
				t.Fatalf("Expected 1 socket, got %d", len(sockets))
			}

			// For the static case, we use a shorter deadline to verify no
			// growth happens.
			if !tc.wantGrowth {
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}

			// Verify if the window grows as expected based on the dial option
			// used.
			if err := verifyWindowGrowthForSocket(ctx, sockets[0], tc.wantGrowth, doneCh); err != nil {
				t.Fatal(err)
			}

			if err := <-errCh; err != nil {
				t.Fatalf("Error receiving large payloads: %v", err)
			}
		})
	}
}

// verifyWindowGrowthForSocket checks if the flow control window for the given
// socket grows beyond the initial window size within the context deadline. If
// wantGrowth is true, it expects the window to grow; if false, it expects the
// window to stay at the initial size. The done channel is used to signal when
// monitoring of the socket is complete.
func verifyWindowGrowthForSocket(ctx context.Context, s *channelz.Socket, wantGrowth bool, done chan<- struct{}) error {
	defer close(done)

	var lastWindow int64
	for {
		if s.EphemeralMetrics != nil {
			metrics := s.EphemeralMetrics()
			if metrics.LocalFlowControlWindow > initialWindowSize {
				if wantGrowth {
					return nil
				}
				return fmt.Errorf("window grew to %d, but expected it to stay at %d", metrics.LocalFlowControlWindow, initialWindowSize)
			}
			lastWindow = metrics.LocalFlowControlWindow
		}

		select {
		case <-ctx.Done():
			if wantGrowth {
				return fmt.Errorf("window did not grow beyond initial size; last seen: %d, err: %v", lastWindow, ctx.Err())
			}
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}
