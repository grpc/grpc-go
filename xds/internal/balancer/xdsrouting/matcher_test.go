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

package xdsrouting

import (
	"context"
	"testing"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/internal/grpcutil"
	"google.golang.org/grpc/metadata"
)

func TestAndMatcherMatch(t *testing.T) {
	tests := []struct {
		name string
		pm   pathMatcherInterface
		hm   headerMatcherInterface
		info balancer.PickInfo
		want bool
	}{
		{
			name: "both match",
			pm:   newPathExactMatcher("/a/b", false),
			hm:   newHeaderExactMatcher("th", "tv"),
			info: balancer.PickInfo{
				FullMethodName: "/a/b",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: true,
		},
		{
			name: "both match with path case insensitive",
			pm:   newPathExactMatcher("/A/B", true),
			hm:   newHeaderExactMatcher("th", "tv"),
			info: balancer.PickInfo{
				FullMethodName: "/a/b",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: true,
		},
		{
			name: "only one match",
			pm:   newPathExactMatcher("/a/b", false),
			hm:   newHeaderExactMatcher("th", "tv"),
			info: balancer.PickInfo{
				FullMethodName: "/z/y",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: false,
		},
		{
			name: "both not match",
			pm:   newPathExactMatcher("/z/y", false),
			hm:   newHeaderExactMatcher("th", "abc"),
			info: balancer.PickInfo{
				FullMethodName: "/a/b",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: false,
		},
		{
			name: "fake header",
			pm:   newPathPrefixMatcher("/", false),
			hm:   newHeaderExactMatcher("content-type", "fake"),
			info: balancer.PickInfo{
				FullMethodName: "/a/b",
				Ctx: grpcutil.WithExtraMetadata(context.Background(), metadata.Pairs(
					"content-type", "fake",
				)),
			},
			want: true,
		},
		{
			name: "binary header",
			pm:   newPathPrefixMatcher("/", false),
			hm:   newHeaderPresentMatcher("t-bin", true),
			info: balancer.PickInfo{
				FullMethodName: "/a/b",
				Ctx: grpcutil.WithExtraMetadata(
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
			a := newCompositeMatcher(tt.pm, []headerMatcherInterface{tt.hm}, nil)
			if got := a.match(tt.info); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFractionMatcherMatch(t *testing.T) {
	const fraction = 500000
	fm := newFractionMatcher(fraction)
	defer func() {
		grpcrandInt63n = grpcrand.Int63n
	}()

	// rand > fraction, should return false.
	grpcrandInt63n = func(n int64) int64 {
		return fraction + 1
	}
	if matched := fm.match(); matched {
		t.Errorf("match() = %v, want not match", matched)
	}

	// rand == fraction, should return true.
	grpcrandInt63n = func(n int64) int64 {
		return fraction
	}
	if matched := fm.match(); !matched {
		t.Errorf("match() = %v, want match", matched)
	}

	// rand < fraction, should return true.
	grpcrandInt63n = func(n int64) int64 {
		return fraction - 1
	}
	if matched := fm.match(); !matched {
		t.Errorf("match() = %v, want match", matched)
	}
}

func TestCompositeMatcherEqual(t *testing.T) {
	tests := []struct {
		name string
		pm   pathMatcherInterface
		hms  []headerMatcherInterface
		fm   *fractionMatcher
		mm   *compositeMatcher
		want bool
	}{
		{
			name: "equal",
			pm:   newPathExactMatcher("/a/b", false),
			mm:   newCompositeMatcher(newPathExactMatcher("/a/b", false), nil, nil),
			want: true,
		},
		{
			name: "no path matcher",
			pm:   nil,
			mm:   newCompositeMatcher(nil, nil, nil),
			want: true,
		},
		{
			name: "not equal",
			pm:   newPathExactMatcher("/a/b", false),
			fm:   newFractionMatcher(123),
			mm:   newCompositeMatcher(newPathExactMatcher("/a/b", false), nil, nil),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newCompositeMatcher(tt.pm, tt.hms, tt.fm)
			if got := a.equal(tt.mm); got != tt.want {
				t.Errorf("equal() = %v, want %v", got, tt.want)
			}
		})
	}
}
