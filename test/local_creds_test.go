/*
 *
 * Copyright 2020 gRPC authors.
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
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/local"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

func testE2ESucceed(network, address string) error {
	ss := &stubServer{
		emptyCall: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}

	sopts := []grpc.ServerOption{grpc.Creds(local.NewCredentials())}
	s := grpc.NewServer(sopts...)
	defer s.Stop()

	testpb.RegisterTestServiceServer(s, ss)

	lis, err := net.Listen(network, address)
	if err != nil {
		return fmt.Errorf("Failed to create listener: %v", err)
	}

	go s.Serve(lis)

	var cc *grpc.ClientConn
	if network == "unix" {
		cc, err = grpc.Dial("passthrough:///"+address, grpc.WithTransportCredentials(local.NewCredentials()), grpc.WithContextDialer(
			func(ctx context.Context, addr string) (net.Conn, error) {
				return net.Dial("unix", address)
			}))
	} else {
		cc, err = grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(local.NewCredentials()))
	}

	if err != nil {
		return fmt.Errorf("Failed to dial server: %v, %v", err, lis.Addr().String())
	}
	defer cc.Close()

	c := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := c.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		return fmt.Errorf("EmptyCall(_, _) = _, %v; want _, <nil>", err)
	}

	return nil
}

func (s) TestLocalhost(t *testing.T) {
	err := testE2ESucceed("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed e2e test for localhost: %v", err)
	}
}

func (s) TestUDS(t *testing.T) {
	addr := fmt.Sprintf("/tmp/grpc_fullstck_test%d", time.Now().UnixNano())
	err := testE2ESucceed("unix", addr)
	if err != nil {
		t.Fatalf("Failed e2e test for UDS: %v", err)
	}
}

type connWrapper struct {
	net.Conn
	remote net.Addr
}

func (c connWrapper) RemoteAddr() net.Addr {
	return c.remote
}

type lisWrapper struct {
	net.Listener
}

func newLisWrapper(l net.Listener) net.Listener {
	return &lisWrapper{l}
}

var remoteAddrs = []net.Addr{
	&net.IPAddr{
		IP:   net.ParseIP("10.8.9.10"),
		Zone: "",
	},
	&net.IPAddr{
		IP:   net.ParseIP("10.8.9.11"),
		Zone: "",
	},
}

func (l *lisWrapper) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return connWrapper{c, remoteAddrs[0]}, nil
}

func dialer(target string, t time.Duration) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", target, t)
	if err != nil {
		return nil, err
	}
	return connWrapper{c, remoteAddrs[1]}, nil
}

func testE2EFail(useLocal bool) error {
	ss := &stubServer{
		emptyCall: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}

	sopts := []grpc.ServerOption{grpc.Creds(local.NewCredentials())}
	s := grpc.NewServer(sopts...)
	defer s.Stop()

	testpb.RegisterTestServiceServer(s, ss)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("Failed to create listener: %v", err)
	}

	go s.Serve(newLisWrapper(lis))

	var cc *grpc.ClientConn
	if useLocal {
		cc, err = grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(local.NewCredentials()), grpc.WithDialer(dialer))
	} else {
		cc, err = grpc.Dial(lis.Addr().String(), grpc.WithInsecure(), grpc.WithDialer(dialer))
	}

	if err != nil {
		return fmt.Errorf("Failed to dial server: %v, %v", err, lis.Addr().String())
	}
	defer cc.Close()

	c := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err = c.EmptyCall(ctx, &testpb.Empty{})
	return err
}

func (s) TestClientFail(t *testing.T) {
	// Use local creds at client-side which should lead to client-side failure.
	err := testE2EFail(true /*useLocal*/)
	if err == nil || !strings.Contains(err.Error(), "local credentials rejected connection to non-local address") {
		t.Fatalf("testE2EFail(%v) = _; want security handshake fails, %v", false, err)
	}
}

func (s) TestServerFail(t *testing.T) {
	// Use insecure at client-side which should lead to server-side failure.
	err := testE2EFail(false /*useLocal*/)
	if err == nil || !strings.Contains(err.Error(), "connection closed") {
		t.Fatalf("testE2EFail(%v) = _; want security handshake fails, %v", true, err)
	}
}
