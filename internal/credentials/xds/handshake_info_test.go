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

package xds

import (
	"crypto/x509"
	"net"
	"net/url"
	"regexp"
	"testing"

	xdsinternal "google.golang.org/grpc/internal/xds"
)

func TestDNSMatch(t *testing.T) {
	tests := []struct {
		desc      string
		host      string
		pattern   string
		wantMatch bool
	}{
		{
			desc:      "invalid wildcard 1",
			host:      "aa.example.com",
			pattern:   "*a.example.com",
			wantMatch: false,
		},
		{
			desc:      "invalid wildcard 2",
			host:      "aa.example.com",
			pattern:   "a*.example.com",
			wantMatch: false,
		},
		{
			desc:      "invalid wildcard 3",
			host:      "abc.example.com",
			pattern:   "a*c.example.com",
			wantMatch: false,
		},
		{
			desc:      "wildcard in one of the middle components",
			host:      "abc.test.example.com",
			pattern:   "abc.*.example.com",
			wantMatch: false,
		},
		{
			desc:      "single component wildcard",
			host:      "a.example.com",
			pattern:   "*",
			wantMatch: false,
		},
		{
			desc:      "short host name",
			host:      "a.com",
			pattern:   "*.example.com",
			wantMatch: false,
		},
		{
			desc:      "suffix mismatch",
			host:      "a.notexample.com",
			pattern:   "*.example.com",
			wantMatch: false,
		},
		{
			desc:      "wildcard match across components",
			host:      "sub.test.example.com",
			pattern:   "*.example.com.",
			wantMatch: false,
		},
		{
			desc:      "host doesn't end in period",
			host:      "test.example.com",
			pattern:   "test.example.com.",
			wantMatch: true,
		},
		{
			desc:      "pattern doesn't end in period",
			host:      "test.example.com.",
			pattern:   "test.example.com",
			wantMatch: true,
		},
		{
			desc:      "case insensitive",
			host:      "TEST.EXAMPLE.COM.",
			pattern:   "test.example.com.",
			wantMatch: true,
		},
		{
			desc:      "simple match",
			host:      "test.example.com",
			pattern:   "test.example.com",
			wantMatch: true,
		},
		{
			desc:      "good wildcard",
			host:      "a.example.com",
			pattern:   "*.example.com",
			wantMatch: true,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			gotMatch := dnsMatch(test.host, test.pattern)
			if gotMatch != test.wantMatch {
				t.Fatalf("dnsMatch(%s, %s) = %v, want %v", test.host, test.pattern, gotMatch, test.wantMatch)
			}

		})
	}
}

func TestMatchingSANExists_FailureCases(t *testing.T) {
	url1, err := url.Parse("http://golang.org")
	if err != nil {
		t.Fatalf("url.Parse() failed: %v", err)
	}
	url2, err := url.Parse("https://github.com/grpc/grpc-go")
	if err != nil {
		t.Fatalf("url.Parse() failed: %v", err)
	}
	inputCert := &x509.Certificate{
		DNSNames:       []string{"foo.bar.example.com", "bar.baz.test.com", "*.example.com"},
		EmailAddresses: []string{"foobar@example.com", "barbaz@test.com"},
		IPAddresses:    []net.IP{net.ParseIP("192.0.0.1"), net.ParseIP("2001:db8::68")},
		URIs:           []*url.URL{url1, url2},
	}

	tests := []struct {
		desc        string
		sanMatchers []xdsinternal.StringMatcher
	}{
		{
			desc: "exact match",
			sanMatchers: []xdsinternal.StringMatcher{
				{ExactMatch: newStringP("abcd.test.com")},
				{ExactMatch: newStringP("http://golang")},
				{ExactMatch: newStringP("HTTP://GOLANG.ORG")},
			},
		},
		{
			desc: "prefix match",
			sanMatchers: []xdsinternal.StringMatcher{
				{PrefixMatch: newStringP("i-aint-the-one")},
				{PrefixMatch: newStringP("192.168.1.1")},
				{PrefixMatch: newStringP("FOO.BAR")},
			},
		},
		{
			desc: "suffix match",
			sanMatchers: []xdsinternal.StringMatcher{
				{SuffixMatch: newStringP("i-aint-the-one")},
				{SuffixMatch: newStringP("1::68")},
				{SuffixMatch: newStringP(".COM")},
			},
		},
		{
			desc: "regex match",
			sanMatchers: []xdsinternal.StringMatcher{
				{RegexMatch: regexp.MustCompile(`.*\.examples\.com`)},
				{RegexMatch: regexp.MustCompile(`192\.[0-9]{1,3}\.1\.1`)},
			},
		},
		{
			desc: "contains match",
			sanMatchers: []xdsinternal.StringMatcher{
				{ContainsMatch: newStringP("i-aint-the-one")},
				{ContainsMatch: newStringP("2001:db8:1:1::68")},
				{ContainsMatch: newStringP("GRPC")},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			hi := NewHandshakeInfo(nil, nil)
			hi.SetSANMatchers(test.sanMatchers)

			if hi.MatchingSANExists(inputCert) {
				t.Fatalf("hi.MatchingSANExists(%+v) with SAN matchers +%v succeeded when expected to fail", inputCert, test.sanMatchers)
			}
		})
	}
}

