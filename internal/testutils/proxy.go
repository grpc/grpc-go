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

package testutils

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const defaultTestTimeout = 10 * time.Second

// ProxyServer represents a test proxy server.
type ProxyServer struct {
	lis          net.Listener
	in           net.Conn                  // The connection from the client to the proxy.
	out          net.Conn                  // The connection from the proxy to the backend.
	requestCheck func(*http.Request) error // The function to check the request sent to proxy.
}

// Stop closes the ProxyServer and its connectionsto client and server.
func (p *ProxyServer) Stop() {
	p.lis.Close()
	if p.in != nil {
		p.in.Close()
	}
	if p.out != nil {
		p.out.Close()
	}
}

// NewProxyServer create and starts a proxy server.
func NewProxyServer(lis net.Listener, reqCheck func(*http.Request) error, errCh chan error, doneCh chan struct{}, backendAddr string, resOnClient bool, proxyStarted func()) *ProxyServer {
	p := &ProxyServer{
		lis:          lis,
		requestCheck: reqCheck,
	}

	// Start the proxy server.
	go func() {
		in, err := p.lis.Accept()
		if err != nil {
			return
		}
		p.in = in
		if proxyStarted != nil {
			proxyStarted()
		}
		req, err := http.ReadRequest(bufio.NewReader(in))
		if err != nil {
			errCh <- fmt.Errorf("failed to read CONNECT req: %v", err)
			return
		}
		if err := p.requestCheck(req); err != nil {
			resp := http.Response{StatusCode: http.StatusMethodNotAllowed}
			resp.Write(p.in)
			p.in.Close()
			errCh <- fmt.Errorf("get wrong CONNECT req: %+v, error: %v", req, err)
			return
		}
		var out net.Conn
		// if resolution is done on client,connect to address received in
		// CONNECT request or else connect to backend address directly.
		if resOnClient {
			out, err = net.Dial("tcp", req.URL.Host)
		} else {
			out, err = net.Dial("tcp", backendAddr)
		}

		if err != nil {
			errCh <- fmt.Errorf("failed to dial to server: %v", err)
			return
		}
		out.SetDeadline(time.Now().Add(defaultTestTimeout))
		//response OK to client
		resp := http.Response{StatusCode: http.StatusOK, Proto: "HTTP/1.0"}
		var buf bytes.Buffer
		resp.Write(&buf)
		p.in.Write(buf.Bytes())
		p.out = out
		// perform the proxy function, i.e pass the data from client to server and server to client.
		go io.Copy(p.in, p.out)
		go io.Copy(p.out, p.in)
		close(doneCh)
	}()
	return p
}
