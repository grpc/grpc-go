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

package transport

import (
	"bytes"
	"io"
	"log"
	"math"
	"net"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

type server struct {
	lis  net.Listener
	port string
	// channel to signal server is ready to serve.
	readyChan chan bool
	mu        sync.Mutex
	conns     map[ServerTransport]bool
}

var (
	tlsDir                = "testdata/"
	expectedRequest       = []byte("ping")
	expectedResponse      = []byte("pong")
	expectedRequestLarge  = make([]byte, initialWindowSize*2)
	expectedResponseLarge = make([]byte, initialWindowSize*2)
)

type testStreamHandler struct {
	t ServerTransport
}

func (h *testStreamHandler) handleStream(s *Stream) {
	req := expectedRequest
	resp := expectedResponse
	if s.Method() == "foo.Large" {
		req = expectedRequestLarge
		resp = expectedResponseLarge
	}
	p := make([]byte, len(req))
	_, err := io.ReadFull(s, p)
	if err != nil || !bytes.Equal(p, req) {
		if err == ErrConnClosing {
			return
		}
		log.Fatalf("handleStream got error: %v, want <nil>; result: %v, want %v", err, p, req)
	}
	// send a response back to the client.
	h.t.Write(s, resp, &Options{})
	// send the trailer to end the stream.
	h.t.WriteStatus(s, codes.OK, "")
}

// handleStreamSuspension blocks until s.ctx is canceled.
func (h *testStreamHandler) handleStreamSuspension(s *Stream) {
	<-s.ctx.Done()
}

func (s *server) Start(useTLS bool, port int, maxStreams uint32, suspend bool) {
	var err error
	if port == 0 {
		s.lis, err = net.Listen("tcp", ":0")
	} else {
		s.lis, err = net.Listen("tcp", ":"+strconv.Itoa(port))
	}
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	if useTLS {
		creds, err := credentials.NewServerTLSFromFile(tlsDir+"server1.pem", tlsDir+"server1.key")
		if err != nil {
			log.Fatalf("Failed to generate credentials %v", err)
		}
		s.lis = creds.NewListener(s.lis)
	}
	_, p, err := net.SplitHostPort(s.lis.Addr().String())
	if err != nil {
		log.Fatalf("failed to parse listener address: %v", err)
	}
	s.port = p
	if s.readyChan != nil {
		close(s.readyChan)
	}
	s.conns = make(map[ServerTransport]bool)
	for {
		conn, err := s.lis.Accept()
		if err != nil {
			return
		}
		t, err := NewServerTransport("http2", conn, maxStreams)
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.conns == nil {
			s.mu.Unlock()
			t.Close()
			return
		}
		s.conns[t] = true
		s.mu.Unlock()
		h := &testStreamHandler{t}
		if suspend {
			go t.HandleStreams(h.handleStreamSuspension)
		} else {
			go t.HandleStreams(h.handleStream)
		}
	}
}

func (s *server) Wait(t *testing.T, timeout time.Duration) {
	select {
	case <-s.readyChan:
	case <-time.After(timeout):
		t.Fatalf("Timed out after %v waiting for server to be ready", timeout)
	}
}

func (s *server) Close() {
	// Keep consistent with closeServer().
	s.lis.Close()
	s.mu.Lock()
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
}

func setUp(t *testing.T, useTLS bool, port int, maxStreams uint32, suspend bool) (*server, ClientTransport) {
	server := &server{readyChan: make(chan bool)}
	go server.Start(useTLS, port, maxStreams, suspend)
	server.Wait(t, 2*time.Second)
	addr := "localhost:" + server.port
	var (
		ct      ClientTransport
		connErr error
	)
	if useTLS {
		creds, err := credentials.NewClientTLSFromFile(tlsDir+"ca.pem", "x.test.youtube.com")
		if err != nil {
			t.Fatalf("Failed to create credentials %v", err)
		}
		ct, connErr = NewClientTransport("http2", addr, []credentials.Credentials{creds})
	} else {
		ct, connErr = NewClientTransport("http2", addr, nil)
	}
	if connErr != nil {
		t.Fatalf("failed to create transport: %v", connErr)
	}
	return server, ct
}

func TestClientSendAndReceive(t *testing.T) {
	server, ct := setUp(t, true, 0, math.MaxUint32, false)
	callHdr := &CallHdr{
		Host:   "localhost",
		Method: "foo.Small",
	}
	s1, err1 := ct.NewStream(context.Background(), callHdr)
	if err1 != nil {
		t.Fatalf("failed to open stream: %v", err1)
	}
	if s1.id != 1 {
		t.Fatalf("wrong stream id: %d", s1.id)
	}
	s2, err2 := ct.NewStream(context.Background(), callHdr)
	if err2 != nil {
		t.Fatalf("failed to open stream: %v", err2)
	}
	if s2.id != 3 {
		t.Fatalf("wrong stream id: %d", s2.id)
	}
	opts := Options{
		Last:  true,
		Delay: false,
	}
	if err := ct.Write(s1, expectedRequest, &opts); err != nil {
		t.Fatalf("failed to send data: %v", err)
	}
	p := make([]byte, len(expectedResponse))
	_, recvErr := io.ReadFull(s1, p)
	if recvErr != nil || !bytes.Equal(p, expectedResponse) {
		t.Fatalf("Error: %v, want <nil>; Result: %v, want %v", recvErr, p, expectedResponse)
	}
	_, recvErr = io.ReadFull(s1, p)
	if recvErr != io.EOF {
		t.Fatalf("Error: %v; want <EOF>", recvErr)
	}
	closeClient(ct, t)
	closeServer(server, t)
}

func TestClientErrorNotify(t *testing.T) {
	server, ct := setUp(t, true, 0, math.MaxUint32, false)
	callHdr := &CallHdr{
		Host:   "localhost",
		Method: "foo.Small",
	}
	s, err := ct.NewStream(context.Background(), callHdr)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	if s.id != 1 {
		t.Fatalf("wrong stream id: %d", s.id)
	}
	// Tear down the server.
	go closeServer(server, t)
	// ct.reader should detect the error and activate ct.Error().
	<-ct.Error()
	closeClient(ct, t)
}

func performOneRPC(ct ClientTransport) {
	callHdr := &CallHdr{
		Host:   "localhost",
		Method: "foo.Small",
	}
	s, err := ct.NewStream(context.Background(), callHdr)
	if err != nil {
		return
	}
	opts := Options{
		Last:  true,
		Delay: false,
	}
	if err := ct.Write(s, expectedRequest, &opts); err == nil {
		time.Sleep(5 * time.Millisecond)
		// The following s.Recv()'s could error out because the
		// underlying transport is gone.
		//
		// Read response
		p := make([]byte, len(expectedResponse))
		io.ReadFull(s, p)
		// Read io.EOF
		io.ReadFull(s, p)
	}
}

func TestClientMix(t *testing.T) {
	s, ct := setUp(t, true, 0, math.MaxUint32, false)
	go func(s *server) {
		time.Sleep(5 * time.Second)
		closeServer(s, t)
	}(s)
	go func(ct ClientTransport) {
		<-ct.Error()
		closeClient(ct, t)
	}(ct)
	for i := 0; i < 1000; i++ {
		time.Sleep(10 * time.Millisecond)
		go performOneRPC(ct)
	}
}

func TestExceedMaxStreamsLimit(t *testing.T) {
	server, ct := setUp(t, true, 0, 1, false)
	defer func() {
		closeClient(ct, t)
		closeServer(server, t)
	}()
	callHdr := &CallHdr{
		Host:   "localhost",
		Method: "foo.Small",
	}
	// Creates the 1st stream and keep it alive.
	_, err1 := ct.NewStream(context.Background(), callHdr)
	if err1 != nil {
		t.Fatalf("failed to open stream: %v", err1)
	}
	// Creates the 2nd stream. It has chance to succeed when the settings
	// frame from the server has not received at the client.
	s, err2 := ct.NewStream(context.Background(), callHdr)
	if err2 != nil {
		se, ok := err2.(StreamError)
		if !ok {
			t.Fatalf("Received unexpected error %v", err2)
		}
		if se.Code != codes.Unavailable {
			t.Fatalf("Got error code: %d, want: %d", se.Code, codes.Unavailable)
		}
		return
	}
	// If the 2nd stream is created successfully, sends the request.
	if err := ct.Write(s, expectedRequest, &Options{Last: true, Delay: false}); err != nil {
		t.Fatalf("failed to send data: %v", err)
	}
	// The 2nd stream was rejected by the server via a reset.
	p := make([]byte, len(expectedResponse))
	_, recvErr := io.ReadFull(s, p)
	if recvErr != io.EOF || s.StatusCode() != codes.Unavailable {
		t.Fatalf("Error: %v, StatusCode: %d; want <EOF>, %d", recvErr, s.StatusCode(), codes.Unavailable)
	}
	// Server's setting has been received. From now on, new stream will be rejected instantly.
	_, err3 := ct.NewStream(context.Background(), callHdr)
	if err3 == nil {
		t.Fatalf("Received unexpected <nil>, want an error with code %d", codes.Unavailable)
	}
	if se, ok := err3.(StreamError); !ok || se.Code != codes.Unavailable {
		t.Fatalf("Got: %v, want a StreamError with error code %d", err3, codes.Unavailable)
	}
}

func TestLargeMessage(t *testing.T) {
	server, ct := setUp(t, true, 0, math.MaxUint32, false)
	callHdr := &CallHdr{
		Host:   "localhost",
		Method: "foo.Large",
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			s, err := ct.NewStream(context.Background(), callHdr)
			if err != nil {
				t.Fatalf("failed to open stream: %v", err)
			}
			if err := ct.Write(s, expectedRequestLarge, &Options{Last: true, Delay: false}); err != nil {
				t.Fatalf("failed to send data: %v", err)
			}
			p := make([]byte, len(expectedResponseLarge))
			_, recvErr := io.ReadFull(s, p)
			if recvErr != nil || !bytes.Equal(p, expectedResponseLarge) {
				t.Fatalf("Error: %v, want <nil>; Result len: %d, want len %d", recvErr, len(p), len(expectedResponseLarge))
			}
			_, recvErr = io.ReadFull(s, p)
			if recvErr != io.EOF {
				t.Fatalf("Error: %v; want <EOF>", recvErr)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	closeClient(ct, t)
	closeServer(server, t)
}

func TestLargeMessageSuspension(t *testing.T) {
	server, ct := setUp(t, true, 0, math.MaxUint32, true)
	callHdr := &CallHdr{
		Host:   "localhost",
		Method: "foo.Large",
	}
	// Set a long enough timeout for writing a large message out.
	ctx, _ := context.WithTimeout(context.Background(), time.Second)
	s, err := ct.NewStream(ctx, callHdr)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	// Write should not be done successfully due to flow control.
	err = ct.Write(s, expectedRequestLarge, &Options{Last: true, Delay: false})
	expectedErr := StreamErrorf(codes.DeadlineExceeded, "%v", context.DeadlineExceeded)
	if err == nil || err != expectedErr {
		t.Fatalf("Write got %v, want %v", err, expectedErr)
	}
	closeClient(ct, t)
	closeServer(server, t)
}

func TestStreamContext(t *testing.T) {
	expectedStream := Stream{}
	ctx := newContextWithStream(context.Background(), &expectedStream)
	s, ok := StreamFromContext(ctx)
	if !ok || !reflect.DeepEqual(expectedStream, *s) {
		t.Fatalf("GetStreamFromContext(%v) = %v, %t, want: %v, true", ctx, *s, ok, expectedStream)
	}
}

// closeClient shuts down the ClientTransport and reports any errors to the
// test framework and terminates the current test case.
func closeClient(ct ClientTransport, t *testing.T) {
	if err := ct.Close(); err != nil {
		t.Fatalf("ct.Close() = %v, want <nil>", err)
	}
}

// closeServerWithErr shuts down the testing server, closing the associated
// transports.  It returns the first error it encounters, if any.
func closeServerWithErr(s *server) error {
	// Keep consistent with s.Close().
	s.lis.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		if err := c.Close(); err != nil {
			return err
		}
	}
	return nil
}

// closeServer shuts down the and testing server, closing the associated
// transport.  It reports any errors to the test framework and terminates the
// current test case.
func closeServer(s *server, t *testing.T) {
	if err := closeServerWithErr(s); err != nil {
		t.Fatalf("server.Close() = %v, want <nil>", err)
	}
}

func TestClientServerDuplicatedClose(t *testing.T) {
	server, ct := setUp(t, true, 0, math.MaxUint32, false)
	if err := ct.Close(); err != nil {
		t.Fatalf("ct.Close() = %v, want <nil>", err)
	}
	if err := ct.Close(); err == nil {
		// Duplicated closes should gracefully issue an error.
		t.Fatalf("ct.Close() = <nil>, want non-nil")
	}
	if err := closeServerWithErr(server); err != nil {
		t.Fatalf("closeServerWithErr(server) = %v, want <nil>", err)
	}
	if err := closeServerWithErr(server); err == nil {
		// Duplicated closes should gracefully issue an error.
		t.Fatalf("closeServerWithErr(server) = <nil>, want non-nil")
	}
}
