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

package resolver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

func TestNewActionsFromRoutes(t *testing.T) {
	tests := []struct {
		name   string
		routes []*xdsclient.Route
		want   map[string]action
	}{
		{
			name: "temp",
			routes: []*xdsclient.Route{
				{Action: map[string]uint32{"B": 60, "A": 40}},
				{Action: map[string]uint32{"A": 30, "B": 70}},
				{Action: map[string]uint32{"B": 90, "C": 10}},
			},
			want: map[string]action{
				"A40_B60_": {map[string]uint32{"A": 40, "B": 60}, "A_B_", ""},
				"A30_B70_": {map[string]uint32{"A": 30, "B": 70}, "A_B_", ""},
				"B90_C10_": {map[string]uint32{"B": 90, "C": 10}, "B_C_", ""},
			},
		},
	}

	cmpOpts := []cmp.Option{cmp.AllowUnexported(action{})}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newActionsFromRoutes(tt.routes)
			if !cmp.Equal(got, tt.want, cmpOpts...) {
				t.Errorf("newActionsFromRoutes() got unexpected result, diff %v", cmp.Diff(got, tt.want, cmpOpts...))
			}
		})
	}
}

func TestRemoveOrReuseName(t *testing.T) {
	tests := []struct {
		name          string
		oldActions    map[string]action
		oldNextIndex  map[string]int
		newActions    map[string]action
		wantActions   map[string]action
		wantNextIndex map[string]int
	}{
		{
			name: "add and reuse",
			oldActions: map[string]action{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
				},
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_1",
				},
				"a50_b50_": {
					clustersWithWeights: map[string]uint32{"a": 50, "b": 50},
					clusterNames:        "a_b_",
					assignedName:        "a_b_0",
				},
			},
			oldNextIndex: map[string]int{
				"a_b_c_": 2,
				"a_b_":   1,
			},
			newActions: map[string]action{
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
				},
				"a30_b30_c40_": {
					clustersWithWeights: map[string]uint32{"a": 30, "b": 30, "c": 40},
					clusterNames:        "a_b_c_",
				},
				"c50_d50_": {
					clustersWithWeights: map[string]uint32{"c": 50, "d": 50},
					clusterNames:        "c_d_",
				},
			},
			wantActions: map[string]action{
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_1",
				},
				"a30_b30_c40_": {
					clustersWithWeights: map[string]uint32{"a": 30, "b": 30, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
				},
				"c50_d50_": {
					clustersWithWeights: map[string]uint32{"c": 50, "d": 50},
					clusterNames:        "c_d_",
					assignedName:        "c_d_0",
				},
			},
			wantNextIndex: map[string]int{
				"a_b_c_": 2,
				"c_d_":   1,
			},
		},
	}
	cmpOpts := []cmp.Option{cmp.AllowUnexported(action{})}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &xdsResolver{
				actions:   tt.oldActions,
				nextIndex: tt.oldNextIndex,
			}
			r.updateActions(tt.newActions)
			if !cmp.Equal(r.actions, tt.wantActions, cmpOpts...) {
				t.Errorf("removeOrReuseName() got unexpected actions, diff %v", cmp.Diff(r.actions, tt.wantActions, cmpOpts...))
			}
			if !cmp.Equal(r.nextIndex, tt.wantNextIndex) {
				t.Errorf("removeOrReuseName() got unexpected nextIndex, diff %v", cmp.Diff(r.nextIndex, tt.wantNextIndex))
			}
		})
	}
}
