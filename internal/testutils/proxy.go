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

type ProxyServer struct {
	lis net.Listener
	in  net.Conn
	out net.Conn

	requestCheck func(*http.Request) error
}

func (p *ProxyServer) run(errCh chan error, backendAddr string) {
	fmt.Printf("run proxy server")
	in, err := p.lis.Accept()
	if err != nil {
		return
	}
	p.in = in

	req, err := http.ReadRequest(bufio.NewReader(in))
	if err != nil {
		errCh <- fmt.Errorf("failed to read CONNECT req: %v", err)
		// p.t.Errorf("failed to read CONNECT req: %v", err)
		return
	}
	fmt.Printf("request sent : %+v\n", req)
	if err := p.requestCheck(req); err != nil {
		resp := http.Response{StatusCode: http.StatusMethodNotAllowed}
		resp.Write(p.in)
		p.in.Close()
		errCh <- fmt.Errorf("get wrong CONNECT req: %+v, error: %v", req, err)
		// p.t.Errorf("get wrong CONNECT req: %+v, error: %v", req, err)
		return
	}
	var out net.Conn
	//To mimick name resolution on proxy, if proxy gets unresolved address in
	// the CONNECT request, dial to the backend address directly.
	if net.ParseIP(req.URL.Host) == nil {
		fmt.Printf("dialing to backend add: %v from proxy", backendAddr)
		out, err = net.Dial("tcp", backendAddr)
	} else {
		//If proxy gets resolved address in CONNECT req,dial to the actual server
		// with address sent in the connect request.
		fmt.Printf("\ndialing with host in connect req\n")
		out, err = net.Dial("tcp", req.URL.Host)
	}
	if err != nil {
		errCh <- fmt.Errorf("failed to dial to server: %v", err)
		// p.t.Errorf("failed to dial to server: %v", err)
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
}

func (p *ProxyServer) Stop() {
	p.lis.Close()
	if p.in != nil {
		p.in.Close()
	}
	if p.out != nil {
		p.out.Close()
	}
}

func NewProxyServer(lis net.Listener, requestCheck func(*http.Request) error, errCh chan error, backendAddr string) *ProxyServer {
	fmt.Printf("starting proxy server")
	p := &ProxyServer{
		lis:          lis,
		requestCheck: requestCheck,
	}
	go p.run(errCh, backendAddr)
	return p
}
