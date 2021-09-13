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

package xdsclient

import (
	"regexp"
	"testing"
	"time"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/httpfilter"
)

type customfilterCfg struct {
	config string
}

func (fc customfilterCfg) Equal(x httpfilter.FilterConfig) bool {
	other, ok := x.(customfilterCfg)
	if !ok {
		return false
	}
	return fc.config == other.config
}

type customFilter struct {
	httpfilter.Filter
	urls []string
}

func (f customFilter) TypeURLs() []string {
	return f.urls
}

func TestListenerUpdate_Equal(t *testing.T) {
	tests := []struct {
		name string
		x    ListenerUpdate
		y    ListenerUpdate
		want bool
	}{
		{
			name: "empty struct",
			want: true,
		},
		{
			name: "diff in RouteConfigName field",
			x:    ListenerUpdate{RouteConfigName: "foo"},
			y:    ListenerUpdate{RouteConfigName: "bar"},
			want: false,
		},
		{
			name: "diff in InlineRouteConfig field",
			x: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
			},
			y:    ListenerUpdate{RouteConfigName: "foo"},
			want: false,
		},
		{
			name: "diff in MaxStreamDuration field",
			x: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
			},
			y: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Millisecond,
			},
			want: false,
		},
		{
			name: "diff in HTTPFilter field - different number of elements",
			x: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
				HTTPFilters: []HTTPFilter{
					{
						Name:   "f1",
						Filter: customFilter{urls: []string{"u1"}},
						Config: customfilterCfg{config: "config1"},
					},
					{
						Name:   "f2",
						Filter: customFilter{urls: []string{"u2"}},
						Config: customfilterCfg{config: "config2"},
					},
				},
			},
			y: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
				HTTPFilters: []HTTPFilter{
					{
						Name:   "f1",
						Filter: customFilter{urls: []string{"u1"}},
						Config: customfilterCfg{config: "config1"},
					},
				},
			},
			want: false,
		},
		{
			name: "diff in HTTPFilter field - different contents",
			x: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
				HTTPFilters: []HTTPFilter{
					{
						Name:   "f1",
						Filter: customFilter{urls: []string{"u1"}},
						Config: customfilterCfg{config: "config1"},
					},
				},
			},
			y: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
				HTTPFilters: []HTTPFilter{
					{
						Name:   "f2",
						Filter: customFilter{urls: []string{"u2"}},
						Config: customfilterCfg{config: "config2"},
					},
				},
			},
			want: false,
		},
		{
			name: "diff in InboundListenerConfig field",
			x: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
				HTTPFilters: []HTTPFilter{
					{
						Name:   "f1",
						Filter: customFilter{urls: []string{"u1"}},
						Config: customfilterCfg{config: "config1"},
					},
				},
				InboundListenerCfg: &InboundListenerConfig{Address: "1.2.3.4"},
			},
			y: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
				HTTPFilters: []HTTPFilter{
					{
						Name:   "f1",
						Filter: customFilter{urls: []string{"u1"}},
						Config: customfilterCfg{config: "config1"},
					},
				},
			},
			want: false,
		},
		{
			name: "equal",
			x: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
				HTTPFilters: []HTTPFilter{
					{
						Name:   "f1",
						Filter: customFilter{urls: []string{"u1"}},
						Config: customfilterCfg{config: "config1"},
					},
				},
				InboundListenerCfg: &InboundListenerConfig{Address: "1.2.3.4"},
			},
			y: ListenerUpdate{
				RouteConfigName:   "foo",
				InlineRouteConfig: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"a.b.c"}}}},
				MaxStreamDuration: time.Second,
				HTTPFilters: []HTTPFilter{
					{
						Name:   "f1",
						Filter: customFilter{urls: []string{"u1"}},
						Config: customfilterCfg{config: "config1"},
					},
				},
				InboundListenerCfg: &InboundListenerConfig{Address: "1.2.3.4"},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("ListenerUpdate.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestHTTPFilter_Equal(t *testing.T) {
	tests := []struct {
		name string
		x    HTTPFilter
		y    HTTPFilter
		want bool
	}{
		{
			name: "empty struct",
			want: true,
		},
		{
			name: "diff in Name field",
			x:    HTTPFilter{Name: "foo"},
			y:    HTTPFilter{Name: "bar"},
			want: false,
		},
		{
			name: "diff in Filter field",
			x: HTTPFilter{
				Name:   "foo",
				Filter: customFilter{urls: []string{"u1"}},
			},
			y: HTTPFilter{
				Name:   "foo",
				Filter: customFilter{urls: []string{"u2"}},
			},
			want: false,
		},
		{
			name: "diff in Config field",
			x: HTTPFilter{
				Name:   "foo",
				Filter: customFilter{urls: []string{"u1"}},
				Config: customfilterCfg{config: "config1"},
			},
			y: HTTPFilter{
				Name:   "foo",
				Filter: customFilter{urls: []string{"u1"}},
				Config: customfilterCfg{config: "config2"},
			},
			want: false,
		},
		{
			name: "equal",
			x: HTTPFilter{
				Name:   "foo",
				Filter: customFilter{urls: []string{"u1"}},
				Config: customfilterCfg{config: "config1"},
			},
			y: HTTPFilter{
				Name:   "foo",
				Filter: customFilter{urls: []string{"u1"}},
				Config: customfilterCfg{config: "config1"},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("HTTPFilter.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestInboundListenerConfig_Equal(t *testing.T) {
	fc, err := NewFilterChainManager(&v3listenerpb.Listener{
		FilterChains: []*v3listenerpb.FilterChain{{
			Filters: []*v3listenerpb.Filter{{
				Name: "hcm",
				ConfigType: &v3listenerpb.Filter_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
						HttpFilters: []*v3httppb.HttpFilter{
							validServerSideHTTPFilter1,
							emptyRouterFilter,
						},
					}),
				}}}}},
	})
	if err != nil {
		t.Fatalf("NewFilterChainManager() failed: %v", err)
	}

	tests := []struct {
		name string
		x    *InboundListenerConfig
		y    *InboundListenerConfig
		want bool
	}{
		{
			name: "nils",
			want: true,
		},
		{
			name: "diff in Address field",
			x:    &InboundListenerConfig{Address: "1.2.3.4"},
			y:    &InboundListenerConfig{Address: "a.b.c.d"},
			want: false,
		},
		{
			name: "diff in Port field",
			x: &InboundListenerConfig{
				Address: "1.2.3.4",
				Port:    "1",
			},
			y: &InboundListenerConfig{
				Address: "1.2.3.4",
				Port:    "2",
			},
			want: false,
		},
		{
			name: "diff in FilterChains field",
			x: &InboundListenerConfig{
				Address:      "1.2.3.4",
				Port:         "1",
				FilterChains: fc,
			},
			y: &InboundListenerConfig{
				Address: "1.2.3.4",
				Port:    "1",
			},
			want: false,
		},
		{
			name: "equal",
			x: &InboundListenerConfig{
				Address:      "1.2.3.4",
				Port:         "1",
				FilterChains: fc,
			},
			y: &InboundListenerConfig{
				Address:      "1.2.3.4",
				Port:         "1",
				FilterChains: fc,
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("InboundListenerConfig.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestRouteConfigUpdate_Equal(t *testing.T) {
	tests := []struct {
		name string
		x    *RouteConfigUpdate
		y    *RouteConfigUpdate
		want bool
	}{
		{
			name: "nils",
			want: true,
		},
		{
			name: "diff in VirtualHosts field - different number of elements",
			x:    &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"foo.com"}}}},
			y: &RouteConfigUpdate{VirtualHosts: []*VirtualHost{
				{Domains: []string{"foo.com"}},
				{Domains: []string{"bar.com"}},
			}},
			want: false,
		},
		{
			name: "diff in VirtualHosts field - different contents",
			x:    &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"foo.com"}}}},
			y:    &RouteConfigUpdate{VirtualHosts: []*VirtualHost{{Domains: []string{"bar.com"}}}},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("RouteConfigUpdate.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestVirtualHost_Equal(t *testing.T) {
	tests := []struct {
		name string
		x    *VirtualHost
		y    *VirtualHost
		want bool
	}{
		{
			name: "nils",
			want: true,
		},
		{
			name: "diff in Domains field",
			x:    &VirtualHost{Domains: []string{"foo.com"}},
			y:    &VirtualHost{Domains: []string{"bar.com"}},
			want: false,
		},
		{
			name: "diff in Routes field - different number of elements",
			x: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
			},
			y:    &VirtualHost{Domains: []string{"foo.com"}},
			want: false,
		},
		{
			name: "diff in Routes field - different contents",
			x: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
			},
			y: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: false}},
			},
			want: false,
		},
		{
			name: "diff in HTTPFilterConfigOverride field - different number of elements",
			x: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "config"},
				},
			},
			y: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
			},
			want: false,
		},
		{
			name: "diff in HTTPFilterConfigOverride field - different contents",
			x: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
			},
			y: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"bar": customfilterCfg{config: "configFoo"},
					"foo": customfilterCfg{config: "configBar"},
				},
			},
			want: false,
		},
		{
			name: "diff in RetryConfig field presence",
			x: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
			},
			y: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
			},
			want: false,
		},
		{
			name: "diff in RetryConfig field contents",
			x: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
			},
			y: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 2},
			},
			want: false,
		},
		{
			name: "equal",
			x: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
			},
			y: &VirtualHost{
				Domains: []string{"foo.com"},
				Routes:  []*Route{{CaseInsensitive: true}},
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("VirtualHost.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestRetryConfig_Equal(t *testing.T) {
	tests := []struct {
		name string
		x    *RetryConfig
		y    *RetryConfig
		want bool
	}{
		{
			name: "nils",
			want: true,
		},
		{
			name: "diff in RetryOn field - different number of elements",
			x: &RetryConfig{
				RetryOn: map[codes.Code]bool{
					codes.DeadlineExceeded: true,
					codes.Canceled:         true,
				},
			},
			y: &RetryConfig{
				RetryOn: map[codes.Code]bool{codes.Canceled: true},
			},
			want: false,
		},
		{
			name: "diff in RetryOn field - different contents",
			x: &RetryConfig{
				RetryOn: map[codes.Code]bool{codes.DeadlineExceeded: true},
			},
			y: &RetryConfig{
				RetryOn: map[codes.Code]bool{codes.Canceled: true},
			},
			want: false,
		},
		{
			name: "diff in NumRetries field",
			x: &RetryConfig{
				RetryOn:    map[codes.Code]bool{codes.DeadlineExceeded: true},
				NumRetries: 1,
			},
			y: &RetryConfig{
				RetryOn:    map[codes.Code]bool{codes.DeadlineExceeded: true},
				NumRetries: 2,
			},
			want: false,
		},
		{
			name: "diff in RetryBackoff field",
			x: &RetryConfig{
				RetryOn:      map[codes.Code]bool{codes.DeadlineExceeded: true},
				NumRetries:   1,
				RetryBackoff: RetryBackoff{BaseInterval: time.Second},
			},
			y: &RetryConfig{
				RetryOn:      map[codes.Code]bool{codes.DeadlineExceeded: true},
				NumRetries:   1,
				RetryBackoff: RetryBackoff{BaseInterval: time.Minute},
			},
			want: false,
		},
		{
			name: "equal",
			x: &RetryConfig{
				RetryOn: map[codes.Code]bool{
					codes.DeadlineExceeded: true,
					codes.Canceled:         true,
				},
				NumRetries: 1,
				RetryBackoff: RetryBackoff{
					BaseInterval: time.Second,
					MaxInterval:  time.Minute,
				},
			},
			y: &RetryConfig{
				RetryOn: map[codes.Code]bool{
					codes.DeadlineExceeded: true,
					codes.Canceled:         true,
				},
				NumRetries: 1,
				RetryBackoff: RetryBackoff{
					BaseInterval: time.Second,
					MaxInterval:  time.Minute,
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("RetryConfig.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestRetryBackoff_Equal(t *testing.T) {
	tests := []struct {
		name string
		x    RetryBackoff
		y    RetryBackoff
		want bool
	}{
		{
			name: "empty struct",
			want: true,
		},
		{
			name: "diff in BaseInterval field",
			x:    RetryBackoff{BaseInterval: time.Second},
			y:    RetryBackoff{BaseInterval: 2 * time.Second},
			want: false,
		},
		{
			name: "diff in MaxInterval field",
			x: RetryBackoff{
				BaseInterval: time.Second,
				MaxInterval:  time.Minute,
			},
			y: RetryBackoff{
				BaseInterval: time.Second,
				MaxInterval:  2 * time.Minute,
			},
			want: false,
		},
		{
			name: "equal",
			x: RetryBackoff{
				BaseInterval: time.Second,
				MaxInterval:  time.Minute,
			},
			y: RetryBackoff{
				BaseInterval: time.Second,
				MaxInterval:  time.Minute,
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("RetryBackoff.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestHashPolicy_Equal(t *testing.T) {
	re1 := regexp.MustCompile("^regex$")
	re2 := regexp.MustCompile("^re2$")

	tests := []struct {
		name string
		x    *HashPolicy
		y    *HashPolicy
		want bool
	}{
		{
			name: "nils",
			want: true,
		},
		{
			name: "diff in HashPolicyType field",
			x:    &HashPolicy{HashPolicyType: HashPolicyTypeHeader},
			y:    &HashPolicy{HashPolicyType: HashPolicyTypeChannelID},
			want: false,
		},
		{
			name: "diff in Terminal field",
			x: &HashPolicy{
				HashPolicyType: HashPolicyTypeHeader,
				Terminal:       true,
			},
			y: &HashPolicy{
				HashPolicyType: HashPolicyTypeHeader,
				Terminal:       false,
			},
			want: false,
		},
		{
			name: "diff in HeaderName field",
			x: &HashPolicy{
				HashPolicyType: HashPolicyTypeHeader,
				Terminal:       true,
				HeaderName:     "foo",
			},
			y: &HashPolicy{
				HashPolicyType: HashPolicyTypeHeader,
				Terminal:       true,
				HeaderName:     "bar",
			},
			want: false,
		},
		{
			name: "diff in Regex field",
			x: &HashPolicy{
				HashPolicyType: HashPolicyTypeHeader,
				Terminal:       true,
				HeaderName:     "foo",
				Regex:          re1,
			},
			y: &HashPolicy{
				HashPolicyType: HashPolicyTypeHeader,
				Terminal:       true,
				HeaderName:     "foo",
				Regex:          re2,
			},
			want: false,
		},
		{
			name: "diff in RegexSubstitution field",
			x: &HashPolicy{
				HashPolicyType:    HashPolicyTypeHeader,
				Terminal:          true,
				HeaderName:        "foo",
				Regex:             re1,
				RegexSubstitution: "foo",
			},
			y: &HashPolicy{
				HashPolicyType:    HashPolicyTypeHeader,
				Terminal:          true,
				HeaderName:        "foo",
				Regex:             re1,
				RegexSubstitution: "bar",
			},
			want: false,
		},
		{
			name: "equal",
			x: &HashPolicy{
				HashPolicyType:    HashPolicyTypeHeader,
				Terminal:          true,
				HeaderName:        "foo",
				Regex:             re1,
				RegexSubstitution: "foo",
			},
			y: &HashPolicy{
				HashPolicyType:    HashPolicyTypeHeader,
				Terminal:          true,
				HeaderName:        "foo",
				Regex:             re1,
				RegexSubstitution: "foo",
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("HashPolicy.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestWeightedCluster_Equal(t *testing.T) {
	tests := []struct {
		name string
		x    WeightedCluster
		y    WeightedCluster
		want bool
	}{
		{
			name: "empty struct",
			want: true,
		},
		{
			name: "diff in Weight field",
			x:    WeightedCluster{Weight: 1},
			y:    WeightedCluster{Weight: 100},
			want: false,
		},
		{
			name: "diff in HTTPFilterConfigOverride field - different number of elements",
			x: WeightedCluster{
				Weight: 100,
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
			},
			y: WeightedCluster{
				Weight: 100,
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
				},
			},
			want: false,
		},
		{
			name: "diff in HTTPFilterConfigOverride field - different contents",
			x: WeightedCluster{
				Weight: 100,
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
			},
			y: WeightedCluster{
				Weight: 100,
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"bar": customfilterCfg{config: "configFoo"},
					"foo": customfilterCfg{config: "configBar"},
				},
			},
			want: false,
		},
		{
			name: "equal",
			x: WeightedCluster{
				Weight: 100,
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
			},
			y: WeightedCluster{
				Weight: 100,
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("WeightedCluster.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestHeaderMatcher_Equal(t *testing.T) {
	re1 := regexp.MustCompile("^regex$")
	re2 := regexp.MustCompile("^re2$")

	tests := []struct {
		name string
		x    *HeaderMatcher
		y    *HeaderMatcher
		want bool
	}{
		{
			name: "nils",
			want: true,
		},
		{
			name: "diff in Name field",
			x:    &HeaderMatcher{Name: "foo"},
			y:    &HeaderMatcher{Name: "bar"},
			want: false,
		},
		{
			name: "diff in InvertMatch field",
			x: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
			},
			y: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(false),
			},
			want: false,
		},
		{
			name: "diff in ExactMatch field",
			x: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
			},
			y: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("not-exact"),
			},
			want: false,
		},
		{
			name: "diff in RegexMatch field",
			x: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
				RegexMatch:  re1,
			},
			y: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
				RegexMatch:  re2,
			},
			want: false,
		},
		{
			name: "diff in PrefixMatch field",
			x: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
				RegexMatch:  re1,
				PrefixMatch: newStringP("prefix"),
			},
			y: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
				RegexMatch:  re1,
				PrefixMatch: newStringP("not-prefix"),
			},
			want: false,
		},
		{
			name: "diff in SuffixMatch field",
			x: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
				RegexMatch:  re1,
				PrefixMatch: newStringP("prefix"),
				SuffixMatch: newStringP("suffix"),
			},
			y: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
				RegexMatch:  re1,
				PrefixMatch: newStringP("prefix"),
				SuffixMatch: newStringP("not-suffix"),
			},
			want: false,
		},
		{
			name: "diff in RangeMatch  field",
			x: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
				RegexMatch:  re1,
				PrefixMatch: newStringP("prefix"),
				SuffixMatch: newStringP("suffix"),
				RangeMatch:  &Int64Range{Start: 1, End: 100},
			},
			y: &HeaderMatcher{
				Name:        "foo",
				InvertMatch: newBoolP(true),
				ExactMatch:  newStringP("exact"),
				RegexMatch:  re1,
				PrefixMatch: newStringP("prefix"),
				SuffixMatch: newStringP("suffix"),
				RangeMatch:  &Int64Range{Start: 2, End: 200},
			},
			want: false,
		},
		{
			name: "diff in PresentMatch field",
			x: &HeaderMatcher{
				Name:         "foo",
				InvertMatch:  newBoolP(true),
				ExactMatch:   newStringP("exact"),
				RegexMatch:   re1,
				PrefixMatch:  newStringP("prefix"),
				SuffixMatch:  newStringP("suffix"),
				RangeMatch:   &Int64Range{Start: 1, End: 100},
				PresentMatch: newBoolP(true),
			},
			y: &HeaderMatcher{
				Name:         "foo",
				InvertMatch:  newBoolP(true),
				ExactMatch:   newStringP("exact"),
				RegexMatch:   re1,
				PrefixMatch:  newStringP("prefix"),
				SuffixMatch:  newStringP("suffix"),
				RangeMatch:   &Int64Range{Start: 1, End: 100},
				PresentMatch: newBoolP(false),
			},
			want: false,
		},
		{
			name: "equal",
			x: &HeaderMatcher{
				Name:         "foo",
				InvertMatch:  newBoolP(true),
				ExactMatch:   newStringP("exact"),
				RegexMatch:   re1,
				PrefixMatch:  newStringP("prefix"),
				SuffixMatch:  newStringP("suffix"),
				RangeMatch:   &Int64Range{Start: 1, End: 100},
				PresentMatch: newBoolP(true),
			},
			y: &HeaderMatcher{
				Name:         "foo",
				InvertMatch:  newBoolP(true),
				ExactMatch:   newStringP("exact"),
				RegexMatch:   re1,
				PrefixMatch:  newStringP("prefix"),
				SuffixMatch:  newStringP("suffix"),
				RangeMatch:   &Int64Range{Start: 1, End: 100},
				PresentMatch: newBoolP(true),
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("HeaderMatcher.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestInt64Range_Equal(t *testing.T) {
	tests := []struct {
		name string
		x    *Int64Range
		y    *Int64Range
		want bool
	}{
		{
			name: "nils",
			want: true,
		},
		{
			name: "diff in Start field",
			x:    &Int64Range{Start: 1},
			y:    &Int64Range{Start: 2},
			want: false,
		},
		{
			name: "diff in End field",
			x:    &Int64Range{Start: 1, End: 100},
			y:    &Int64Range{Start: 1, End: 200},
			want: false,
		},
		{
			name: "equal",
			x:    &Int64Range{Start: 1, End: 100},
			y:    &Int64Range{Start: 1, End: 100},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("Int64Range.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}

func TestRoute_Equal(t *testing.T) {
	re1 := regexp.MustCompile("^regex$")
	re2 := regexp.MustCompile("^re2$")

	tests := []struct {
		name string
		x    *Route
		y    *Route
		want bool
	}{
		{
			name: "nils",
			want: true,
		},
		{
			name: "diff in Path field",
			x:    &Route{Path: newStringP("path")},
			y:    &Route{Path: newStringP("not-path")},
			want: false,
		},
		{
			name: "diff in Prefix field",
			x: &Route{
				Path:   newStringP("path"),
				Prefix: newStringP("prefix"),
			},
			y: &Route{
				Path:   newStringP("path"),
				Prefix: newStringP("not-prefix"),
			},
			want: false,
		},
		{
			name: "diff in Regex field",
			x: &Route{
				Path:   newStringP("path"),
				Prefix: newStringP("prefix"),
				Regex:  re1,
			},
			y: &Route{
				Path:   newStringP("path"),
				Prefix: newStringP("prefix"),
				Regex:  re2,
			},
			want: false,
		},
		{
			name: "diff in CaseInsensitive field",
			x: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
			},
			y: &Route{
				Path:   newStringP("path"),
				Prefix: newStringP("prefix"),
				Regex:  re1,
			},
			want: false,
		},
		{
			name: "diff in Headers field - different number of elements",
			x: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
			},
			y: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
			},
			want: false,
		},
		{
			name: "diff in Headers field - different contents",
			x: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
			},
			y: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "bar"}},
			},
			want: false,
		},
		{
			name: "diff in Fraction field",
			x: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
				Fraction:        newUInt32P(1),
			},
			y: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
				Fraction:        newUInt32P(2),
			},
			want: false,
		},
		{
			name: "diff in HashPolicies field - different number of elements",
			x: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
				Fraction:        newUInt32P(1),
				HashPolicies:    []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
			},
			y: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
				Fraction:        newUInt32P(1),
			},
			want: false,
		},
		{
			name: "diff in HashPolicies field - different contents",
			x: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
				Fraction:        newUInt32P(1),
				HashPolicies:    []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
			},
			y: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
				Fraction:        newUInt32P(1),
				HashPolicies:    []*HashPolicy{{HashPolicyType: HashPolicyTypeChannelID}},
			},
			want: false,
		},
		{
			name: "diff in WeightedClusters field - different number of elements",
			x: &Route{
				Path:             newStringP("path"),
				Prefix:           newStringP("prefix"),
				Regex:            re1,
				CaseInsensitive:  true,
				Headers:          []*HeaderMatcher{{Name: "foo"}},
				Fraction:         newUInt32P(1),
				HashPolicies:     []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters: map[string]WeightedCluster{"foo": {Weight: 100}},
			},
			y: &Route{
				Path:            newStringP("path"),
				Prefix:          newStringP("prefix"),
				Regex:           re1,
				CaseInsensitive: true,
				Headers:         []*HeaderMatcher{{Name: "foo"}},
				Fraction:        newUInt32P(1),
				HashPolicies:    []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
			},
			want: false,
		},
		{
			name: "diff in WeightedClusters field - different contents",
			x: &Route{
				Path:             newStringP("path"),
				Prefix:           newStringP("prefix"),
				Regex:            re1,
				CaseInsensitive:  true,
				Headers:          []*HeaderMatcher{{Name: "foo"}},
				Fraction:         newUInt32P(1),
				HashPolicies:     []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters: map[string]WeightedCluster{"foo": {Weight: 100}},
			},
			y: &Route{
				Path:             newStringP("path"),
				Prefix:           newStringP("prefix"),
				Regex:            re1,
				CaseInsensitive:  true,
				Headers:          []*HeaderMatcher{{Name: "foo"}},
				Fraction:         newUInt32P(1),
				HashPolicies:     []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters: map[string]WeightedCluster{"bar": {Weight: 100}},
			},
			want: false,
		},
		{
			name: "diff in MaxStreamDuration field",
			x: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
			},
			y: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(2 * time.Second),
			},
			want: false,
		},
		{
			name: "diff in HTTPFilterConfigOverride field - different number of elements",
			x: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
				},
			},
			y: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
			},
			want: false,
		},
		{
			name: "diff in HTTPFilterConfigOverride field - different contents",
			x: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
			},
			y: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"bar": customfilterCfg{config: "configFoo"},
					"foo": customfilterCfg{config: "configBar"},
				},
			},
			want: false,
		},
		{
			name: "diff in RetryConfig field",
			x: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
			},
			y: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 2},
			},
			want: false,
		},
		{
			name: "diff in RouteAction field",
			x: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
				RouteAction: RouteActionRoute,
			},
			y: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
				RouteAction: RouteActionNonForwardingAction,
			},
			want: false,
		},
		{
			name: "equal",
			x: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
				RouteAction: RouteActionRoute,
			},
			y: &Route{
				Path:              newStringP("path"),
				Prefix:            newStringP("prefix"),
				Regex:             re1,
				CaseInsensitive:   true,
				Headers:           []*HeaderMatcher{{Name: "foo"}},
				Fraction:          newUInt32P(1),
				HashPolicies:      []*HashPolicy{{HashPolicyType: HashPolicyTypeHeader}},
				WeightedClusters:  map[string]WeightedCluster{"foo": {Weight: 100}},
				MaxStreamDuration: newDurationP(time.Second),
				HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
					"foo": customfilterCfg{config: "configFoo"},
					"bar": customfilterCfg{config: "configBar"},
				},
				RetryConfig: &RetryConfig{NumRetries: 1},
				RouteAction: RouteActionRoute,
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.x.Equal(test.y); got != test.want {
				t.Fatalf("Route.Equal(%+v, %+v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}
