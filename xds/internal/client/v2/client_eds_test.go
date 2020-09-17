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

package v2

import (
	"testing"
	"time"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/protobuf/ptypes"
	anypb "github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/version"
)

var (
	badlyMarshaledEDSResponse = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: version.V2EndpointsURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: version.V2EndpointsURL,
	}
	badResourceTypeInEDSResponse = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
		TypeUrl: version.V2EndpointsURL,
	}
	goodEDSResponse1 = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			func() *anypb.Any {
				clab0 := testutils.NewClusterLoadAssignmentBuilder(goodEDSName, nil)
				clab0.AddLocality("locality-1", 1, 1, []string{"addr1:314"}, nil)
				clab0.AddLocality("locality-2", 1, 0, []string{"addr2:159"}, nil)
				a, _ := ptypes.MarshalAny(clab0.Build())
				return a
			}(),
		},
		TypeUrl: version.V2EndpointsURL,
	}
	goodEDSResponse2 = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			func() *anypb.Any {
				clab0 := testutils.NewClusterLoadAssignmentBuilder("not-goodEDSName", nil)
				clab0.AddLocality("locality-1", 1, 1, []string{"addr1:314"}, nil)
				clab0.AddLocality("locality-2", 1, 0, []string{"addr2:159"}, nil)
				a, _ := ptypes.MarshalAny(clab0.Build())
				return a
			}(),
		},
		TypeUrl: version.V2EndpointsURL,
	}
)

func (s) TestEDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		edsResponse   *v2xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *xdsclient.EndpointsUpdate
		wantUpdateErr bool
	}{
		// Any in resource is badly marshaled.
		{
			name:          "badly-marshaled_response",
			edsResponse:   badlyMarshaledEDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response doesn't contain resource with the right type.
		{
			name:          "no-config-in-response",
			edsResponse:   badResourceTypeInEDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one uninteresting ClusterLoadAssignment.
		{
			name:          "one-uninterestring-assignment",
			edsResponse:   goodEDSResponse2,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one good ClusterLoadAssignment.
		{
			name:        "one-good-assignment",
			edsResponse: goodEDSResponse1,
			wantErr:     false,
			wantUpdate: &xdsclient.EndpointsUpdate{
				Localities: []xdsclient.Locality{
					{
						Endpoints: []xdsclient.Endpoint{{Address: "addr1:314"}},
						ID:        internal.LocalityID{SubZone: "locality-1"},
						Priority:  1,
						Weight:    1,
					},
					{
						Endpoints: []xdsclient.Endpoint{{Address: "addr2:159"}},
						ID:        internal.LocalityID{SubZone: "locality-2"},
						Priority:  0,
						Weight:    1,
					},
				},
			},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				rType:            xdsclient.EndpointsResource,
				resourceName:     goodEDSName,
				responseToHandle: test.edsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateErr:    test.wantUpdateErr,
			})
		})
	}
}

// TestEDSHandleResponseWithoutWatch tests the case where the v2Client
// receives an EDS response without a registered EDS watcher.
func (s) TestEDSHandleResponseWithoutWatch(t *testing.T) {
	_, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(xdsclient.ResourceType, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()

	if v2c.handleEDSResponse(badResourceTypeInEDSResponse) == nil {
		t.Fatal("v2c.handleEDSResponse() succeeded, should have failed")
	}

	if v2c.handleEDSResponse(goodEDSResponse1) != nil {
		t.Fatal("v2c.handleEDSResponse() succeeded, should have failed")
	}
}
