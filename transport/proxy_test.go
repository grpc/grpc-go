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
	"bufio"
	"io"
	"net"
	"net/http"
	"testing"

	"golang.org/x/net/context"
)

type proxyServer struct {
	t   *testing.T
	lis net.Listener
	in  net.Conn
	out net.Conn
}

func (p *proxyServer) run() {
	in, err := p.lis.Accept()
	if err != nil {
		p.t.Errorf("failed to accept: %v", err)
		return
	}
	p.in = in

	req, err := http.ReadRequest(bufio.NewReader(in))
	if err != nil {
		p.t.Errorf("failed to read CONNECT req: %v", err)
		return
	}
	if req.Method != http.MethodConnect || req.UserAgent() != "gRPC-go" {
		resp := http.Response{StatusCode: http.StatusMethodNotAllowed}
		resp.Write(p.in)
		p.in.Close()
		p.t.Errorf("get wrong CONNECT req: %+v", req)
		return
	}

	out, err := net.Dial("tcp", req.URL.Host)
	if err != nil {
		p.t.Errorf("failed to dial to server: %v", err)
		return
	}
	resp := http.Response{StatusCode: http.StatusOK, Proto: "HTTP/1.0"}
	resp.Write(p.in)
	p.out = out
	go io.Copy(p.in, p.out)
	go io.Copy(p.out, p.in)
}

func (p *proxyServer) stop() {
	p.lis.Close()
	if p.in != nil {
		p.in.Close()
	}
	if p.out != nil {
		p.out.Close()
	}
}

func TestHTTPConnect(t *testing.T) {
	plis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	p := &proxyServer{t: t, lis: plis}
	go p.run()
	defer p.stop()

	blis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	msg := []byte{4, 3, 5, 2}
	recvBuf := make([]byte, len(msg), len(msg))
	done := make(chan struct{})
	go func() {
		in, err := blis.Accept()
		if err != nil {
			t.Errorf("failed to accept: %v", err)
			return
		}
		defer in.Close()
		in.Read(recvBuf)
		close(done)
	}()

	// Dial to proxy server.
	c, err := net.Dial("tcp", plis.Addr().String())
	if err != nil {
		t.Fatalf("failed to create http connect dialer")
	}
	defer c.Close()
	// Do http connect handshake on the connection to proxy server.
	c, err = doHTTPConnectHandshake(context.Background(), c, blis.Addr().String())
	if err != nil {
		t.Fatalf("http connect Dial failed: %v", err)
	}
	defer c.Close()

	// Send msg on the connection.
	c.Write(msg)
	<-done

	// Check received msg.
	if string(recvBuf) != string(msg) {
		t.Fatalf("received msg: %v, want %v", recvBuf, msg)
	}
}
