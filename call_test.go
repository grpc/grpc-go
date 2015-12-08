/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpc

import (
	"io"
	"math"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"github.com/VerveWireless/grpc/codes"
	"github.com/VerveWireless/grpc/transport"
)

var (
	expectedRequest  = "ping"
	expectedResponse = "pong"
	sizeLargeErr     = 1024 * 1024
)

type testCodec struct {
}

func (testCodec) Marshal(v interface{}) ([]byte, error) {
	return []byte(*(v.(*string))), nil
}

func (testCodec) Unmarshal(data []byte, v interface{}) error {
	*(v.(*string)) = string(data)
	return nil
}

func (testCodec) String() string {
	return "test"
}

type testStreamHandler struct {
	t transport.ServerTransport
}

func (h *testStreamHandler) handleStream(t *testing.T, s *transport.Stream) {
	p := &parser{s: s}
	for {
		pf, req, err := p.recvMsg()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to receive the message from the client.")
		}
		if pf != compressionNone {
			t.Fatalf("Received the mistaken message format %d, want %d", pf, compressionNone)
		}
		var v string
		codec := testCodec{}
		if err := codec.Unmarshal(req, &v); err != nil {
			t.Fatalf("Failed to unmarshal the received message %v", err)
		}
		if v != expectedRequest {
			h.t.WriteStatus(s, codes.Internal, string(make([]byte, sizeLargeErr)))
			return
		}
	}
	// send a response back to end the stream.
	reply, err := encode(testCodec{}, &expectedResponse, compressionNone)
	if err != nil {
		t.Fatalf("Failed to encode the response: %v", err)
	}
	h.t.Write(s, reply, &transport.Options{})
	h.t.WriteStatus(s, codes.OK, "")
}

type server struct {
	lis  net.Listener
	port string
	// channel to signal server is ready to serve.
	readyChan chan bool
	mu        sync.Mutex
	conns     map[transport.ServerTransport]bool
}

// start starts server. Other goroutines should block on s.readyChan for futher operations.
func (s *server) start(t *testing.T, port int, maxStreams uint32) {
	var err error
	if port == 0 {
		s.lis, err = net.Listen("tcp", ":0")
	} else {
		s.lis, err = net.Listen("tcp", ":"+strconv.Itoa(port))
	}
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	_, p, err := net.SplitHostPort(s.lis.Addr().String())
	if err != nil {
		t.Fatalf("failed to parse listener address: %v", err)
	}
	s.port = p
	s.conns = make(map[transport.ServerTransport]bool)
	if s.readyChan != nil {
		close(s.readyChan)
	}
	for {
		conn, err := s.lis.Accept()
		if err != nil {
			return
		}
		st, err := transport.NewServerTransport("http2", conn, maxStreams, nil)
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.conns == nil {
			s.mu.Unlock()
			st.Close()
			return
		}
		s.conns[st] = true
		s.mu.Unlock()
		h := &testStreamHandler{st}
		go st.HandleStreams(func(s *transport.Stream) {
			go h.handleStream(t, s)
		})
	}
}

func (s *server) wait(t *testing.T, timeout time.Duration) {
	select {
	case <-s.readyChan:
	case <-time.After(timeout):
		t.Fatalf("Timed out after %v waiting for server to be ready", timeout)
	}
}

func (s *server) stop() {
	s.lis.Close()
	s.mu.Lock()
	for c := range s.conns {
		c.Close()
	}
	s.conns = nil
	s.mu.Unlock()
}

func setUp(t *testing.T, port int, maxStreams uint32) (*server, *ClientConn) {
	server := &server{readyChan: make(chan bool)}
	go server.start(t, port, maxStreams)
	server.wait(t, 2*time.Second)
	addr := "localhost:" + server.port
	cc, err := Dial(addr, WithBlock(), WithInsecure(), WithCodec(testCodec{}))
	if err != nil {
		t.Fatalf("Failed to create ClientConn: %v", err)
	}
	return server, cc
}

func TestInvoke(t *testing.T) {
	server, cc := setUp(t, 0, math.MaxUint32)
	var reply string
	if err := Invoke(context.Background(), "/foo/bar", &expectedRequest, &reply, cc); err != nil || reply != expectedResponse {
		t.Fatalf("grpc.Invoke(_, _, _, _, _) = %v, want <nil>", err)
	}
	cc.Close()
	server.stop()
}

func TestInvokeLargeErr(t *testing.T) {
	server, cc := setUp(t, 0, math.MaxUint32)
	var reply string
	req := "hello"
	err := Invoke(context.Background(), "/foo/bar", &req, &reply, cc)
	if _, ok := err.(rpcError); !ok {
		t.Fatalf("grpc.Invoke(_, _, _, _, _) receives non rpc error.")
	}
	if Code(err) != codes.Internal || len(ErrorDesc(err)) != sizeLargeErr {
		t.Fatalf("grpc.Invoke(_, _, _, _, _) = %v, want an error of code %d and desc size %d", err, codes.Internal, sizeLargeErr)
	}
	cc.Close()
	server.stop()
}
