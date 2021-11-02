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

package xdsclient

import (
	"context"
	"testing"

	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/internal/grpcutil"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"
)

func TestAndMatcherMatch(t *testing.T) {
	tests := []struct {
		name string
		pm   pathMatcher
		hm   matcher.HeaderMatcher
		info iresolver.RPCInfo
		want bool
	}{
		{
			name: "both match",
			pm:   newPathExactMatcher("/a/b", false),
			hm:   matcher.NewHeaderExactMatcher("th", "tv", false),
			info: iresolver.RPCInfo{
				Method:  "/a/b",
				Context: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: true,
		},
		{
			name: "both match with path case insensitive",
			pm:   newPathExactMatcher("/A/B", true),
			hm:   matcher.NewHeaderExactMatcher("th", "tv", false),
			info: iresolver.RPCInfo{
				Method:  "/a/b",
				Context: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: true,
		},
		{
			name: "only one match",
			pm:   newPathExactMatcher("/a/b", false),
			hm:   matcher.NewHeaderExactMatcher("th", "tv", false),
			info: iresolver.RPCInfo{
				Method:  "/z/y",
				Context: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: false,
		},
		{
			name: "both not match",
			pm:   newPathExactMatcher("/z/y", false),
			hm:   matcher.NewHeaderExactMatcher("th", "abc", false),
			info: iresolver.RPCInfo{
				Method:  "/a/b",
				Context: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: false,
		},
		{
			name: "fake header",
			pm:   newPathPrefixMatcher("/", false),
			hm:   matcher.NewHeaderExactMatcher("content-type", "fake", false),
			info: iresolver.RPCInfo{
				Method: "/a/b",
				Context: grpcutil.WithExtraMetadata(context.Background(), metadata.Pairs(
					"content-type", "fake",
				)),
			},
			want: true,
		},
		{
			name: "binary header",
			pm:   newPathPrefixMatcher("/", false),
			hm:   matcher.NewHeaderPresentMatcher("t-bin", true, false),
			info: iresolver.RPCInfo{
				Method: "/a/b",
				Context: grpcutil.WithExtraMetadata(
					metadata.NewOutgoingContext(context.Background(), metadata.Pairs("t-bin", "123")), metadata.Pairs(
						"content-type", "fake",
					)),
			},
			// Shouldn't match binary header, even though it's in metadata.
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newCompositeMatcher(tt.pm, []matcher.HeaderMatcher{tt.hm}, nil)
			if got := a.Match(tt.info); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFractionMatcherMatch(t *testing.T) {
	const fraction = 500000
	fm := newFractionMatcher(fraction)
	defer func() {
		RandInt63n = grpcrand.Int63n
	}()

	// rand > fraction, should return false.
	RandInt63n = func(n int64) int64 {
		return fraction + 1
	}
	if matched := fm.match(); matched {
		t.Errorf("match() = %v, want not match", matched)
	}

	// rand == fraction, should return true.
	RandInt63n = func(n int64) int64 {
		return fraction
	}
	if matched := fm.match(); !matched {
		t.Errorf("match() = %v, want match", matched)
	}

	// rand < fraction, should return true.
	RandInt63n = func(n int64) int64 {
		return fraction - 1
	}
	if matched := fm.match(); !matched {
		t.Errorf("match() = %v, want match", matched)
	}
}

func (s) TestMatchTypeForDomain(t *testing.T) {
	tests := []struct {
		d    string
		want domainMatchType
	}{
		{d: "", want: domainMatchTypeInvalid},
		{d: "*", want: domainMatchTypeUniversal},
		{d: "bar.*", want: domainMatchTypePrefix},
		{d: "*.abc.com", want: domainMatchTypeSuffix},
		{d: "foo.bar.com", want: domainMatchTypeExact},
		{d: "foo.*.com", want: domainMatchTypeInvalid},
	}
	for _, tt := range tests {
		if got := matchTypeForDomain(tt.d); got != tt.want {
			t.Errorf("matchTypeForDomain(%q) = %v, want %v", tt.d, got, tt.want)
		}
	}
}

func (s) TestMatch(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		host        string
		wantTyp     domainMatchType
		wantMatched bool
	}{
		{name: "invalid-empty", domain: "", host: "", wantTyp: domainMatchTypeInvalid, wantMatched: false},
		{name: "invalid", domain: "a.*.b", host: "", wantTyp: domainMatchTypeInvalid, wantMatched: false},
		{name: "universal", domain: "*", host: "abc.com", wantTyp: domainMatchTypeUniversal, wantMatched: true},
		{name: "prefix-match", domain: "abc.*", host: "abc.123", wantTyp: domainMatchTypePrefix, wantMatched: true},
		{name: "prefix-no-match", domain: "abc.*", host: "abcd.123", wantTyp: domainMatchTypePrefix, wantMatched: false},
		{name: "suffix-match", domain: "*.123", host: "abc.123", wantTyp: domainMatchTypeSuffix, wantMatched: true},
		{name: "suffix-no-match", domain: "*.123", host: "abc.1234", wantTyp: domainMatchTypeSuffix, wantMatched: false},
		{name: "exact-match", domain: "foo.bar", host: "foo.bar", wantTyp: domainMatchTypeExact, wantMatched: true},
		{name: "exact-no-match", domain: "foo.bar.com", host: "foo.bar", wantTyp: domainMatchTypeExact, wantMatched: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotTyp, gotMatched := match(tt.domain, tt.host); gotTyp != tt.wantTyp || gotMatched != tt.wantMatched {
				t.Errorf("match() = %v, %v, want %v, %v", gotTyp, gotMatched, tt.wantTyp, tt.wantMatched)
			}
		})
	}
}
