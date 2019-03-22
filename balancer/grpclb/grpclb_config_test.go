/*
 *
 * Copyright 2019 gRPC authors.
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

package grpclb

import (
	"encoding/json"
	"reflect"
	"testing"
)

func Test_parseFullServiceConfig(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want *serviceConfig
	}{
		{
			name: "empty",
			s:    "",
			want: nil,
		},
		{
			name: "success1",
			s:    `{"loadBalancingConfig":[{"grpclb":{"childPolicy":[{"pick_first":{}}]}}]}`,
			want: &serviceConfig{
				LoadBalancingConfig: &[]map[string]*grpclbServiceConfig{
					{"grpclb": &grpclbServiceConfig{
						ChildPolicy: &[]map[string]json.RawMessage{
							{"pick_first": json.RawMessage("{}")},
						},
					}},
				},
			},
		},
		{
			name: "success2",
			s:    `{"loadBalancingConfig":[{"grpclb":{"childPolicy":[{"round_robin":{}},{"pick_first":{}}]}}]}`,
			want: &serviceConfig{
				LoadBalancingConfig: &[]map[string]*grpclbServiceConfig{
					{"grpclb": &grpclbServiceConfig{
						ChildPolicy: &[]map[string]json.RawMessage{
							{"round_robin": json.RawMessage("{}")},
							{"pick_first": json.RawMessage("{}")},
						},
					}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseFullServiceConfig(tt.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseFullServiceConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func Test_parseServiceConfig(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want *grpclbServiceConfig
	}{
		{
			name: "empty",
			s:    "",
			want: nil,
		},
		{
			name: "success1",
			s:    `{"loadBalancingConfig":[{"grpclb":{"childPolicy":[{"pick_first":{}}]}}]}`,
			want: &grpclbServiceConfig{
				ChildPolicy: &[]map[string]json.RawMessage{
					{"pick_first": json.RawMessage("{}")},
				},
			},
		},
		{
			name: "success2",
			s:    `{"loadBalancingConfig":[{"grpclb":{"childPolicy":[{"round_robin":{}},{"pick_first":{}}]}}]}`,
			want: &grpclbServiceConfig{
				ChildPolicy: &[]map[string]json.RawMessage{
					{"round_robin": json.RawMessage("{}")},
					{"pick_first": json.RawMessage("{}")},
				},
			},
		},
		{
			name: "no_grpclb",
			s:    `{"loadBalancingConfig":[{"notgrpclb":{"childPolicy":[{"round_robin":{}},{"pick_first":{}}]}}]}`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseServiceConfig(tt.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseFullServiceConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func Test_childIsPickFirst(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{
			name: "invalid",
			s:    "",
			want: false,
		},
		{
			name: "pickfirst_only",
			s:    `{"loadBalancingConfig":[{"grpclb":{"childPolicy":[{"pick_first":{}}]}}]}`,
			want: true,
		},
		{
			name: "pickfirst_before_rr",
			s:    `{"loadBalancingConfig":[{"grpclb":{"childPolicy":[{"pick_first":{}},{"round_robin":{}}]}}]}`,
			want: true,
		},
		{
			name: "rr_before_pickfirst",
			s:    `{"loadBalancingConfig":[{"grpclb":{"childPolicy":[{"round_robin":{}},{"pick_first":{}}]}}]}`,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := childIsPickFirst(tt.s); got != tt.want {
				t.Errorf("childIsPickFirst() = %v, want %v", got, tt.want)
			}
		})
	}
}
