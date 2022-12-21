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

package csds

import (
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/protobuf/testing/protocmp"

	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) Test_nodeProtoToV3(t *testing.T) {
	const (
		testID      = "test-id"
		testCluster = "test-cluster"
		testZone    = "test-zone"
	)
	tests := []struct {
		name string
		n    proto.Message
		want *v3corepb.Node
	}{
		{
			name: "v3",
			n: &v3corepb.Node{
				Id:       testID,
				Cluster:  testCluster,
				Locality: &v3corepb.Locality{Zone: testZone},
			},
			want: &v3corepb.Node{
				Id:       testID,
				Cluster:  testCluster,
				Locality: &v3corepb.Locality{Zone: testZone},
			},
		},
		{
			name: "v2",
			n: &v2corepb.Node{
				Id:       testID,
				Cluster:  testCluster,
				Locality: &v2corepb.Locality{Zone: testZone},
			},
			want: &v3corepb.Node{
				Id:       testID,
				Cluster:  testCluster,
				Locality: &v3corepb.Locality{Zone: testZone},
			},
		},
		{
			name: "not node",
			n:    &v2corepb.Locality{Zone: testZone},
			want: nil, // Input is not a node, should return nil.
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nodeProtoToV3(tt.n, nil)
			if diff := cmp.Diff(got, tt.want, protocmp.Transform()); diff != "" {
				t.Errorf("nodeProtoToV3() got unexpected result, diff (-got, +want): %v", diff)
			}
		})
	}
}
