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

package rls

import (
	"testing"

	"github.com/golang/protobuf/proto"
	_ "google.golang.org/grpc/balancer/rls"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/internal/testutils"
	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer"
	"google.golang.org/grpc/xds/internal/clusterspecifier"
	"google.golang.org/protobuf/types/known/durationpb"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestParseClusterSpecifierConfig tests the parsing functionality of the RLS
// Cluster Specifier Plugin.
func (s) TestParseClusterSpecifierConfig(t *testing.T) {
	tests := []struct {
		name    string
		rlcs    proto.Message
		wantErr bool
	}{
		{
			name:    "invalid-rls-cluster-specifier",
			rlcs:    rlsClusterSpecifierConfigError,
			wantErr: true,
		},
		{
			name: "valid-rls-cluster-specifier",
			rlcs: rlsClusterSpecifierConfigWithoutTransformations,
		},
	}
	for _, test := range tests {
		cs := clusterspecifier.Get("type.googleapis.com/grpc.lookup.v1.RouteLookupClusterSpecifier")
		if cs == nil {
			t.Fatal("Error getting cluster specifier")
		}
		_, err := cs.ParseClusterSpecifierConfig(test.rlcs)

		if (err != nil) != test.wantErr {
			t.Fatalf("ParseClusterSpecifierConfig(%+v) returned err: %v, wantErr: %v", test.rlcs, err, test.wantErr)
		}
	}
}

// This will error because the required match field is set in grpc key builder.
var rlsClusterSpecifierConfigError = testutils.MarshalAny(&grpc_lookup_v1.RouteLookupClusterSpecifier{
	RouteLookupConfig: &grpc_lookup_v1.RouteLookupConfig{
		GrpcKeybuilders: []*grpc_lookup_v1.GrpcKeyBuilder{
			{
				Names: []*grpc_lookup_v1.GrpcKeyBuilder_Name{
					{
						Service: "service",
						Method:  "method",
					},
				},
				Headers: []*grpc_lookup_v1.NameMatcher{
					{
						Key:           "k1",
						RequiredMatch: true,
						Names:         []string{"v1"},
					},
				},
			},
		},
	},
})

// Corresponds to the rls unit test case in
// balancer/rls/internal/config_test.go.
var rlsClusterSpecifierConfigWithoutTransformations = testutils.MarshalAny(&grpc_lookup_v1.RouteLookupClusterSpecifier{
	RouteLookupConfig: &grpc_lookup_v1.RouteLookupConfig{
		GrpcKeybuilders: []*grpc_lookup_v1.GrpcKeyBuilder{
			{
				Names: []*grpc_lookup_v1.GrpcKeyBuilder_Name{
					{
						Service: "service",
						Method:  "method",
					},
				},
				Headers: []*grpc_lookup_v1.NameMatcher{
					{
						Key:   "k1",
						Names: []string{"v1"},
					},
				},
			},
		},
		LookupService:        "target",
		LookupServiceTimeout: &durationpb.Duration{Seconds: 100},
		MaxAge:               &durationpb.Duration{Seconds: 60},
		StaleAge:             &durationpb.Duration{Seconds: 50},
		CacheSizeBytes:       1000,
		DefaultTarget:        "passthrough:///default",
	},
})
