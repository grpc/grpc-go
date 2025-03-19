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

package internal

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testServerIdentifierExtension struct{ x int }

func (ts *testServerIdentifierExtension) Equal(other any) bool {
	ots, ok := other.(*testServerIdentifierExtension)
	if !ok {
		return false
	}
	return ts.x == ots.x
}

func (ts *testServerIdentifierExtension) String() string {
	return fmt.Sprintf("testServerIdentifierExtension-%d", ts.x)
}

type testServerIdentifierExtensionWithoutStringAndEqual struct{}

func newStructProtoFromMap(t *testing.T, input map[string]any) *structpb.Struct {
	t.Helper()

	ret, err := structpb.NewStruct(input)
	if err != nil {
		t.Fatalf("Failed to create new struct proto from map %v: %v", input, err)
	}
	return ret
}

func (s) TestServerIdentifierString(t *testing.T) {
	tests := []struct {
		name string
		si   clients.ServerIdentifier
		want string
	}{
		{
			name: "empty",
			si:   clients.ServerIdentifier{},
			want: "",
		},
		{
			name: "server_uri_only",
			si:   clients.ServerIdentifier{ServerURI: "foo"},
			want: "foo",
		},
		{
			name: "stringer_extension",
			si: clients.ServerIdentifier{
				ServerURI:  "foo",
				Extensions: &testServerIdentifierExtension{x: 1},
			},
			want: "foo-testServerIdentifierExtension-1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ServerIdentifierString(test.si); got != test.want {
				t.Errorf("ServerIdentifierString() = %v, want %v", got, test.want)
			}
		})
	}
}

func (s) TestServerIdentifierString_NonStringerExtension(t *testing.T) {
	si := clients.ServerIdentifier{
		ServerURI:  "foo",
		Extensions: &testServerIdentifierExtensionWithoutStringAndEqual{},
	}
	got := ServerIdentifierString(si)
	// Check if the output matches the expected pattern for non-stringer
	// extensions i.e. si.ServerURI-0xxxx
	matched, err := regexp.MatchString(`^foo-0x[0-9a-f]+$`, got)
	if err != nil {
		t.Fatalf("Error in regex matching: %v", err)
	}
	if !matched {
		t.Errorf("ServerIdentifierString() = %v, want a string matching the pattern `^foo-0x[0-9a-f]+$`", got)
	}
}

func TestServerIdentifier_Equal(t *testing.T) {
	tests := []struct {
		name   string
		s1     clients.ServerIdentifier
		s2     clients.ServerIdentifier
		wantEq bool
	}{
		{
			name:   "both_empty",
			s1:     clients.ServerIdentifier{},
			s2:     clients.ServerIdentifier{},
			wantEq: true,
		},
		{
			name:   "one_empty",
			s1:     clients.ServerIdentifier{},
			s2:     clients.ServerIdentifier{ServerURI: "bar"},
			wantEq: false,
		},
		{
			name:   "other_empty",
			s1:     clients.ServerIdentifier{ServerURI: "foo"},
			s2:     clients.ServerIdentifier{},
			wantEq: false,
		},
		{
			name:   "different_ServerURI",
			s1:     clients.ServerIdentifier{ServerURI: "foo"},
			s2:     clients.ServerIdentifier{ServerURI: "bar"},
			wantEq: false,
		},
		{
			name: "different_Extensions_with_no_Equal_method",
			s1: clients.ServerIdentifier{
				Extensions: 1,
			},
			s2: clients.ServerIdentifier{
				Extensions: 2,
			},
			wantEq: true,
		},
		{
			name: "same_Extensions_with_no_Equal_method",
			s1: clients.ServerIdentifier{
				Extensions: 1,
			},
			s2: clients.ServerIdentifier{
				Extensions: 1,
			},
			wantEq: true,
		},
		{
			name: "different_Extensions_with_Equal_method",
			s1: clients.ServerIdentifier{
				Extensions: &testServerIdentifierExtension{1},
			},
			s2: clients.ServerIdentifier{
				Extensions: &testServerIdentifierExtension{2},
			},
			wantEq: false,
		},
		{
			name: "same_Extensions_same_with_Equal_method",
			s1: clients.ServerIdentifier{
				Extensions: &testServerIdentifierExtension{1},
			},
			s2: clients.ServerIdentifier{
				Extensions: &testServerIdentifierExtension{1},
			},
			wantEq: true,
		},
		{
			name: "first_config_Extensions_is_nil",
			s1: clients.ServerIdentifier{
				Extensions: &testServerIdentifierExtension{1},
			},
			s2: clients.ServerIdentifier{
				Extensions: nil,
			},
			wantEq: false,
		},
		{
			name: "other_config_Extensions_is_nil",
			s1: clients.ServerIdentifier{
				Extensions: nil,
			},
			s2: clients.ServerIdentifier{
				Extensions: &testServerIdentifierExtension{2},
			},
			wantEq: false,
		},
		{
			name: "all_fields_same",
			s1: clients.ServerIdentifier{
				ServerURI:  "foo",
				Extensions: &testServerIdentifierExtension{1},
			},
			s2: clients.ServerIdentifier{
				ServerURI:  "foo",
				Extensions: &testServerIdentifierExtension{1},
			},
			wantEq: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if gotEq := ServerIdentifierEqual(test.s1, test.s2); gotEq != test.wantEq {
				t.Errorf("Equal() = %v, want %v", gotEq, test.wantEq)
			}
		})
	}
}