func TestMatchingSANExists_Success(t *testing.T) {
	url1, err := url.Parse("http://golang.org")
	if err != nil {
		t.Fatalf("url.Parse() failed: %v", err)
	}
	url2, err := url.Parse("https://github.com/grpc/grpc-go")
	if err != nil {
		t.Fatalf("url.Parse() failed: %v", err)
	}
	inputCert := &x509.Certificate{
		DNSNames:       []string{"baz.test.com", "*.example.com"},
		EmailAddresses: []string{"foobar@example.com", "barbaz@test.com"},
		IPAddresses:    []net.IP{net.ParseIP("192.0.0.1"), net.ParseIP("2001:db8::68")},
		URIs:           []*url.URL{url1, url2},
	}

	tests := []struct {
		desc        string
		sanMatchers []xdsinternal.StringMatcher
	}{
		{
			desc: "no san matchers",
		},
		{
			desc: "exact match dns wildcard",
			sanMatchers: []xdsinternal.StringMatcher{
				{PrefixMatch: newStringP("192.168.1.1")},
				{ExactMatch: newStringP("https://github.com/grpc/grpc-java")},
				{ExactMatch: newStringP("abc.example.com")},
			},
		},
		{
			desc: "exact match ignore case",
			sanMatchers: []xdsinternal.StringMatcher{
				{
					ExactMatch: newStringP("FOOBAR@EXAMPLE.COM"),
					IgnoreCase: true,
				},
			},
		},
		{
			desc: "prefix match",
			sanMatchers: []xdsinternal.StringMatcher{
				{SuffixMatch: newStringP(".co.in")},
				{PrefixMatch: newStringP("192.168.1.1")},
				{PrefixMatch: newStringP("baz.test")},
			},
		},
		{
			desc: "prefix match ignore case",
			sanMatchers: []xdsinternal.StringMatcher{
				{
					PrefixMatch: newStringP("BAZ.test"),
					IgnoreCase:  true,
				},
			},
		},
		{
			desc: "suffix  match",
			sanMatchers: []xdsinternal.StringMatcher{
				{RegexMatch: regexp.MustCompile(`192\.[0-9]{1,3}\.1\.1`)},
				{SuffixMatch: newStringP("192.168.1.1")},
				{SuffixMatch: newStringP("@test.com")},
			},
		},
		{
			desc: "suffix  match ignore case",
			sanMatchers: []xdsinternal.StringMatcher{
				{
					SuffixMatch: newStringP("@test.COM"),
					IgnoreCase:  true,
				},
			},
		},
		{
			desc: "regex match",
			sanMatchers: []xdsinternal.StringMatcher{
				{ContainsMatch: newStringP("https://github.com/grpc/grpc-java")},
				{RegexMatch: regexp.MustCompile(`192\.[0-9]{1,3}\.1\.1`)},
				{RegexMatch: regexp.MustCompile(`.*\.test\.com`)},
			},
		},
		{
			desc: "contains match",
			sanMatchers: []xdsinternal.StringMatcher{
				{ExactMatch: newStringP("https://github.com/grpc/grpc-java")},
				{ContainsMatch: newStringP("2001:68::db8")},
				{ContainsMatch: newStringP("192.0.0")},
			},
		},
		{
			desc: "contains match ignore case",
			sanMatchers: []xdsinternal.StringMatcher{
				{
					ContainsMatch: newStringP("GRPC"),
					IgnoreCase:    true,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			hi := NewHandshakeInfo(nil, nil)
			hi.SetSANMatchers(test.sanMatchers)

			if !hi.MatchingSANExists(inputCert) {
				t.Fatalf("hi.MatchingSANExists(%+v) with SAN matchers +%v failed when expected to succeed", inputCert, test.sanMatchers)
			}
		})
	}
}

func newStringP(s string) *string {
	return &s
}
