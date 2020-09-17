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
		want   map[string]actionWithAssignedName
	}{
		{
			name: "temp",
			routes: []*xdsclient.Route{
				{Action: map[string]uint32{"B": 60, "A": 40}},
				{Action: map[string]uint32{"A": 30, "B": 70}},
				{Action: map[string]uint32{"B": 90, "C": 10}},
			},
			want: map[string]actionWithAssignedName{
				"A40_B60_": {map[string]uint32{"A": 40, "B": 60}, "A_B_", "", 0},
				"A30_B70_": {map[string]uint32{"A": 30, "B": 70}, "A_B_", "", 0},
				"B90_C10_": {map[string]uint32{"B": 90, "C": 10}, "B_C_", "", 0},
			},
		},
	}

	cmpOpts := []cmp.Option{cmp.AllowUnexported(actionWithAssignedName{})}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newActionsFromRoutes(tt.routes); !cmp.Equal(got, tt.want, cmpOpts...) {
				t.Errorf("newActionsFromRoutes() got unexpected result, diff %v", cmp.Diff(got, tt.want, cmpOpts...))
			}
		})
	}
}

func TestRemoveOrReuseName(t *testing.T) {
	tests := []struct {
		name         string
		oldActions   map[string]actionWithAssignedName
		oldRandNums  map[int64]bool
		newActions   map[string]actionWithAssignedName
		wantActions  map[string]actionWithAssignedName
		wantRandNums map[int64]bool
	}{
		{
			name: "add same cluster",
			oldActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
			},
			oldRandNums: map[int64]bool{
				0: true,
			},
			newActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
				},
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
				},
			},
			wantActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_1000",
					randomNumber:        1000,
				},
			},
			wantRandNums: map[int64]bool{
				0:    true,
				1000: true,
			},
		},
		{
			name: "delete same cluster",
			oldActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_1",
					randomNumber:        1,
				},
			},
			oldRandNums: map[int64]bool{
				0: true,
				1: true,
			},
			newActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
				},
			},
			wantActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
			},
			wantRandNums: map[int64]bool{
				0: true,
			},
		},
		{
			name: "add new clusters",
			oldActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
			},
			oldRandNums: map[int64]bool{
				0: true,
			},
			newActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
				},
				"a50_b50_": {
					clustersWithWeights: map[string]uint32{"a": 50, "b": 50},
					clusterNames:        "a_b_",
				},
			},
			wantActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
				"a50_b50_": {
					clustersWithWeights: map[string]uint32{"a": 50, "b": 50},
					clusterNames:        "a_b_",
					assignedName:        "a_b_1000",
					randomNumber:        1000,
				},
			},
			wantRandNums: map[int64]bool{
				0:    true,
				1000: true,
			},
		},
		{
			name: "reuse",
			oldActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
			},
			oldRandNums: map[int64]bool{
				0: true,
			},
			newActions: map[string]actionWithAssignedName{
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
				},
			},
			wantActions: map[string]actionWithAssignedName{
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
			},
			wantRandNums: map[int64]bool{
				0: true,
			},
		},
		{
			name: "add and reuse",
			oldActions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_1",
					randomNumber:        1,
				},
				"a50_b50_": {
					clustersWithWeights: map[string]uint32{"a": 50, "b": 50},
					clusterNames:        "a_b_",
					assignedName:        "a_b_2",
					randomNumber:        2,
				},
			},
			oldRandNums: map[int64]bool{
				0: true,
				1: true,
				2: true,
			},
			newActions: map[string]actionWithAssignedName{
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
			wantActions: map[string]actionWithAssignedName{
				"a10_b50_c40_": {
					clustersWithWeights: map[string]uint32{"a": 10, "b": 50, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_1",
					randomNumber:        1,
				},
				"a30_b30_c40_": {
					clustersWithWeights: map[string]uint32{"a": 30, "b": 30, "c": 40},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
					randomNumber:        0,
				},
				"c50_d50_": {
					clustersWithWeights: map[string]uint32{"c": 50, "d": 50},
					clusterNames:        "c_d_",
					assignedName:        "c_d_1000",
					randomNumber:        1000,
				},
			},
			wantRandNums: map[int64]bool{
				0:    true,
				1:    true,
				1000: true,
			},
		},
	}
	cmpOpts := []cmp.Option{cmp.AllowUnexported(actionWithAssignedName{})}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer replaceRandNumGenerator(1000)()
			r := &xdsResolver{
				actions:                    tt.oldActions,
				usedActionNameRandomNumber: tt.oldRandNums,
			}
			r.updateActions(tt.newActions)
			if !cmp.Equal(r.actions, tt.wantActions, cmpOpts...) {
				t.Errorf("removeOrReuseName() got unexpected actions, diff %v", cmp.Diff(r.actions, tt.wantActions, cmpOpts...))
			}
			if !cmp.Equal(r.usedActionNameRandomNumber, tt.wantRandNums) {
				t.Errorf("removeOrReuseName() got unexpected nextIndex, diff %v", cmp.Diff(r.usedActionNameRandomNumber, tt.wantRandNums))
			}
		})
	}
}

func TestGetActionAssignedName(t *testing.T) {
	tests := []struct {
		name    string
		actions map[string]actionWithAssignedName
		action  map[string]uint32
		want    string
	}{
		{
			name: "good",
			actions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
				},
			},
			action: map[string]uint32{"a": 20, "b": 30, "c": 50},
			want:   "a_b_c_0",
		},
		{
			name: "two",
			actions: map[string]actionWithAssignedName{
				"a20_b30_c50_": {
					clustersWithWeights: map[string]uint32{"a": 20, "b": 30, "c": 50},
					clusterNames:        "a_b_c_",
					assignedName:        "a_b_c_0",
				},
				"c50_d50_": {
					clustersWithWeights: map[string]uint32{"c": 50, "d": 50},
					clusterNames:        "c_d_",
					assignedName:        "c_d_0",
				},
			},
			action: map[string]uint32{"c": 50, "d": 50},
			want:   "c_d_0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &xdsResolver{
				actions: tt.actions,
			}
			if got := r.getActionAssignedName(tt.action); got != tt.want {
				t.Errorf("getActionAssignedName() = %v, want %v", got, tt.want)
			}
		})
	}
}
