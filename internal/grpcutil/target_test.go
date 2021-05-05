/*
 *
 * Copyright 2020 gRPC authors.
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

package grpcutil

import (
	"testing"

	"google.golang.org/grpc/resolver"
)

func TestParseTarget(t *testing.T) {
	for _, test := range []resolver.Target{
		{Scheme: "dns", Authority: "", Endpoint: "google.com"},
		{Scheme: "dns", Authority: "a.server.com", Endpoint: "google.com"},
		{Scheme: "dns", Authority: "a.server.com", Endpoint: "google.com/?a=b"},
		{Scheme: "passthrough", Authority: "", Endpoint: "/unix/socket/address"},
	} {
		str := test.Scheme + "://" + test.Authority + "/" + test.Endpoint
		got := ParseTarget(str, false)
		if got != test {
			t.Errorf("ParseTarget(%q, false) = %+v, want %+v", str, got, test)
		}
		got = ParseTarget(str, true)
		if got != test {
			t.Errorf("ParseTarget(%q, true) = %+v, want %+v", str, got, test)
		}
	}
}

func TestParseTargetString(t *testing.T) {
	for _, test := range []struct {
		targetStr      string
		want           resolver.Target
		wantWithDialer resolver.Target
	}{
		{targetStr: "", want: resolver.Target{Scheme: "", Authority: "", Endpoint: ""}},
		{targetStr: ":///", want: resolver.Target{Scheme: "", Authority: "", Endpoint: ""}},
		{targetStr: "a:///", want: resolver.Target{Scheme: "a", Authority: "", Endpoint: ""}},
		{targetStr: "://a/", want: resolver.Target{Scheme: "", Authority: "a", Endpoint: ""}},
		{targetStr: ":///a", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "a"}},
		{targetStr: "a://b/", want: resolver.Target{Scheme: "a", Authority: "b", Endpoint: ""}},
		{targetStr: "a:///b", want: resolver.Target{Scheme: "a", Authority: "", Endpoint: "b"}},
		{targetStr: "://a/b", want: resolver.Target{Scheme: "", Authority: "a", Endpoint: "b"}},
		{targetStr: "a://b/c", want: resolver.Target{Scheme: "a", Authority: "b", Endpoint: "c"}},
		{targetStr: "dns:///google.com", want: resolver.Target{Scheme: "dns", Authority: "", Endpoint: "google.com"}},
		{targetStr: "dns://a.server.com/google.com", want: resolver.Target{Scheme: "dns", Authority: "a.server.com", Endpoint: "google.com"}},
		{targetStr: "dns://a.server.com/google.com/?a=b", want: resolver.Target{Scheme: "dns", Authority: "a.server.com", Endpoint: "google.com/?a=b"}},

		{targetStr: "/", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "/"}},
		{targetStr: "google.com", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "google.com"}},
		{targetStr: "google.com/?a=b", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "google.com/?a=b"}},
		{targetStr: "/unix/socket/address", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "/unix/socket/address"}},

		// If we can only parse part of the target.
		{targetStr: "://", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "://"}},
		{targetStr: "unix://domain", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "unix://domain"}},
		{targetStr: "unix://a/b/c", want: resolver.Target{Scheme: "unix", Authority: "a", Endpoint: "/b/c"}},
		{targetStr: "a:b", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "a:b"}},
		{targetStr: "a/b", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "a/b"}},
		{targetStr: "a:/b", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "a:/b"}},
		{targetStr: "a//b", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "a//b"}},
		{targetStr: "a://b", want: resolver.Target{Scheme: "", Authority: "", Endpoint: "a://b"}},

		// Unix cases without custom dialer.
		// unix:[local_path], unix:[/absolute], and unix://[/absolute] have different
		// behaviors with a custom dialer, to prevent behavior changes with custom dialers.
		{targetStr: "unix:a/b/c", want: resolver.Target{Scheme: "unix", Authority: "", Endpoint: "a/b/c"}, wantWithDialer: resolver.Target{Scheme: "", Authority: "", Endpoint: "unix:a/b/c"}},
		{targetStr: "unix:/a/b/c", want: resolver.Target{Scheme: "unix", Authority: "", Endpoint: "/a/b/c"}, wantWithDialer: resolver.Target{Scheme: "", Authority: "", Endpoint: "unix:/a/b/c"}},
		{targetStr: "unix:///a/b/c", want: resolver.Target{Scheme: "unix", Authority: "", Endpoint: "/a/b/c"}},

		{targetStr: "unix-abstract:a/b/c", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "a/b/c"}},
		{targetStr: "unix-abstract:a b", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "a b"}},
		{targetStr: "unix-abstract:a:b", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "a:b"}},
		{targetStr: "unix-abstract:a-b", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "a-b"}},
		{targetStr: "unix-abstract:/ a///://::!@#$%^&*()b", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "/ a///://::!@#$%^&*()b"}},
		{targetStr: "unix-abstract:passthrough:abc", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "passthrough:abc"}},
		{targetStr: "unix-abstract:unix:///abc", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "unix:///abc"}},
		{targetStr: "unix-abstract:///a/b/c", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "/a/b/c"}},
		{targetStr: "unix-abstract://authority/a/b/c", want: resolver.Target{Scheme: "unix-abstract", Authority: "authority", Endpoint: "/a/b/c"}},
		{targetStr: "unix-abstract:///", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "/"}},
		{targetStr: "unix-abstract://authority", want: resolver.Target{Scheme: "unix-abstract", Authority: "", Endpoint: "//authority"}},

		{targetStr: "passthrough:///unix:///a/b/c", want: resolver.Target{Scheme: "passthrough", Authority: "", Endpoint: "unix:///a/b/c"}},
	} {
		got := ParseTarget(test.targetStr, false)
		if got != test.want {
			t.Errorf("ParseTarget(%q, false) = %+v, want %+v", test.targetStr, got, test.want)
		}
		wantWithDialer := test.wantWithDialer
		if wantWithDialer == (resolver.Target{}) {
			wantWithDialer = test.want
		}
		got = ParseTarget(test.targetStr, true)
		if got != wantWithDialer {
			t.Errorf("ParseTarget(%q, true) = %+v, want %+v", test.targetStr, got, wantWithDialer)
		}
	}
}
