/*
 *
 * Copyright 2017, Google Inc.
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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/context"
)

// errDisabled indicates that proxy is disabled for the address.
var errDisabled = errors.New("proxy is disabled for the address")

func mapAddress(ctx context.Context, address string) (string, error) {
	req := &http.Request{
		URL: &url.URL{
			Scheme: "https",
			Host:   address,
		},
	}
	url, err := http.ProxyFromEnvironment(req)
	if err != nil {
		return "", err
	}
	if url == nil {
		return "", errDisabled
	}
	return url.Host, nil
}

// To read a response from a net.Conn, http.ReadResponse() takes a bufio.Reader.
// It's possible that this reader reads more than what's need for the response and stores
// those bytes in the buffer.
// bufConn wraps the original net.Conn and the bufio.Reader to make sure we don't lose the
// bytes in the buffer.
type bufConn struct {
	net.Conn
	r io.Reader
}

func (c *bufConn) Read(b []byte) (int, error) {
	return c.r.Read(b)
}

func doHTTPConnectHandshake(ctx context.Context, conn net.Conn, addr string) (_ net.Conn, err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	req := (&http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Host: addr},
		Header: map[string][]string{"User-Agent": {"gRPC-go"}},
	})

	if err := sendRequest(ctx, req, conn); err != nil {
		return nil, fmt.Errorf("failed to write the HTTP request: %v", err)
	}

	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		return nil, fmt.Errorf("reading server HTTP response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to do connect handshake, status code: %s", resp.Status)
	}

	return &bufConn{Conn: conn, r: r}, nil
}

// newProxyDialer returns a dialer that connects to proxy first if necessary.
// The returned dialer checks if a proxy is necessary, dial to the proxy with the
// provided dialer, does HTTP CONNECT handshake and returns the connection.
func newProxyDialer(dialer func(string, time.Duration) (net.Conn, error)) func(string, time.Duration) (net.Conn, error) {
	return func(addr string, d time.Duration) (conn net.Conn, err error) {
		ctx := context.Background()
		var cancel context.CancelFunc
		if d > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), d)
			defer cancel()
		}
		var skipHandshake bool

		newAddr, err := mapAddress(ctx, addr)
		if err != nil {
			if err != errDisabled {
				return nil, err
			}
			skipHandshake = true
			newAddr = addr
		}

		if deadline, ok := ctx.Deadline(); ok {
			conn, err = dialer(newAddr, deadline.Sub(time.Now()))
		} else {
			conn, err = dialer(newAddr, 0)
		}
		if err != nil {
			return
		}
		if !skipHandshake {
			conn, err = doHTTPConnectHandshake(context.Background(), conn, addr)
		}
		return
	}
}
