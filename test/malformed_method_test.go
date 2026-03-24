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

package test

import (
	"bytes"
	"context"
	"net"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"

	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// TestMalformedMethodPath tests that the server responds with Unimplemented
// when the method path is malformed. This verifies that the server does not
// route requests with a malformed method path to the application handler.
func (s) TestMalformedMethodPath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		envVar     bool
		wantStatus string // string representation of codes.Code
	}{
		{
			name:       "missing_leading_slash_disableStrictPathChecking_false",
			path:       "grpc.testing.TestService/UnaryCall",
			wantStatus: "12", // Unimplemented
		},
		{
			name:       "empty_path_disableStrictPathChecking_false",
			path:       "",
			wantStatus: "12", // Unimplemented
		},
		{
			name:       "just_slash_disableStrictPathChecking_false",
			path:       "/",
			wantStatus: "12", // Unimplemented
		},
		{
			name:       "missing_leading_slash_disableStrictPathChecking_true",
			path:       "grpc.testing.TestService/UnaryCall",
			envVar:     true,
			wantStatus: "0", // OK
		},
		{
			name:       "empty_path_disableStrictPathChecking_true",
			path:       "",
			envVar:     true,
			wantStatus: "12", // Unimplemented
		},
		{
			name:       "just_slash_disableStrictPathChecking_true",
			path:       "/",
			envVar:     true,
			wantStatus: "12", // Unimplemented
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			testutils.SetEnvConfig(t, &envconfig.DisableStrictPathChecking, tc.envVar)

			ss := &stubserver.StubServer{
				UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
					return &testpb.SimpleResponse{Payload: &testpb.Payload{Body: []byte("pwned")}}, nil
				},
			}
			if err := ss.Start(nil); err != nil {
				t.Fatalf("Error starting endpoint server: %v", err)
			}
			defer ss.Stop()

			// Open a raw TCP connection to the server and speak HTTP/2 directly.
			tcpConn, err := net.Dial("tcp", ss.Address)
			if err != nil {
				t.Fatalf("Failed to dial tcp: %v", err)
			}
			defer tcpConn.Close()

			// Write the HTTP/2 connection preface and the initial settings frame.
			if _, err := tcpConn.Write([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")); err != nil {
				t.Fatalf("Failed to write preface: %v", err)
			}
			framer := http2.NewFramer(tcpConn, tcpConn)
			if err := framer.WriteSettings(); err != nil {
				t.Fatalf("Failed to write settings: %v", err)
			}

			// Encode and write the HEADERS frame.
			var headerBuf bytes.Buffer
			enc := hpack.NewEncoder(&headerBuf)
			writeHeader := func(name, value string) {
				enc.WriteField(hpack.HeaderField{Name: name, Value: value})
			}
			writeHeader(":method", "POST")
			writeHeader(":scheme", "http")
			writeHeader(":authority", ss.Address)
			writeHeader(":path", tc.path)
			writeHeader("content-type", "application/grpc")
			writeHeader("te", "trailers")
			if err := framer.WriteHeaders(http2.HeadersFrameParam{
				StreamID:      1,
				BlockFragment: headerBuf.Bytes(),
				EndStream:     false,
				EndHeaders:    true,
			}); err != nil {
				t.Fatalf("Failed to write headers: %v", err)
			}

			// Send a small gRPC-encoded data frame (0 length).
			if err := framer.WriteData(1, true, []byte{0, 0, 0, 0, 0}); err != nil {
				t.Fatalf("Failed to write data: %v", err)
			}

			// Read responses and look for grpc-status.
			gotStatus := ""
			dec := hpack.NewDecoder(4096, func(f hpack.HeaderField) {
				if f.Name == "grpc-status" {
					gotStatus = f.Value
				}
			})
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					frame, err := framer.ReadFrame()
					if err != nil {
						return
					}
					if headers, ok := frame.(*http2.HeadersFrame); ok {
						if _, err := dec.Write(headers.HeaderBlockFragment()); err != nil {
							return
						}
						if headers.StreamEnded() {
							return
						}
					}
				}
			}()

			select {
			case <-done:
			case <-ctx.Done():
				t.Fatalf("Timed out waiting for response")
			}

			if gotStatus != tc.wantStatus {
				t.Errorf("Got grpc-status %v, want %v", gotStatus, tc.wantStatus)
			}
		})
	}
}
