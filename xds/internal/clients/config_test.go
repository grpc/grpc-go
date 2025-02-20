/*
 *
 * Copyright 2024 gRPC authors.
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

package clients

import (
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testServerConfigExtension struct{ x int }

func (ts testServerConfigExtension) Equal(other any) bool {
	ots, ok := other.(testServerConfigExtension)
	if !ok {
		return false
	}
	return ts.x == ots.x
}

func newStructProtoFromMap(t *testing.T, input map[string]any) *structpb.Struct {
	t.Helper()

	ret, err := structpb.NewStruct(input)
	if err != nil {
		t.Fatalf("Failed to create new struct proto from map %v: %v", input, err)
	}
	return ret
}

func (s) TestServerConfig_Equal(t *testing.T) {
	tests := []struct {
		name   string
		s1     *ServerConfig
		s2     *ServerConfig
		wantEq bool
	}{
		{
			name:   "both_nil",
			s1:     nil,
			s2:     nil,
			wantEq: true,
		},
		{
			name:   "one_nil",
			s1:     nil,
			s2:     &ServerConfig{},
			wantEq: false,
		},
		{
			name:   "other_nil",
			s1:     &ServerConfig{},
			s2:     nil,
			wantEq: false,
		},
		{
			name:   "both_empty_and_equal",
			s1:     &ServerConfig{},
			s2:     &ServerConfig{},
			wantEq: true,
		},
		{
			name:   "different_ServerURI",
			s1:     &ServerConfig{ServerURI: "foo"},
			s2:     &ServerConfig{ServerURI: "bar"},
			wantEq: false,
		},
		{
			name:   "different_IgnoreResourceDeletion",
			s1:     &ServerConfig{IgnoreResourceDeletion: true},
			s2:     &ServerConfig{},
			wantEq: false,
		},
		{
			name: "different_Extensions_with_no_Equal_method",
			s1: &ServerConfig{
				Extensions: 1,
			},
			s2: &ServerConfig{
				Extensions: 2,
			},
			wantEq: false, // By default, if there's no Equal method, they are unequal
		},
		{
			name: "same_Extensions_with_no_Equal_method",
			s1: &ServerConfig{
				Extensions: 1,
			},
			s2: &ServerConfig{
				Extensions: 1,
			},
			wantEq: false, // By default, if there's no Equal method, they are unequal
		},
		{
			name: "different_Extensions_with_Equal_method",
			s1: &ServerConfig{
				Extensions: testServerConfigExtension{1},
			},
			s2: &ServerConfig{
				Extensions: testServerConfigExtension{2},
			},
			wantEq: false,
		},
		{
			name: "same_Extensions_same_with_Equal_method",
			s1: &ServerConfig{
				Extensions: testServerConfigExtension{1},
			},
			s2: &ServerConfig{
				Extensions: testServerConfigExtension{1},
			},
			wantEq: true,
		},
		{
			name: "first_config_Extensions_is_nil",
			s1: &ServerConfig{
				Extensions: testServerConfigExtension{1},
			},
			s2: &ServerConfig{
				Extensions: nil,
			},
			wantEq: false,
		},
		{
			name: "other_config_Extensions_is_nil",
			s1: &ServerConfig{
				Extensions: nil,
			},
			s2: &ServerConfig{
				Extensions: testServerConfigExtension{2},
			},
			wantEq: false,
		},
		{
			name: "all_fields_same",
			s1: &ServerConfig{
				ServerURI:              "foo",
				IgnoreResourceDeletion: true,
				Extensions:             testServerConfigExtension{1},
			},
			s2: &ServerConfig{
				ServerURI:              "foo",
				IgnoreResourceDeletion: true,
				Extensions:             testServerConfigExtension{1},
			},
			wantEq: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if gotEq := test.s1.equal(test.s2); gotEq != test.wantEq {
				t.Errorf("Equal() = %v, want %v", gotEq, test.wantEq)
			}
		})
	}
}

func (s) TestLocality_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		locality Locality
		want     bool
	}{
		{
			name:     "empty_locality",
			locality: Locality{},
			want:     true,
		},
		{
			name:     "non_empty_region",
			locality: Locality{Region: "region"},
			want:     false,
		},
		{
			name:     "non_empty_zone",
			locality: Locality{Zone: "zone"},
			want:     false,
		},
		{
			name:     "non_empty_subzone",
			locality: Locality{SubZone: "subzone"},
			want:     false,
		},
		{
			name:     "non_empty_all_fields",
			locality: Locality{Region: "region", Zone: "zone", SubZone: "subzone"},
			want:     false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.locality.isEmpty(); got != test.want {
				t.Errorf("IsEmpty() = %v, want %v", got, test.want)
			}
		})
	}
}

func (s) TestLocality_Equal(t *testing.T) {
	tests := []struct {
		name   string
		l1     Locality
		l2     Locality
		wantEq bool
	}{
		{
			name:   "both_equal",
			l1:     Locality{Region: "region", Zone: "zone", SubZone: "subzone"},
			l2:     Locality{Region: "region", Zone: "zone", SubZone: "subzone"},
			wantEq: true,
		},
		{
			name:   "different_regions",
			l1:     Locality{Region: "region1", Zone: "zone", SubZone: "subzone"},
			l2:     Locality{Region: "region2", Zone: "zone", SubZone: "subzone"},
			wantEq: false,
		},

		{
			name:   "different_zones",
			l1:     Locality{Region: "region", Zone: "zone1", SubZone: "subzone"},
			l2:     Locality{Region: "region", Zone: "zone2", SubZone: "subzone"},
			wantEq: false,
		},
		{
			name:   "different_subzones",
			l1:     Locality{Region: "region", Zone: "zone", SubZone: "subzone1"},
			l2:     Locality{Region: "region", Zone: "zone", SubZone: "subzone2"},
			wantEq: false,
		},
		{
			name:   "one_empty",
			l1:     Locality{},
			l2:     Locality{Region: "region", Zone: "zone", SubZone: "subzone"},
			wantEq: false,
		},
		{
			name:   "both_empty",
			l1:     Locality{},
			l2:     Locality{},
			wantEq: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if gotEq := test.l1.equal(test.l2); gotEq != test.wantEq {
				t.Errorf("Equal() = %v, want %v", gotEq, test.wantEq)
			}
		})
	}
}

func (s) TestNode_ToProto(t *testing.T) {
	tests := []struct {
		desc      string
		inputNode Node
		wantProto *v3corepb.Node
	}{
		{
			desc: "all_fields_set",
			inputNode: Node{
				ID:      "id",
				Cluster: "cluster",
				Locality: Locality{
					Region:  "region",
					Zone:    "zone",
					SubZone: "sub_zone",
				},
				Metadata:         newStructProtoFromMap(t, map[string]any{"k1": "v1", "k2": 101, "k3": 280.0}),
				UserAgentName:    "user agent",
				UserAgentVersion: "version",
				clientFeatures:   []string{"feature1", "feature2"},
			},
			wantProto: &v3corepb.Node{
				Id:      "id",
				Cluster: "cluster",
				Locality: &v3corepb.Locality{
					Region:  "region",
					Zone:    "zone",
					SubZone: "sub_zone",
				},
				Metadata:             newStructProtoFromMap(t, map[string]any{"k1": "v1", "k2": 101, "k3": 280.0}),
				UserAgentName:        "user agent",
				UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "version"},
				ClientFeatures:       []string{"feature1", "feature2"},
			},
		},
		{
			desc: "some_fields_unset",
			inputNode: Node{
				ID: "id",
			},
			wantProto: &v3corepb.Node{
				Id:                   "id",
				UserAgentName:        "",
				UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: ""},
				ClientFeatures:       nil,
			},
		},
		{
			desc: "empty_locality",
			inputNode: Node{
				ID:       "id",
				Locality: Locality{},
			},
			wantProto: &v3corepb.Node{
				Id:                   "id",
				UserAgentName:        "",
				UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: ""},
				ClientFeatures:       nil,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			gotProto := test.inputNode.toProto()
			if diff := cmp.Diff(test.wantProto, gotProto, protocmp.Transform()); diff != "" {
				t.Fatalf("Unexpected diff in node proto: (-want, +got):\n%s", diff)
			}
		})
	}
}
