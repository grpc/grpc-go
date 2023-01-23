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
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"

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
		{target: "://", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "://"))}},
		{target: ":///", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, ":///"))}},
		{target: "://a/", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "://a/"))}},
		{target: ":///a", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, ":///a"))}},
		{target: "://a/b", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "://a/b"))}},
		{target: "/", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "/"))}},
		{target: "a/b", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a/b"))}},
		{target: "a//b", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a//b"))}},
		{target: "google.com", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "google.com"))}},
		{target: "google.com/?a=b", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "google.com/?a=b"))}},
		{target: "/unix/socket/address", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "/unix/socket/address"))}},

		// An unregistered scheme is specified.
		{target: "a:///", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a:///"))}},
		{target: "a://b/", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a://b/"))}},
		{target: "a:///b", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a:///b"))}},
		{target: "a://b/c", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a://b/c"))}},
		{target: "a:b", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a:b"))}},
		{target: "a:/b", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a:/b"))}},
		{target: "a://b", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "a://b"))}},

		// A registered scheme is specified.
		{target: "dns:///google.com", wantParsed: resolver.Target{Scheme: "dns", Authority: "", URL: *testutils.MustParseURL("dns:///google.com")}},
		{target: "dns://a.server.com/google.com", wantParsed: resolver.Target{Scheme: "dns", Authority: "a.server.com", URL: *testutils.MustParseURL("dns://a.server.com/google.com")}},
		{target: "dns://a.server.com/google.com/?a=b", wantParsed: resolver.Target{Scheme: "dns", Authority: "a.server.com", URL: *testutils.MustParseURL("dns://a.server.com/google.com/?a=b")}},
		{target: "unix:///a/b/c", wantParsed: resolver.Target{Scheme: "unix", Authority: "", URL: *testutils.MustParseURL("unix:///a/b/c")}},
		{target: "unix-abstract:a/b/c", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:a/b/c")}},
		{target: "unix-abstract:a b", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:a b")}},
		{target: "unix-abstract:a:b", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:a:b")}},
		{target: "unix-abstract:a-b", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:a-b")}},
		{target: "unix-abstract:/ a///://::!@#$%25^&*()b", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:/ a///://::!@#$%25^&*()b")}},
		{target: "unix-abstract:passthrough:abc", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:passthrough:abc")}},
		{target: "unix-abstract:unix:///abc", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:unix:///abc")}},
		{target: "unix-abstract:///a/b/c", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:///a/b/c")}},
		{target: "unix-abstract:///", wantParsed: resolver.Target{Scheme: "unix-abstract", Authority: "", URL: *testutils.MustParseURL("unix-abstract:///")}},
		{target: "passthrough:///unix:///a/b/c", wantParsed: resolver.Target{Scheme: "passthrough", Authority: "", URL: *testutils.MustParseURL("passthrough:///unix:///a/b/c")}},

		// Cases for `scheme:absolute-path`.
		{target: "dns:/a/b/c", wantParsed: resolver.Target{Scheme: "dns", Authority: "", URL: *testutils.MustParseURL("dns:/a/b/c")}},
		{target: "unregistered:/a/b/c", badScheme: true, wantParsed: resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL("unregistered:/a/b/c")}},
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
			test.wantParsed.URL = *url

			cc, err := Dial(test.target, WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("Dial(%q) failed: %v", test.target, err)
			}
			defer cc.Close()

			if !cmp.Equal(cc.parsedTarget, test.wantParsed) {
				t.Errorf("cc.parsedTarget for dial target %q = %+v, want %+v", test.target, cc.parsedTarget, test.wantParsed)
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
			wantParsed:        resolver.Target{Scheme: "unix", Authority: "", URL: *testutils.MustParseURL("unix:a/b/c")},
			wantDialerAddress: "unix:a/b/c",
		},
		{
			target:            "unix:/a/b/c",
			wantParsed:        resolver.Target{Scheme: "unix", Authority: "", URL: *testutils.MustParseURL("unix:/a/b/c")},
			wantDialerAddress: "unix:///a/b/c",
		},
		{
			target:            "unix:///a/b/c",
			wantParsed:        resolver.Target{Scheme: "unix", Authority: "", URL: *testutils.MustParseURL("unix:///a/b/c")},
			wantDialerAddress: "unix:///a/b/c",
		},
		{
			target:            "dns:///127.0.0.1:50051",
			wantParsed:        resolver.Target{Scheme: "dns", Authority: "", URL: *testutils.MustParseURL("dns:///127.0.0.1:50051")},
			wantDialerAddress: "127.0.0.1:50051",
		},
		{
			target:            ":///127.0.0.1:50051",
			badScheme:         true,
			wantParsed:        resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, ":///127.0.0.1:50051"))},
			wantDialerAddress: ":///127.0.0.1:50051",
		},
		{
			target:            "dns://authority/127.0.0.1:50051",
			wantParsed:        resolver.Target{Scheme: "dns", Authority: "authority", URL: *testutils.MustParseURL("dns://authority/127.0.0.1:50051")},
			wantDialerAddress: "127.0.0.1:50051",
		},
		{
			target:            "://authority/127.0.0.1:50051",
			badScheme:         true,
			wantParsed:        resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "://authority/127.0.0.1:50051"))},
			wantDialerAddress: "://authority/127.0.0.1:50051",
		},
		{
			target:            "/unix/socket/address",
			badScheme:         true,
			wantParsed:        resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, "/unix/socket/address"))},
			wantDialerAddress: "/unix/socket/address",
		},
		{
			target:            "",
			badScheme:         true,
			wantParsed:        resolver.Target{Scheme: defScheme, Authority: "", URL: *testutils.MustParseURL(fmt.Sprintf("%s:///%s", defScheme, ""))},
			wantDialerAddress: "",
		},
		{
			target:            "passthrough://a.server.com/google.com",
			wantParsed:        resolver.Target{Scheme: "passthrough", Authority: "a.server.com", URL: *testutils.MustParseURL("passthrough://a.server.com/google.com")},
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
			test.wantParsed.URL = *url

			addrCh := make(chan string, 1)
			dialer := func(ctx context.Context, address string) (net.Conn, error) {
				addrCh <- address
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
			if !cmp.Equal(cc.parsedTarget, test.wantParsed) {
				t.Errorf("cc.parsedTarget for dial target %q = %+v, want %+v", test.target, cc.parsedTarget, test.wantParsed)
			}
		})
	}
}
