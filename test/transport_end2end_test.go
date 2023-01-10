/*
*
* Copyright 2022 gRPC authors.
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
	"io"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

// authInfoWithConn wraps the underlying net.Conn, and makes it available
// to the test as part of the Peer call option.
type authInfoWithConn struct {
	credentials.CommonAuthInfo
	conn net.Conn
}

func (ai *authInfoWithConn) AuthType() string {
	return ""
}

// connWrapperWithCloseCh wraps a net.Conn and pushes on a channel when closed.
type connWrapperWithCloseCh struct {
	net.Conn
	closeCh chan interface{}
}

// Close closes the connection and sends a value on the close channel.
func (cw *connWrapperWithCloseCh) Close() error {
	err := cw.Conn.Close()
	for {
		select {
		case cw.closeCh <- nil:
			return err
		case <-cw.closeCh:
		}
	}
}

// These custom creds are used for storing the connections made by the client.
// The closeCh in conn can be used to detect when conn is closed.
type transportRestartCheckCreds struct {
	mu          sync.Mutex
	connections []connWrapperWithCloseCh
}

func (c *transportRestartCheckCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, nil, nil
}
func (c *transportRestartCheckCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn := &connWrapperWithCloseCh{Conn: rawConn, closeCh: make(chan interface{}, 1)}
	c.connections = append(c.connections, *conn)
	commonAuthInfo := credentials.CommonAuthInfo{SecurityLevel: credentials.NoSecurity}
	authInfo := &authInfoWithConn{commonAuthInfo, conn}
	return conn, authInfo, nil
}
func (c *transportRestartCheckCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}
func (c *transportRestartCheckCreds) Clone() credentials.TransportCredentials {
	return &transportRestartCheckCreds{}
}
func (c *transportRestartCheckCreds) OverrideServerName(s string) error {
	return nil
}

// Tests that the client transport drains and restarts when next stream ID exceeds
// MaxStreamID. This test also verifies that subsequent RPCs use a new client
// transport and the old transport is closed.
func (s) TestClientTransportRestartsAfterStreamIDExhausted(t *testing.T) {
	// Set the transport's MaxStreamID to 5 to cause connection to drain after 2 RPCs.
	originalMaxStreamID := transport.MaxStreamID
	transport.MaxStreamID = 5
	defer func() {
		transport.MaxStreamID = originalMaxStreamID
	}()

	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			for i := 0; i < 2; i++ {
				if _, err := stream.Recv(); err != nil {
					return status.Errorf(codes.Internal, "unexpected error receiving: %v", err)
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{}); err != nil {
					return status.Errorf(codes.Internal, "unexpected error sending: %v", err)
				}
			}
			if recv, err := stream.Recv(); err != io.EOF {
				return status.Errorf(codes.Internal, "Recv = %v, %v; want _, io.EOF", recv, err)
			}
			return nil
		},
	}

	creds := &transportRestartCheckCreds{}
	if err := ss.Start(nil, grpc.WithTransportCredentials(creds)); err != nil {
		t.Fatalf("Starting stubServer: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	var streams []testpb.TestService_FullDuplexCallClient

	// expectedNumConns when each stream is created.
	expectedNumConns := []int{1, 1, 2}

	// Set up 3 streams and call sendAndReceive() once on each.
	for i := 0; i < 3; i++ {
		s, err := ss.Client.FullDuplexCall(ctx)
		if err != nil {
			t.Fatalf("Creating FullDuplex stream: %v", err)
		}

		streams = append(streams, s)
		if err := s.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
			t.Fatalf("Sending on stream %d: %v", i, err)
		}
		if _, err := s.Recv(); err != nil {
			t.Fatalf("Receiving on stream %d: %v", i, err)
		}

		// Verify expected num of conns.
		if len(creds.connections) != expectedNumConns[i] {
			t.Fatalf("Number of connections created: %v, want: %v", len(creds.connections), expectedNumConns[i])
		}
	}

	// Verify all streams still work.
	for i, stream := range streams {
		if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
			t.Fatalf("Sending on stream %d: %v", i, err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("Receiving on stream %d: %v", i, err)
		}
	}

	var connPerStream []net.Conn

	// The peer passed via the call option is set up only after the RPC is complete.
	// Conn used by the stream is available in authInfo.
	for i, stream := range streams {
		if err := stream.CloseSend(); err != nil {
			t.Fatalf("CloseSend() on stream %d: %v", i, err)
		}
		p, ok := peer.FromContext(stream.Context())
		if !ok {
			t.Fatalf("Getting peer from stream context for stream %d", i)
		}
		connPerStream = append(connPerStream, p.AuthInfo.(*authInfoWithConn).conn)
	}

	// Verifying the first and second RPCs were made on the same connection.
	if connPerStream[0] != connPerStream[1] {
		t.Fatal("Got streams using different connections; want same.")
	}
	// Verifying the third and first/second RPCs were made on different connections.
	if connPerStream[2] == connPerStream[0] {
		t.Fatal("Got streams using same connections; want different.")
	}

	// Verifying first connection was closed.
	select {
	case <-creds.connections[0].closeCh:
	case <-ctx.Done():
		t.Fatal("Timeout expired when waiting for first client transport to close")
	}
}
