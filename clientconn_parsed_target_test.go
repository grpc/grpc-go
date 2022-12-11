/*
 *
 * Copyright 2021 gRPC authors.
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
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials/insecure"

	"google.golang.org/grpc/resolver"
)

func (s) TestParsedTarget_Success_WithoutCustomDialer(t *testing.T) {
	defScheme := resolver.GetDefaultScheme()
	tests := []struct {
		target     string
		badScheme  bool
		wantParsed resolver.Target
	}{
		// No scheme is specified.
		{target: "://", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/://"}}},
		{target: ":///", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/:///"}}},
		{target: "://a/", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/://a/"}}},
		{target: ":///a", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/:///a"}}},
		{target: "://a/b", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/://a/b"}}},
		{target: "/", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "//"}}},
		{target: "a/b", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a/b"}}},
		{target: "a//b", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a//b"}}},
		{target: "google.com", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/google.com"}}},
		{target: "google.com/?a=b", badScheme: true, wantParsed: resolver.Target{
			URL: url.URL{Scheme: defScheme, Host: "", Path: "/google.com/", RawQuery: "a=b"}}},
		{target: "/unix/socket/address", badScheme: true, wantParsed: resolver.Target{
			URL: url.URL{Scheme: defScheme, Host: "", Path: "//unix/socket/address"}}},

		// An unregistered scheme is specified.
		{target: "a:///", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a:///"}}},
		{target: "a://b/", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a://b/"}}},
		{target: "a:///b", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a:///b"}}},
		{target: "a://b/c", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a://b/c"}}},
		{target: "a:b", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a:b"}}},
		{target: "a:/b", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a:/b"}}},
		{target: "a://b", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/a://b"}}},

		// A registered scheme is specified.
		{target: "dns:///google.com", wantParsed: resolver.Target{URL: url.URL{Scheme: "dns", Host: "", Path: "/google.com"}}},
		{target: "dns://a.server.com/google.com", wantParsed: resolver.Target{
			URL: url.URL{Scheme: "dns", Host: "a.server.com", Path: "/google.com"}}},
		{target: "dns://a.server.com/google.com/?a=b", wantParsed: resolver.Target{
			URL: url.URL{Scheme: "dns", Host: "a.server.com", Path: "/google.com/", RawQuery: "a=b"}}},
		{target: "unix:///a/b/c", wantParsed: resolver.Target{URL: url.URL{Scheme: "unix", Host: "", Path: "/a/b/c"}}},
		{target: "unix-abstract:a/b/c", wantParsed: resolver.Target{URL: url.URL{Scheme: "unix-abstract", Host: "", Opaque: "a/b/c"}}},
		{target: "unix-abstract:a b", wantParsed: resolver.Target{URL: url.URL{Scheme: "unix-abstract", Host: "", Opaque: "a b"}}},
		{target: "unix-abstract:a:b", wantParsed: resolver.Target{URL: url.URL{Scheme: "unix-abstract", Host: "", Opaque: "a:b"}}},
		{target: "unix-abstract:a-b", wantParsed: resolver.Target{}},
		{target: "unix-abstract:/ a///://::!@#$%25^&*()b", wantParsed: resolver.Target{}},
		{target: "unix-abstract:passthrough:abc", wantParsed: resolver.Target{URL: url.URL{Scheme: "unix-abstract", Opaque: "passthrough:abc"}}},
		{target: "unix-abstract:unix:///abc", wantParsed: resolver.Target{URL: url.URL{Scheme: "unix-abstract", Opaque: "unix:///abc"}}},
		{target: "unix-abstract:///a/b/c", wantParsed: resolver.Target{URL: url.URL{Scheme: "unix-abstract", Path: "/a/b/c"}}},
		{target: "unix-abstract:///", wantParsed: resolver.Target{URL: url.URL{Scheme: "unix-abstract", Path: "/"}}},
		{target: "passthrough:///unix:///a/b/c", wantParsed: resolver.Target{URL: url.URL{Scheme: "passthrough", Path: "/unix:///a/b/c"}}},

		// Cases for `scheme:absolute-path`.
		{target: "dns:/a/b/c", wantParsed: resolver.Target{URL: url.URL{Scheme: "dns", Path: "/a/b/c", OmitHost: true}}},
		{target: "unregistered:/a/b/c", badScheme: true, wantParsed: resolver.Target{URL: url.URL{Scheme: defScheme, Path: "/unregistered:/a/b/c"}}},
	}

	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			target := test.target
			if test.badScheme {
				target = defScheme + ":///" + target
			}
			url, err := url.Parse(target)
			if err != nil {
				t.Fatalf("Unexpected error parsing URL: %v", err)
			}
			if test.wantParsed.URL.Scheme == "" {
				test.wantParsed.URL = *url
			}

			cc, err := Dial(test.target, WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("Dial(%q) failed: %v", test.target, err)
			}
			defer cc.Close()

			if !cmp.Equal(cc.parsedTarget.URL, test.wantParsed.URL) {
				t.Errorf("cc.parsedTarget.URL for dial target %q = %+v, want %+v", test.target, cc.parsedTarget.URL, test.wantParsed.URL)
			}
		})
	}
}

func (s) TestParsedTarget_Failure_WithoutCustomDialer(t *testing.T) {
	targets := []string{
		"",
		"unix://a/b/c",
		"unix://authority",
		"unix-abstract://authority/a/b/c",
		"unix-abstract://authority",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			if cc, err := Dial(target, WithTransportCredentials(insecure.NewCredentials())); err == nil {
				defer cc.Close()
				t.Fatalf("Dial(%q) succeeded cc.parsedTarget = %+v, expected to fail", target, cc.parsedTarget)
			}
		})
	}
}

func (s) TestParsedTarget_WithCustomDialer(t *testing.T) {
	defScheme := resolver.GetDefaultScheme()
	tests := []struct {
		target            string
		badScheme         bool
		wantParsed        resolver.Target
		wantDialerAddress string
	}{
		// unix:[local_path], unix:[/absolute], and unix://[/absolute] have
		// different behaviors with a custom dialer.
		{
			target:            "unix:a/b/c",
			wantParsed:        resolver.Target{URL: url.URL{Scheme: "unix", Host: "", Opaque: "a/b/c"}},
			wantDialerAddress: "unix:a/b/c",
		},
		{
			target:            "unix:/a/b/c",
			wantParsed:        resolver.Target{URL: url.URL{Scheme: "unix", Host: "", OmitHost: true, Path: "/a/b/c"}},
			wantDialerAddress: "unix:///a/b/c",
		},
		{
			target:            "unix:///a/b/c",
			wantParsed:        resolver.Target{URL: url.URL{Scheme: "unix", Host: "", Path: "/a/b/c"}},
			wantDialerAddress: "unix:///a/b/c",
		},

		{
			target:            "dns:///127.0.0.1:50051",
			wantParsed:        resolver.Target{URL: url.URL{Scheme: "dns", Host: "", Path: "/127.0.0.1:50051"}},
			wantDialerAddress: "127.0.0.1:50051",
		},

		{
			target:            ":///127.0.0.1:50051",
			badScheme:         true,
			wantParsed:        resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/:///127.0.0.1:50051"}},
			wantDialerAddress: ":///127.0.0.1:50051",
		},

		{
			target:            "dns://authority/127.0.0.1:50051",
			wantParsed:        resolver.Target{URL: url.URL{Scheme: "dns", Host: "authority", Path: "/127.0.0.1:50051"}},
			wantDialerAddress: "127.0.0.1:50051",
		},

		{
			target:            "://authority/127.0.0.1:50051",
			badScheme:         true,
			wantParsed:        resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/://authority/127.0.0.1:50051"}},
			wantDialerAddress: "://authority/127.0.0.1:50051",
		},

		{
			target:            "/unix/socket/address",
			badScheme:         true,
			wantParsed:        resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "//unix/socket/address"}},
			wantDialerAddress: "/unix/socket/address",
		},

		{
			target:            "",
			badScheme:         true,
			wantParsed:        resolver.Target{URL: url.URL{Scheme: defScheme, Host: "", Path: "/"}},
			wantDialerAddress: "",
		},
		{
			target:            "passthrough://a.server.com/google.com",
			wantParsed:        resolver.Target{URL: url.URL{Scheme: "passthrough", Host: "a.server.com", Path: "/google.com"}},
			wantDialerAddress: "google.com",
		},
	}

	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			target := test.target
			if test.badScheme {
				target = defScheme + ":///" + target
			}
			url, err := url.Parse(target)
			if err != nil {
				t.Fatalf("Unexpected error parsing URL: %v", err)
			}
			if test.wantParsed.URL.Scheme == "" {
				test.wantParsed.URL = *url
			}

			addrCh := make(chan string, 1)
			dialer := func(ctx context.Context, address string) (net.Conn, error) {
				addrCh <- address
				t.Log("passsing address:", address)
				return nil, errors.New("dialer error")
			}

			cc, err := Dial(test.target, WithTransportCredentials(insecure.NewCredentials()), WithContextDialer(dialer))
			if err != nil {
				t.Fatalf("Dial(%q) failed: %v", test.target, err)
			}
			defer cc.Close()

			select {
			case addr := <-addrCh:
				if addr != test.wantDialerAddress {
					t.Fatalf("address in custom dialer is %q, want %q", addr, test.wantDialerAddress)
				}
			case <-time.After(time.Second):
				t.Fatal("timeout when waiting for custom dialer to be invoked")
			}
			if !cmp.Equal(cc.parsedTarget.URL, test.wantParsed.URL) {
				t.Errorf("cc.parsedTarget.URL for dial target %q = %+v, want %+v", test.target, cc.parsedTarget.URL, test.wantParsed.URL)
			}
		})
	}
}
