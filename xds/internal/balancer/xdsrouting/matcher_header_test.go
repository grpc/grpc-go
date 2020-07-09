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
	"google.golang.org/grpc/metadata"
)

func TestHeaderExactMatcherMatch(t *testing.T) {
	tests := []struct {
		name       string
		key, exact string
		info       balancer.PickInfo
		want       bool
	}{
		{
			name:  "one value one match",
			key:   "th",
			exact: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: true,
		},
		{
			name:  "two value one match",
			key:   "th",
			exact: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "abc", "th", "tv")),
			},
			want: true,
		},
		{
			name:  "not match",
			key:   "th",
			exact: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "abc")),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hem := newHeaderExactMatcher(tt.key, tt.exact)
			if got := hem.match(tt.info); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderRegexMatcherMatch(t *testing.T) {
	tests := []struct {
		name          string
		key, regexStr string
		info          balancer.PickInfo
		want          bool
	}{
		{
			name:     "one value one match",
			key:      "th",
			regexStr: "^t+v*$",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tttvv")),
			},
			want: true,
		},
		{
			name:     "two value one match",
			key:      "th",
			regexStr: "^t+v*$",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "abc", "th", "tttvv")),
			},
			want: true,
		},
		{
			name:     "no match",
			key:      "th",
			regexStr: "^t+v*$",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "abc")),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hrm := newHeaderRegexMatcher(tt.key, tt.regexStr)
			if got := hrm.match(tt.info); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderRangeMatcherMatch(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		start, end int64
		info       balancer.PickInfo
		want       bool
	}{
		{
			name:  "match",
			key:   "th",
			start: 1, end: 10,
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "5")),
			},
			want: true,
		},
		{
			name:  "equal to start",
			key:   "th",
			start: 1, end: 10,
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "1")),
			},
			want: true,
		},
		{
			name:  "equal to end",
			key:   "th",
			start: 1, end: 10,
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "10")),
			},
			want: false,
		},
		{
			name:  "negative",
			key:   "th",
			start: -10, end: 10,
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "-5")),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hrm := newHeaderRangeMatcher(tt.key, tt.start, tt.end)
			if got := hrm.match(tt.info); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderPresentMatcherMatch(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		present bool
		info    balancer.PickInfo
		want    bool
	}{
		{
			name:    "want present is present",
			key:     "th",
			present: true,
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: true,
		},
		{
			name:    "want present not present",
			key:     "th",
			present: true,
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("abc", "tv")),
			},
			want: false,
		},
		{
			name:    "want not present is present",
			key:     "th",
			present: false,
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv")),
			},
			want: false,
		},
		{
			name:    "want not present is not present",
			key:     "th",
			present: false,
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("abc", "tv")),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpm := newHeaderPresentMatcher(tt.key, tt.present)
			if got := hpm.match(tt.info); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderPrefixMatcherMatch(t *testing.T) {
	tests := []struct {
		name        string
		key, prefix string
		info        balancer.PickInfo
		want        bool
	}{
		{
			name:   "one value one match",
			key:    "th",
			prefix: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "tv123")),
			},
			want: true,
		},
		{
			name:   "two value one match",
			key:    "th",
			prefix: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "abc", "th", "tv123")),
			},
			want: true,
		},
		{
			name:   "not match",
			key:    "th",
			prefix: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "abc")),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpm := newHeaderPrefixMatcher(tt.key, tt.prefix)
			if got := hpm.match(tt.info); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderSuffixMatcherMatch(t *testing.T) {
	tests := []struct {
		name        string
		key, suffix string
		info        balancer.PickInfo
		want        bool
	}{
		{
			name:   "one value one match",
			key:    "th",
			suffix: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "123tv")),
			},
			want: true,
		},
		{
			name:   "two value one match",
			key:    "th",
			suffix: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "abc", "th", "123tv")),
			},
			want: true,
		},
		{
			name:   "not match",
			key:    "th",
			suffix: "tv",
			info: balancer.PickInfo{
				Ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("th", "abc")),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hsm := newHeaderSuffixMatcher(tt.key, tt.suffix)
			if got := hsm.match(tt.info); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}
