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
	"time"

	"golang.org/x/net/context"
)

// ErrIneffective indicates the mapper function is not effective.
var ErrIneffective = errors.New("Mapper function is not effective")

// Mapper defines the interface gRPC uses to map the proxy address.
type Mapper interface {
	// MapAddress is called before we connect to the target address.
	// It can be used to programmatically override the address that we will connect to.
	// It returns the address of the proxy, and the header to be sent in the request.
	MapAddress(ctx context.Context, address string) (string, map[string][]string, error)
}

// NewEnvironmentProxyMapper returns a Mapper that returns the address of the proxy
// as indicated by the environment variables HTTP_PROXY, HTTPS_PROXY and NO_PROXY
// (or the lowercase versions thereof).
func NewEnvironmentProxyMapper() Mapper {
	return &environmentProxyMapper{}
}

type environmentProxyMapper struct{}

func (pm *environmentProxyMapper) MapAddress(ctx context.Context, address string) (string, map[string][]string, error) {
	req := &http.Request{
		URL: &url.URL{
			Scheme: "https",
			Host:   address,
		},
	}
	url, err := http.ProxyFromEnvironment(req)
	if err != nil {
		return "", nil, err
	}
	if url == nil {
		return "", nil, ErrIneffective
	}
	return url.Host, nil, nil
}

// Handshaker defines the interface to do proxy handshake.
type Handshaker interface {
	// Handshake takes the connection to do proxy handshake on, the addr of the real server behind the
	// proxy and the header to be sent to the proxy server.
	// It returns the new connection after handshake and error if there's any.
	Handshake(ctx context.Context, conn net.Conn, addr string, header http.Header) (net.Conn, error)
}

// NewHTTPConnectHandshaker returns a Handshaker that does HTTP CONNECT handshake
// on the given connection.
func NewHTTPConnectHandshaker() Handshaker {
	return &httpConnectHandshaker{}
}

type httpConnectHandshaker struct{}

func (h *httpConnectHandshaker) Handshake(ctx context.Context, conn net.Conn, addr string, header http.Header) (net.Conn, error) {
	return doHTTPConnectHandshake(ctx, conn, addr, header)
}

type bufConn struct {
	net.Conn
	r io.Reader
}

func (c *bufConn) Read(b []byte) (int, error) {
	return c.r.Read(b)
}

// NewDialer returns a dialer with the provided Mapper, Handshaker and dialer.
// The returned dialer uses Mapper to get the proxy's address, dial to the proxy with the
// provided dialer, does handshake with the Handshaker and returns the connection.
func NewDialer(pm Mapper, hs Handshaker, dialer func(string, time.Duration) (net.Conn, error)) func(string, time.Duration) (net.Conn, error) {
	return func(addr string, d time.Duration) (conn net.Conn, err error) {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		var skipHandshake bool

		newAddr, h, err := pm.MapAddress(ctx, addr)
		if err != nil {
			if err != ErrIneffective {
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
			conn, err = hs.Handshake(context.Background(), conn, addr, h)
		}
		return
	}
}
