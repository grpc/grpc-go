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
	"testing"
	"time"

	"google.golang.org/grpc/resolver"
)

func (s) TestParsedTarget_Success_WithoutCustomDialer(t *testing.T) {
	defScheme := resolver.GetDefaultScheme()
	tests := []struct {
		target     string
		wantParsed resolver.Target
	}{
		// No scheme is specified.
		{target: "", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: ""}},
		{target: ":///", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: ":///"}},
		{target: "://a/", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "://a/"}},
		{target: ":///a", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: ":///a"}},
		{target: "://a/b", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "://a/b"}},
		{target: "/", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "/"}},
		{target: "a/b", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a/b"}},
		{target: "a//b", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a//b"}},
		{target: "google.com", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "google.com"}},
		{target: "google.com/?a=b", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "google.com/?a=b"}},
		{target: "/unix/socket/address", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "/unix/socket/address"}},

		// An unregistered scheme is specified.
		{target: "a:///", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a:///"}},
		{target: "a://b/", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a://b/"}},
		{target: "a:///b", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a:///b"}},
		{target: "a://b/c", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a://b/c"}},
		{target: "a:b", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a:b"}},
		{target: "a:/b", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a:/b"}},
		{target: "a://b", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "a://b"}},

		// A registered scheme is specified.
		{target: "dns:///google.com", wantParsed: resolver.Target{Scheme: "dns", Authority: "", Endpoint: "google.com"}},
		{target: "dns://a.server.com/google.com", wantParsed: resolver.Target{Scheme: "dns", Authority: "a.server.com", Endpoint: "google.com"}},
		{target: "dns://a.server.com/google.com/?a=b", wantParsed: resolver.Target{Scheme: "dns", Authority: "a.server.com", Endpoint: "google.com/?a=b"}},
		{target: "unix:///a/b/c", wantParsed: resolver.Target{Scheme: "unix", Authority: "", Endpoint: "/a/b/c"}},
		{target: "unix-abstract:a/b/c", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "a/b/c"}},
		{target: "unix-abstract:a b", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "a b"}},
		{target: "unix-abstract:a:b", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "a:b"}},
		{target: "unix-abstract:a-b", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "a-b"}},
		{target: "unix-abstract:/ a///://::!@#$%^&*()b", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "/ a///://::!@#$%^&*()b"}},
		{target: "unix-abstract:passthrough:abc", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "passthrough:abc"}},
		{target: "unix-abstract:unix:///abc", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "unix:///abc"}},
		{target: "unix-abstract:///a/b/c", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "/a/b/c"}},
		{target: "unix-abstract:///", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "/"}},
		{target: "unix-abstract://authority", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "//authority"}},
		{target: "passthrough:///unix:///a/b/c", wantParsed: resolver.Target{Scheme: "passthrough", Authority: "", Endpoint: "unix:///a/b/c"}},

		// If we can only parse part of the target.
		{target: "://", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "://"}},
		{target: "unix://domain", wantParsed: resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "unix://domain"}},
	}

	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			cc, err := Dial(test.target, WithInsecure())
			if err != nil {
				t.Fatalf("Dial(%q) failed: %v", test.target, err)
			}
			defer cc.Close()

			if gotParsed := cc.parsedTarget; gotParsed != test.wantParsed {
				t.Errorf("cc.parsedTarget = %+v, want %+v", gotParsed, test.wantParsed)
			}
		})
	}
}

func (s) TestParsedTarget_Failure_WithoutCustomDialer(t *testing.T) {
	targets := []string{
		"unix://a/b/c",
		"unix-abstract://authority/a/b/c",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			if cc, err := Dial(target, WithInsecure()); err == nil {
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
		wantParsed        resolver.Target
		wantDialerAddress string
	}{
		// unix:[local_path], unix:[/absolute], and unix://[/absolute] have
		// different behaviors with a custom dialer.
		{
			target:            "unix:a/b/c",
			wantParsed:        resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "unix:a/b/c"},
			wantDialerAddress: "unix:a/b/c",
		},
		{
			target:            "unix:/a/b/c",
			wantParsed:        resolver.Target{Scheme: defScheme, Authority: "", Endpoint: "unix:/a/b/c"},
			wantDialerAddress: "unix:/a/b/c",
		},
		{
			target:            "unix:///a/b/c",
			wantParsed:        resolver.Target{Scheme: "unix", Authority: "", Endpoint: "/a/b/c"},
			wantDialerAddress: "unix:///a/b/c",
		},
	}

	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			addrCh := make(chan string, 1)
			dialer := func(ctx context.Context, address string) (net.Conn, error) {
				addrCh <- address
				return nil, errors.New("dialer error")
			}

			cc, err := Dial(test.target, WithInsecure(), WithContextDialer(dialer))
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
			if gotParsed := cc.parsedTarget; gotParsed != test.wantParsed {
				t.Errorf("cc.parsedTarget for dial target %q = %+v, want %+v", test.target, gotParsed, test.wantParsed)
			}
		})
	}
}