func (s) TestIsLocalityEmpty(t *testing.T) {
	tests := []struct {
		name     string
		locality clients.Locality
		want     bool
	}{
		{
			name:     "empty_locality",
			locality: clients.Locality{},
			want:     true,
		},
		{
			name:     "non_empty_region",
			locality: clients.Locality{Region: "region"},
			want:     false,
		},
		{
			name:     "non_empty_zone",
			locality: clients.Locality{Zone: "zone"},
			want:     false,
		},
		{
			name:     "non_empty_subzone",
			locality: clients.Locality{SubZone: "subzone"},
			want:     false,
		},
		{
			name:     "non_empty_all_fields",
			locality: clients.Locality{Region: "region", Zone: "zone", SubZone: "subzone"},
			want:     false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isLocalityEmpty(test.locality); got != test.want {
				t.Errorf("IsEmpty() = %v, want %v", got, test.want)
			}
		})
	}
}

func (s) TestIsLocalityEqual(t *testing.T) {
	tests := []struct {
		name   string
		l1     clients.Locality
		l2     clients.Locality
		wantEq bool
	}{
		{
			name:   "both_equal",
			l1:     clients.Locality{Region: "region", Zone: "zone", SubZone: "subzone"},
			l2:     clients.Locality{Region: "region", Zone: "zone", SubZone: "subzone"},
			wantEq: true,
		},
		{
			name:   "different_regions",
			l1:     clients.Locality{Region: "region1", Zone: "zone", SubZone: "subzone"},
			l2:     clients.Locality{Region: "region2", Zone: "zone", SubZone: "subzone"},
			wantEq: false,
		},

		{
			name:   "different_zones",
			l1:     clients.Locality{Region: "region", Zone: "zone1", SubZone: "subzone"},
			l2:     clients.Locality{Region: "region", Zone: "zone2", SubZone: "subzone"},
			wantEq: false,
		},
		{
			name:   "different_subzones",
			l1:     clients.Locality{Region: "region", Zone: "zone", SubZone: "subzone1"},
			l2:     clients.Locality{Region: "region", Zone: "zone", SubZone: "subzone2"},
			wantEq: false,
		},
		{
			name:   "one_empty",
			l1:     clients.Locality{},
			l2:     clients.Locality{Region: "region", Zone: "zone", SubZone: "subzone"},
			wantEq: false,
		},
		{
			name:   "both_empty",
			l1:     clients.Locality{},
			l2:     clients.Locality{},
			wantEq: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if gotEq := isLocalityEqual(test.l1, test.l2); gotEq != test.wantEq {
				t.Errorf("Equal() = %v, want %v", gotEq, test.wantEq)
			}
		})
	}
}

func (s) TestNodeProto(t *testing.T) {
	tests := []struct {
		desc      string
		inputNode clients.Node
		wantProto *v3corepb.Node
	}{
		{
			desc: "all_fields_set",
			inputNode: clients.Node{
				ID:      "id",
				Cluster: "cluster",
				Locality: clients.Locality{
					Region:  "region",
					Zone:    "zone",
					SubZone: "sub_zone",
				},
				Metadata:         newStructProtoFromMap(t, map[string]any{"k1": "v1", "k2": 101, "k3": 280.0}),
				UserAgentName:    "user agent",
				UserAgentVersion: "version",
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
			},
		},
		{
			desc: "some_fields_unset",
			inputNode: clients.Node{
				ID: "id",
			},
			wantProto: &v3corepb.Node{
				Id:                   "id",
				UserAgentName:        "",
				UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: ""},
			},
		},
		{
			desc: "empty_locality",
			inputNode: clients.Node{
				ID:       "id",
				Locality: clients.Locality{},
			},
			wantProto: &v3corepb.Node{
				Id:                   "id",
				UserAgentName:        "",
				UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: ""},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			gotProto := NodeProto(test.inputNode)
			if diff := cmp.Diff(test.wantProto, gotProto, protocmp.Transform()); diff != "" {
				t.Fatalf("Unexpected diff in node proto: (-want, +got):\n%s", diff)
			}
		})
	}
}
