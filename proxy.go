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

package grpc

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const proxyAuthHeaderKey = "Proxy-Authorization"

var (
	// errDisabled indicates that proxy is disabled for the address.
	errDisabled = errors.New("proxy is disabled for the address")
	// The following variable will be overwritten in the tests.
	httpProxyFromEnvironment = http.ProxyFromEnvironment
)

func mapAddress(ctx context.Context, address string) (*url.URL, error) {
	req := &http.Request{
		URL: &url.URL{
			Scheme: "https",
			Host:   address,
		},
	}
	url, err := httpProxyFromEnvironment(req)
	if err != nil {
		return nil, err
	}
	if url == nil {
		return nil, errDisabled
	}
	return url, nil
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

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func parseAuthenticationScheme(input string) (string, error) {
	const ws = " \n\r\t"
	const qs = `"`
	s := strings.Trim(input, ws)
	words := strings.Split(s, " ")
	if words[0] == "Basic" || words[0] == "Digest" {
		return words[0], nil
	}
	return "", fmt.Errorf("unknown authentication scheme: %q", s)
}

func doHTTPConnectHandshake(ctx context.Context, conn net.Conn, backendAddr string, proxyURL *url.URL) (_ net.Conn, err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: backendAddr},
		Header: map[string][]string{"User-Agent": {grpcUA}},
	}
	if err := sendHTTPRequest(ctx, req, conn); err != nil {
		return nil, fmt.Errorf("failed to write the HTTP request: %v", err)
	}

	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, req)
	if err != nil {
		return nil, fmt.Errorf("reading server HTTP response: %v", err)
	}

	if resp.StatusCode == http.StatusProxyAuthRequired {
		resp.Body.Close()

		basicSchemeFound := false
		digestSchemeFound := false
		challenge := &digestChallenge{}
		for _, input := range resp.Header["Proxy-Authenticate"] {
			scheme, _ := parseAuthenticationScheme(input)
			if scheme == "Basic" {
				basicSchemeFound = true
			} else if scheme == "Digest" {
				digestSchemeFound = true
				challenge, err = parseChallenge(input)
				if err != nil {
					// ignore bad challenge. Maybe unsupported algorithm?
					continue
				}
				// rfc7616: When the client receives the first challenge, it SHOULD use
				// the first challenge it supports
				break
			}
		}
		if digestSchemeFound {
			if t := proxyURL.User; t != nil {
				u := t.Username()
				p, _ := t.Password()
				cr := newDigestCredentials(req, challenge, u, p)
				auth, err := cr.authorize()
				if err != nil {
					return nil, err
				}
				req.Header.Add(proxyAuthHeaderKey, auth)
			}
		} else if basicSchemeFound {
			if t := proxyURL.User; t != nil {
				u := t.Username()
				p, _ := t.Password()
				req.Header.Add(proxyAuthHeaderKey, "Basic "+basicAuth(u, p))
			}
		}
		if err := sendHTTPRequest(ctx, req, conn); err != nil {
			return nil, fmt.Errorf("failed to write the HTTP request: %v", err)
		}
		resp, err = http.ReadResponse(r, req)
		if err != nil {
			return nil, fmt.Errorf("reading server HTTP response: %v", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		dump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			return nil, fmt.Errorf("failed to do connect handshake, status code: %s", resp.Status)
		}
		return nil, fmt.Errorf("failed to do connect handshake, response: %q", dump)
	}

	return &bufConn{Conn: conn, r: r}, nil
}

// newProxyDialer returns a dialer that connects to proxy first if necessary.
// The returned dialer checks if a proxy is necessary, dial to the proxy with the
// provided dialer, does HTTP CONNECT handshake and returns the connection.
func newProxyDialer(dialer func(context.Context, string) (net.Conn, error)) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, addr string) (conn net.Conn, err error) {
		var newAddr string
		proxyURL, err := mapAddress(ctx, addr)
		if err != nil {
			if err != errDisabled {
				return nil, err
			}
			newAddr = addr
		} else {
			newAddr = proxyURL.Host
		}

		conn, err = dialer(ctx, newAddr)
		if err != nil {
			return
		}
		if proxyURL != nil {
			// proxy is disabled if proxyURL is nil.
			conn, err = doHTTPConnectHandshake(ctx, conn, addr, proxyURL)
		}
		return
	}
}

func sendHTTPRequest(ctx context.Context, req *http.Request, conn net.Conn) error {
	req = req.WithContext(ctx)
	if err := req.Write(conn); err != nil {
		return fmt.Errorf("failed to write the HTTP request: %v", err)
	}
	return nil
}
