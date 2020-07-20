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
	"reflect"
	"testing"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/testutils"
)

var (
	testPickers = []*testutils.TestConstPicker{
		{SC: testutils.TestSubConns[0]},
		{SC: testutils.TestSubConns[1]},
	}
)

func TestRoutingPickerGroupPick(t *testing.T) {
	tests := []struct {
		name string

		routes  []pickerRoute
		pickers map[internal.LocalityID]*subBalancerState
		info    balancer.PickInfo

		want    balancer.PickResult
		wantErr bool
	}{
		{
			name:    "empty",
			wantErr: true,
		},
		{
			name: "one route no match",
			routes: []pickerRoute{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/"), nil, nil), subBalancerID: "action-0"},
			},
			pickers: map[internal.LocalityID]*subBalancerState{
				makeLocalityFromName("action-0"): {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
			},
			info:    balancer.PickInfo{FullMethodName: "/z/y"},
			wantErr: true,
		},
		{
			name: "one route one match",
			routes: []pickerRoute{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/"), nil, nil), subBalancerID: "action-0"},
			},
			pickers: map[internal.LocalityID]*subBalancerState{
				makeLocalityFromName("action-0"): {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
			},
			info: balancer.PickInfo{FullMethodName: "/a/b"},
			want: balancer.PickResult{SubConn: testutils.TestSubConns[0]},
		},
		{
			name: "two routes first match",
			routes: []pickerRoute{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/"), nil, nil), subBalancerID: "action-0"},
				{m: newCompositeMatcher(newPathPrefixMatcher("/z/"), nil, nil), subBalancerID: "action-1"},
			},
			pickers: map[internal.LocalityID]*subBalancerState{
				makeLocalityFromName("action-0"): {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
				makeLocalityFromName("action-1"): {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[1],
				}},
			},
			info: balancer.PickInfo{FullMethodName: "/a/b"},
			want: balancer.PickResult{SubConn: testutils.TestSubConns[0]},
		},
		{
			name: "two routes second match",
			routes: []pickerRoute{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/"), nil, nil), subBalancerID: "action-0"},
				{m: newCompositeMatcher(newPathPrefixMatcher("/z/"), nil, nil), subBalancerID: "action-1"},
			},
			pickers: map[internal.LocalityID]*subBalancerState{
				makeLocalityFromName("action-0"): {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
				makeLocalityFromName("action-1"): {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[1],
				}},
			},
			info: balancer.PickInfo{FullMethodName: "/z/y"},
			want: balancer.PickResult{SubConn: testutils.TestSubConns[1]},
		},
		{
			name: "two routes both match former more specific",
			routes: []pickerRoute{
				{m: newCompositeMatcher(newPathExactMatcher("/a/b"), nil, nil), subBalancerID: "action-0"},
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/"), nil, nil), subBalancerID: "action-1"},
			},
			pickers: map[internal.LocalityID]*subBalancerState{
				makeLocalityFromName("action-0"): {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
				makeLocalityFromName("action-1"): {state: balancer.State{
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
			routes: []pickerRoute{
				{m: newCompositeMatcher(newPathPrefixMatcher("/a/"), nil, nil), subBalancerID: "action-0"},
				{m: newCompositeMatcher(newPathExactMatcher("/a/b"), nil, nil), subBalancerID: "action-1"},
			},
			pickers: map[internal.LocalityID]*subBalancerState{
				makeLocalityFromName("action-0"): {state: balancer.State{
					ConnectivityState: connectivity.Ready,
					Picker:            testPickers[0],
				}},
				makeLocalityFromName("action-1"): {state: balancer.State{
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := newPickerGroup(tt.routes, tt.pickers)
			got, err := pg.Pick(tt.info)
			t.Logf("result: %+v, err: %v", got, err)
			if (err != nil) != tt.wantErr {
				t.Errorf("Pick() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Pick() got = %v, want %v", got, tt.want)
			}
		})
	}

}
