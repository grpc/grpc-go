/*
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
 */

package orca

import (
	"reflect"
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/internal/balancerload/orca/orca_v1"
	"google.golang.org/grpc/metadata"
)

var (
	testMessage = &orca_v1.LoadReport{
		CpuUtilization:           0.1,
		MemUtilization:           0.2,
		NicInUtilization:         0,
		NicOutUtilization:        0,
		RequestCostOrUtilization: map[string]float64{"ttt": 0.4},
	}
	testBytes, _ = proto.Marshal(testMessage)
)

func TestToMetadata(t *testing.T) {
	tests := []struct {
		name string
		r    *orca_v1.LoadReport
		want metadata.MD
	}{{
		name: "nil",
		r:    nil,
		want: nil,
	}, {
		name: "valid",
		r:    testMessage,
		want: metadata.MD{
			strings.ToLower(mdKey): []string{string(testBytes)},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToMetadata(tt.r); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromMetadata(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
		want *orca_v1.LoadReport
	}{{
		name: "nil",
		md:   nil,
		want: nil,
	}, {
		name: "valid",
		md: metadata.MD{
			strings.ToLower(mdKey): []string{string(testBytes)},
		},
		want: testMessage,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromMetadata(tt.md); !proto.Equal(got, tt.want) {
				t.Errorf("FromMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}
