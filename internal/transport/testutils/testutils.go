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

// Package testutils contains helpers for transport tests.
package testutils

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc/internal/testutils"
)

const defaultTestTimeout = 10 * time.Second

// ProxyServer represents a test proxy server.
type ProxyServer struct {
	lis       net.Listener
	in        net.Conn            // Connection from the client to the proxy.
	out       net.Conn            // Connection from the proxy to the backend.
	onRequest func(*http.Request) // Function to check the request sent to proxy.
	Addr      string              // Address of the proxy
}

// Stop closes the ProxyServer and its connections to client and server.
func (p *ProxyServer) stop() {
	p.lis.Close()
	if p.in != nil {
		p.in.Close()
	}
	if p.out != nil {
		p.out.Close()
	}
}

func (p *ProxyServer) handleRequest(t *testing.T, in net.Conn, waitForServerHello bool) {
	req, err := http.ReadRequest(bufio.NewReader(in))
	if err != nil {
		t.Errorf("failed to read CONNECT req: %v", err)
		return
	}

	p.onRequest(req)

	t.Logf("Dialing to %s", req.URL.Host)
	out, err := net.Dial("tcp", req.URL.Host)
	if err != nil {
		in.Close()
		t.Logf("failed to dial to server: %v", err)
		return
	}
	out.SetDeadline(time.Now().Add(defaultTestTimeout))
	resp := http.Response{StatusCode: http.StatusOK, Proto: "HTTP/1.0"}
	var buf bytes.Buffer
	resp.Write(&buf)

	if waitForServerHello {
		// Batch the first message from the server with the http connect
		// response. This is done to test the cases in which the grpc client has
		// the response to the connect request and proxied packets from the
		// destination server when it reads the transport.
		b := make([]byte, 50)
		bytesRead, err := out.Read(b)
		if err != nil {
			t.Errorf("Got error while reading server hello: %v", err)
			in.Close()
			out.Close()
			return
		}
		buf.Write(b[0:bytesRead])
	}
	p.in = in
	p.in.Write(buf.Bytes())
	p.out = out

	go io.Copy(p.in, p.out)
	go io.Copy(p.out, p.in)
}

// SetupProxy initializes and starts a proxy server, registers a cleanup to
// stop it, and returns the proxy's listener and helper channels.
func SetupProxy(t *testing.T, reqCheck func(*http.Request), waitForServerHello bool) *ProxyServer {
	t.Helper()
	pLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	p := &ProxyServer{
		lis:       pLis,
		onRequest: reqCheck,
	}

	// Start the proxy server.
	go func() {
		for {
			in, err := p.lis.Accept()
			if err != nil {
				return
			}
			// p.handleRequest is not invoked in a goroutine because the test
			// proxy currently supports handling only one connection at a time.
			p.handleRequest(t, in, waitForServerHello)
		}
	}()
	t.Cleanup(p.stop)
	p.Addr = fmt.Sprintf("localhost:%d", testutils.ParsePort(t, pLis.Addr().String()))
	return p
}
