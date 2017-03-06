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

// Package proxy defines interfaces to support proxyies in gRPC.
package proxy // import "google.golang.org/grpc/proxy"
import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"

	"golang.org/x/net/context"
)

// ErrIneffective indicates the mapper function is not effective.
var ErrIneffective = errors.New("MapAddress function is not effective")

// Proxyer defines the interface gRPC uses to map the proxy address.
type Proxyer interface {
	// MapAddress is called before we connect to the target address.
	// It can be used to programmatically override the address that we will connect to.
	// It returns the address of the proxy, and the header to be sent in the request.
	MapAddress(ctx context.Context, address string) (string, map[string][]string, error)
	// Handshake does the proxy handshake on the connection.
	Handshake(ctx context.Context, conn net.Conn, addr string, header http.Header) (net.Conn, error)
}

// NewEnvironmentVariableHTTPConnectProxy returns a Proxyer that returns the address of the proxy
// as indicated by the environment variables HTTP_PROXY, HTTPS_PROXY and NO_PROXY
// (or the lowercase versions thereof) when mapping address, and does HTTP connect handshake on
// the connection.
func NewEnvironmentVariableHTTPConnectProxy() Proxyer {
	return &environmentProxyMapper{}
}

type environmentProxyMapper struct{}

func (pm *environmentProxyMapper) MapAddress(ctx context.Context, address string) (string, map[string][]string, error) {
	req := &http.Request{
		URL: &url.URL{Host: address},
	}
	url, err := http.ProxyFromEnvironment(req)
	if err != nil {
		return "", nil, err
	}
	if url == nil {
		return "", nil, ErrIneffective
	}
	return url.String(), nil, nil
}

func (pm *environmentProxyMapper) Handshake(ctx context.Context, conn net.Conn, addr string, header http.Header) (net.Conn, error) {
	return doHTTPConnectHandshake(ctx, conn, addr, header)
}

type bufConn struct {
	net.Conn
	r io.Reader
}

func (c *bufConn) Read(b []byte) (int, error) {
	return c.r.Read(b)
}
