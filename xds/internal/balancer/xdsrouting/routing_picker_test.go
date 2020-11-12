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
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/xds/internal/testutils"
)

var (
	testPickers = []*testutils.TestConstPicker{
		{SC: testutils.TestSubConns[0]},
		{SC: testutils.TestSubConns[1]},
	}
)

func (s) TestRoutingPickerGroupPick(t *testing.T) {
	tests := []struct {
		name string

		routes  []route
		pickers map[string]*subBalancerState
		info    balancer.PickInfo

		want    balancer.PickResult
		wantErr error
	}{
		{
			name:    "empty",
			wantErr: errNoMatchedRouteFound,
		},
		{
			name: "one route no match",
			routes: []route{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/", false), nil, nil), action: "action-0"},
			},
			pickers: map[string]*subBalancerState{
				"action-0": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
			},
			info:    balancer.PickInfo{FullMethodName: "/z/y"},
			wantErr: errNoMatchedRouteFound,
		},
		{
			name: "one route one match",
			routes: []route{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/", false), nil, nil), action: "action-0"},
			},
			pickers: map[string]*subBalancerState{
				"action-0": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
			},
			info: balancer.PickInfo{FullMethodName: "/a/b"},
			want: balancer.PickResult{SubConn: testutils.TestSubConns[0]},
		},
		{
			name: "two routes first match",
			routes: []route{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/", false), nil, nil), action: "action-0"},
				{m: newCompositeMatcher(newPathPrefixMatcher("/z/", false), nil, nil), action: "action-1"},
			},
			pickers: map[string]*subBalancerState{
				"action-0": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
				"action-1": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[1],
				}},
			},
			info: balancer.PickInfo{FullMethodName: "/a/b"},
			want: balancer.PickResult{SubConn: testutils.TestSubConns[0]},
		},
		{
			name: "two routes second match",
			routes: []route{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/", false), nil, nil), action: "action-0"},
				{m: newCompositeMatcher(newPathPrefixMatcher("/z/", false), nil, nil), action: "action-1"},
			},
			pickers: map[string]*subBalancerState{
				"action-0": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
				"action-1": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[1],
				}},
			},
			info: balancer.PickInfo{FullMethodName: "/z/y"},
			want: balancer.PickResult{SubConn: testutils.TestSubConns[1]},
		},
		{
			name: "two routes both match former more specific",
			routes: []route{
				{m: newCompositeMatcher(newPathExactMatcher("/a/b", false), nil, nil), action: "action-0"},
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/", false), nil, nil), action: "action-1"},
			},
			pickers: map[string]*subBalancerState{
				"action-0": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
				"action-1": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[1],
				}},
			},
			info: balancer.PickInfo{FullMethodName: "/a/b"},
			// First route is a match, so first action is picked.
			want: balancer.PickResult{SubConn: testutils.TestSubConns[0]},
		},
		{
			name: "tow routes both match latter more specific",
			routes: []route{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/", false), nil, nil), action: "action-0"},
				{m: newCompositeMatcher(newPathExactMatcher("/a/b", false), nil, nil), action: "action-1"},
			},
			pickers: map[string]*subBalancerState{
				"action-0": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
				"action-1": {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[1],
				}},
			},
			info: balancer.PickInfo{FullMethodName: "/a/b"},
			// First route is a match, so first action is picked, even though
			// second is an exact match.
			want: balancer.PickResult{SubConn: testutils.TestSubConns[0]},
		},
	}
	cmpOpts := []cmp.Option{cmp.AllowUnexported(testutils.TestSubConn{})}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := newPickerGroup(tt.routes, tt.pickers)
			got, err := pg.Pick(tt.info)
			t.Logf("Pick(%+v) = {%+v, %+v}", tt.info, got, err)
			if err != tt.wantErr {
				t.Errorf("Pick() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !cmp.Equal(got, tt.want, cmpOpts...) {
				t.Errorf("Pick() got = %v, want %v, diff %s", got, tt.want, cmp.Diff(got, tt.want, cmpOpts...))
			}
		})
	}

}
