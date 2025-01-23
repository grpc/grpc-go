//go:build !race
// +build !race

/*
 *
 * Copyright 2017 gRPC authors.
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

package transport

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"google.golang.org/grpc/internal/proxyattributes"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/proxyserver"
	"google.golang.org/grpc/resolver"
)

func (s) TestHTTPConnectWithServerHello(t *testing.T) {
	serverMessage := []byte("server-hello")
	blis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	reqCheck := func(req *http.Request) {
		if req.Method != http.MethodConnect {
			t.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		host, _, err := net.SplitHostPort(req.URL.Host)
		if err != nil {
			t.Error(err)
		}
		_, err = netip.ParseAddr(host)
		if err != nil {
			t.Error(err)
		}
	}
	pServer := proxyserver.New(t, reqCheck, true)

	msg := []byte{4, 3, 5, 2}
	recvBuf := make([]byte, len(msg))
	done := make(chan error, 1)
	go func() {
		in, err := blis.Accept()
		if err != nil {
			done <- err
			return
		}
		defer in.Close()
		in.Write(serverMessage)
		in.Read(recvBuf)
		done <- nil
	}()

	// Dial to proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := proxyDial(ctx, resolver.Address{Addr: pServer.Addr}, "test", proxyattributes.Options{ConnectAddr: blis.Addr().String()})
	if err != nil {
		t.Fatalf("HTTP connect Dial failed: %v", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(defaultTestTimeout))

	// Send msg on the connection.
	c.Write(msg)
	if err := <-done; err != nil {
		t.Fatalf("Failed to accept: %v", err)
	}

	// Check received msg.
	if string(recvBuf) != string(msg) {
		t.Fatalf("Received msg: %v, want %v", recvBuf, msg)
	}

	if len(serverMessage) > 0 {
		gotServerMessage := make([]byte, len(serverMessage))
		if _, err := c.Read(gotServerMessage); err != nil {
			t.Errorf("Got error while reading message from server: %v", err)
			return
		}
		if string(gotServerMessage) != string(serverMessage) {
			t.Errorf("Message from server: %v, want %v", gotServerMessage, serverMessage)
		}
	}
}
