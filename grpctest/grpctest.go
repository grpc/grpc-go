/*
Package grpctest provides utilities for RPC testing.

Call grpctest.NewServer to create a Server for a test;
use its Addr field as the address to dial, or invoke its Dial method.
*/
package grpctest

import (
	"net"
	"testing"

	"google.golang.org/grpc"
)

// A Server is an RPC server listening on a system-chosen port on the local loopback interface.
type Server struct {
	GRPC *grpc.Server
	Addr string // address of test server, suitable for passing to grpc.Dial

	t       *testing.T
	l       net.Listener
	closing bool
	closed  chan struct{}
}

// NewServer returns a new unstarted Server.
// Attach RPC services to s.GRPC, then invoke s.Start to start the server.
// The caller should call the Server's Close method when finished.
func NewServer(t *testing.T) *Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		// IPv4 might be unavailable. Try IPv6.
		t.Logf("net.Listen on 127.0.0.1:0: %v", err)
		l, err = net.Listen("tcp", "[::1]:0")
		if err != nil {
			t.Fatalf("net.Listen on [::1]:0: %v", err)
		}
	}
	s := &Server{
		GRPC:   grpc.NewServer(),
		Addr:   l.Addr().String(),
		t:      t,
		l:      l,
		closed: make(chan struct{}),
	}
	return s
}

// Start starts the server. RPC services must be registered before this is called.
func (s *Server) Start() {
	go func() {
		err := s.GRPC.Serve(s.l)
		if !s.closing && err != nil {
			s.t.Errorf("gRPC serve: %v", err)
		}
		close(s.closed)
	}()
}

// Close shuts down the server and refuses any new connections.
// Existing RPC connections will stay open.
func (s *Server) Close() {
	s.closing = true
	s.l.Close()
	<-s.closed
}

// Dial is like grpc.Dial(s.Addr) but with error handling abstracted.
func (s *Server) Dial(opts ...grpc.DialOption) *grpc.ClientConn {
	s.t.Helper()
	opts = append([]grpc.DialOption{grpc.WithInsecure()}, opts...)

	conn, err := grpc.Dial(s.Addr, opts...)
	if err != nil {
		s.t.Fatalf("grpc.Dial(%q): %v", s.Addr, err)
	}
	return conn
}
