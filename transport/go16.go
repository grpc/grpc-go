// +build !go1.7

/*
 * Copyright 2016, Google Inc.
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
	"fmt"
	"net"
	"net/http"
	"net/url"

	"golang.org/x/net/context"
)

// dialContext connects to the address on the named network.
func dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{Cancel: ctx.Done()}).Dial(network, address)
}

func doHTTPConnectHandshake(ctx context.Context, conn net.Conn, addr string, header http.Header) (net.Conn, error) {
	if header == nil {
		header = make(map[string][]string)
	}
	if ua := header.Get("User-Agent"); ua == "" {
		header.Set("User-Agent", primaryUA)
	}
	if host := header.Get("Host"); host != "" {
		// Use the user specified Host header if it's set.
		addr = host
	}
	req := (&http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Host: addr},
		Header: header,
		Cancel: ctx.Done(), // WithContext is not avilable before go 1.7.
	})
	if err := req.Write(conn); err != nil {
		return conn, fmt.Errorf("failed to write the HTTP request: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return conn, fmt.Errorf("reading server HTTP response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return conn, fmt.Errorf("failed to do connect handshake, status code: %s", resp.Status)
	}

	return conn, nil
}
