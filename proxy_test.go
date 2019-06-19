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

package grpc

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	envTestAddr  = "1.2.3.4:8080"
	envProxyAddr = "2.3.4.5:7687"
)

// overwriteAndRestore overwrite function httpProxyFromEnvironment and
// returns a function to restore the default values.
func overwrite(hpfe func(req *http.Request) (*url.URL, error)) func() {
	backHPFE := httpProxyFromEnvironment
	httpProxyFromEnvironment = hpfe
	return func() {
		httpProxyFromEnvironment = backHPFE
	}
}

type proxyServer struct {
	t   *testing.T
	lis net.Listener
	in  net.Conn
	out net.Conn

	initialRequestCheck func(*http.Request) error
	initialReplyPrepare func() *http.Response
	requestCheck        func(*http.Request) error
}

func (p *proxyServer) run() {
	in, err := p.lis.Accept()
	if err != nil {
		return
	}
	p.in = in

	req, err := http.ReadRequest(bufio.NewReader(in))
	if err != nil {
		p.t.Errorf("failed to read CONNECT req: %v", err)
		return
	}

	if p.initialRequestCheck != nil && p.initialReplyPrepare != nil {
		if err := p.initialRequestCheck(req); err != nil {
			resp := http.Response{StatusCode: http.StatusMethodNotAllowed}
			resp.Write(p.in)
			p.in.Close()
			p.t.Errorf("get wrong initial CONNECT req: %+v, error: %v", req, err)
			return
		}
		resp := p.initialReplyPrepare()
		resp.Write(p.in)

		req, err = http.ReadRequest(bufio.NewReader(in))
		if err != nil {
			p.t.Errorf("failed to read next CONNECT req: %v", err)
			return
		}
	}

	if err := p.requestCheck(req); err != nil {
		resp := http.Response{StatusCode: http.StatusMethodNotAllowed}
		resp.Write(p.in)
		p.in.Close()
		p.t.Errorf("get wrong CONNECT req: %+v, error: %v", req, err)
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

func testHTTPConnect(t *testing.T, proxyURLModify func(*url.URL) *url.URL,
	proxyInitialReqCheck func(*http.Request) error,
	proxyInitialRepPrepare func() *http.Response,
	proxyReqCheck func(*http.Request) error) {

	plis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	p := &proxyServer{
		t:                   t,
		lis:                 plis,
		initialRequestCheck: proxyInitialReqCheck,
		initialReplyPrepare: proxyInitialRepPrepare,
		requestCheck:        proxyReqCheck,
	}
	go p.run()
	defer p.stop()

	blis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	msg := []byte{4, 3, 5, 2}
	recvBuf := make([]byte, len(msg))
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

	// Overwrite the function in the test and restore them in defer.
	hpfe := func(req *http.Request) (*url.URL, error) {
		return proxyURLModify(&url.URL{Host: plis.Addr().String()}), nil
	}
	defer overwrite(hpfe)()

	// Dial to proxy server.
	dialer := newProxyDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		if deadline, ok := ctx.Deadline(); ok {
			return net.DialTimeout("tcp", addr, time.Until(deadline))
		}
		return net.Dial("tcp", addr)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := dialer(ctx, blis.Addr().String())
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

func (s) TestHTTPConnect(t *testing.T) {
	testHTTPConnect(t,
		func(in *url.URL) *url.URL {
			return in
		},
		nil,
		nil,
		func(req *http.Request) error {
			if req.Method != http.MethodConnect {
				return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
			}
			if req.UserAgent() != grpcUA {
				return fmt.Errorf("unexpect user agent %q, want %q", req.UserAgent(), grpcUA)
			}
			return nil
		},
	)
}

func (s) TestHTTPConnectBasicAuth(t *testing.T) {
	const (
		user     = "notAUser"
		password = "notAPassword"
	)
	testHTTPConnect(t,
		func(in *url.URL) *url.URL {
			in.User = url.UserPassword(user, password)
			return in
		},
		func(req *http.Request) error {
			if req.Method != http.MethodConnect {
				return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
			}
			if req.UserAgent() != grpcUA {
				return fmt.Errorf("unexpect user agent %q, want %q", req.UserAgent(), grpcUA)
			}
			if req.Header.Get(proxyAuthHeaderKey) != "" {
				return fmt.Errorf("unexpect header %q: %q",
					proxyAuthHeaderKey, req.Header.Get(proxyAuthHeaderKey))
			}
			return nil
		},
		func() *http.Response {
			resp := http.Response{
				StatusCode: http.StatusProxyAuthRequired,
				Proto:      "HTTP/1.0",
				Header:     make(http.Header)}
			resp.Header.Set("Proxy-Authenticate", `Basic realm="Test Proxy"`)
			return &resp
		},
		func(req *http.Request) error {
			if req.Method != http.MethodConnect {
				return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
			}
			if req.UserAgent() != grpcUA {
				return fmt.Errorf("unexpect user agent %q, want %q", req.UserAgent(), grpcUA)
			}
			wantProxyAuthStr := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
			if got := req.Header.Get(proxyAuthHeaderKey); got != wantProxyAuthStr {
				gotDecoded, _ := base64.StdEncoding.DecodeString(got)
				wantDecoded, _ := base64.StdEncoding.DecodeString(wantProxyAuthStr)
				return fmt.Errorf("unexpected auth %q (%q), want %q (%q)", got, gotDecoded, wantProxyAuthStr, wantDecoded)
			}
			return nil
		},
	)
}

func (s) TestHTTPConnectDigestAuth(t *testing.T) {
	const (
		user      = "notAUser"
		password  = "notAPassword"
		realm     = "Test Proxy"
		qop       = "auth"
		nonce     = "dcd98b7102dd2f0e8b11d0f600bfb0c093"
		opaque    = "5ccc069c403ebaf9f0171e9517f40e41"
		algorithm = "MD5"
	)
	testHTTPConnect(t,
		func(in *url.URL) *url.URL {
			in.User = url.UserPassword(user, password)
			return in
		},
		func(req *http.Request) error {
			if req.Method != http.MethodConnect {
				return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
			}
			if req.UserAgent() != grpcUA {
				return fmt.Errorf("unexpect user agent %q, want %q", req.UserAgent(), grpcUA)
			}
			if req.Header.Get(proxyAuthHeaderKey) != "" {
				return fmt.Errorf("unexpect header %q: %q",
					proxyAuthHeaderKey, req.Header.Get(proxyAuthHeaderKey))
			}
			return nil
		},
		func() *http.Response {
			resp := http.Response{
				StatusCode: http.StatusProxyAuthRequired,
				Proto:      "HTTP/1.0",
				Header:     make(http.Header)}
			resp.Header.Set("Proxy-Authenticate",
				fmt.Sprintf(`Digest realm="%s", qop="%s", nonce="%s", opaque="%s"`,
					realm, qop, nonce, opaque))
			return &resp
		},
		func(req *http.Request) error {
			if req.Method != http.MethodConnect {
				return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
			}
			if req.UserAgent() != grpcUA {
				return fmt.Errorf("unexpect user agent %q, want %q", req.UserAgent(), grpcUA)
			}
			got := req.Header.Get(proxyAuthHeaderKey)
			if !strings.HasPrefix(got, "Digest ") {
				return fmt.Errorf("unexpected auth %q, want the Digest prefix", got)
			}
			got = strings.TrimPrefix(got, "Digest ")
			fieldsList := strings.Split(got, ", ")
			fields := make(map[string]string)
			for i := range fieldsList {
				kv := strings.SplitN(fieldsList[i], "=", 2)
				fields[kv[0]] = strings.TrimSuffix(strings.TrimPrefix(kv[1], `"`), `"`)
			}
			expected := map[string]string{
				"username":  user,
				"realm":     realm,
				"nonce":     nonce,
				"uri":       req.URL.Host,
				"algorithm": algorithm,
				"opaque":    opaque,
				"qop":       qop,
				// "response":  "", // Tested separately in digest_test.go TestResp
			}
			for k, v := range expected {
				if fields[k] != v {
					return fmt.Errorf("unexpected %s %q, want %q", k, fields[k], v)
				}
			}
			return nil
		},
	)
}

func (s) TestMapAddressEnv(t *testing.T) {
	// Overwrite the function in the test and restore them in defer.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == envTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	defer overwrite(hpfe)()

	// envTestAddr should be handled by ProxyFromEnvironment.
	got, err := mapAddress(context.Background(), envTestAddr)
	if err != nil {
		t.Error(err)
	}
	if got.Host != envProxyAddr {
		t.Errorf("want %v, got %v", envProxyAddr, got)
	}
}
