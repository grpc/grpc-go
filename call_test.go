/*
 *
 * Copyright 2014 gRPC authors.
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

package grpc

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/status"
)

var (
	expectedRequest  = "ping"
	expectedResponse = "pong"
	weirdError       = "format verbs: %v%s"
	sizeLargeErr     = 1024 * 1024
	canceled         = 0
)

const defaultTestTimeout = 10 * time.Second

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
	port string
	t    transport.ServerTransport
}

func (h *testStreamHandler) handleStream(t *testing.T, s *transport.Stream) {
	p := &parser{r: s}
	for {
		pf, req, err := p.recvMsg(math.MaxInt32)
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
		if pf != compressionNone {
			t.Errorf("Received the mistaken message format %d, want %d", pf, compressionNone)
			return
		}
		var v string
		codec := testCodec{}
		if err := codec.Unmarshal(req, &v); err != nil {
			t.Errorf("Failed to unmarshal the received message: %v", err)
			return
		}
		if v == "weird error" {
			h.t.WriteStatus(s, status.New(codes.Internal, weirdError))
			return
		}
		if v == "canceled" {
			canceled++
			h.t.WriteStatus(s, status.New(codes.Internal, ""))
			return
		}
		if v == "port" {
			h.t.WriteStatus(s, status.New(codes.Internal, h.port))
			return
		}

		if v != expectedRequest {
			h.t.WriteStatus(s, status.New(codes.Internal, strings.Repeat("A", sizeLargeErr)))
			return
		}
	}
	// send a response back to end the stream.
	data, err := encode(testCodec{}, &expectedResponse)
	if err != nil {
		t.Errorf("Failed to encode the response: %v", err)
		return
	}
	hdr, payload := msgHeader(data, nil)
	h.t.Write(s, hdr, payload, &transport.Options{})
	h.t.WriteStatus(s, status.New(codes.OK, ""))
}

type server struct {
	lis        net.Listener
	port       string
	addr       string
	startedErr chan error // sent nil or an error after server starts
	mu         sync.Mutex
	conns      map[transport.ServerTransport]bool
	channelzID *channelz.Identifier
}

func newTestServer() *server {
	return &server{
		startedErr: make(chan error, 1),
		channelzID: channelz.NewIdentifierForTesting(channelz.RefServer, time.Now().Unix(), nil),
	}
}

// start starts server. Other goroutines should block on s.startedErr for further operations.
func (s *server) start(t *testing.T, port int, maxStreams uint32) {
	var err error
	if port == 0 {
		s.lis, err = net.Listen("tcp", "localhost:0")
	} else {
		s.lis, err = net.Listen("tcp", "localhost:"+strconv.Itoa(port))
	}
	if err != nil {
		s.startedErr <- fmt.Errorf("failed to listen: %v", err)
		return
	}
	s.addr = s.lis.Addr().String()
	_, p, err := net.SplitHostPort(s.addr)
	if err != nil {
		s.startedErr <- fmt.Errorf("failed to parse listener address: %v", err)
		return
	}
	s.port = p
	s.conns = make(map[transport.ServerTransport]bool)
	s.startedErr <- nil
	for {
		conn, err := s.lis.Accept()
		if err != nil {
			return
		}
		config := &transport.ServerConfig{
			MaxStreams:       maxStreams,
			ChannelzParentID: s.channelzID,
		}
		st, err := transport.NewServerTransport(conn, config)
		if err != nil {
			t.Errorf("failed to create server transport: %v", err)
			continue
		}
		s.mu.Lock()
		if s.conns == nil {
			s.mu.Unlock()
			st.Close()
			return
		}
		s.conns[st] = true
		s.mu.Unlock()
		h := &testStreamHandler{
			port: s.port,
			t:    st,
		}
		go st.HandleStreams(func(s *transport.Stream) {
			go h.handleStream(t, s)
		}, func(ctx context.Context, method string) context.Context {
			return ctx
		})
	}
}

func (s *server) wait(t *testing.T, timeout time.Duration) {
	select {
	case err := <-s.startedErr:
		if err != nil {
			t.Fatal(err)
		}
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
