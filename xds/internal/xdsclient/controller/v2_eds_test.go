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

package controller

import (
	"testing"
	"time"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal"
	xtestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
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
		Resources: []*anypb.Any{marshaledConnMgr1},
		TypeUrl:   version.V2EndpointsURL,
	}
	marshaledGoodCLA1 = func() *anypb.Any {
		clab0 := xtestutils.NewClusterLoadAssignmentBuilder(goodEDSName, nil)
		clab0.AddLocality("locality-1", 1, 1, []string{"addr1:314"}, nil)
		clab0.AddLocality("locality-2", 1, 0, []string{"addr2:159"}, nil)
		return testutils.MarshalAny(clab0.Build())
	}()
	goodEDSResponse1 = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledGoodCLA1,
		},
		TypeUrl: version.V2EndpointsURL,
	}
	marshaledGoodCLA2 = func() *anypb.Any {
		clab0 := xtestutils.NewClusterLoadAssignmentBuilder("not-goodEDSName", nil)
		clab0.AddLocality("locality-1", 1, 0, []string{"addr1:314"}, nil)
		return testutils.MarshalAny(clab0.Build())
	}()
	goodEDSResponse2 = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledGoodCLA2,
		},
		TypeUrl: version.V2EndpointsURL,
	}
)

func (s) TestEDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		edsResponse   *v2xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    map[string]xdsresource.EndpointsUpdateErrTuple
		wantUpdateMD  xdsresource.UpdateMetadata
		wantUpdateErr bool
	}{
		// Any in resource is badly marshaled.
		{
			name:        "badly-marshaled_response",
			edsResponse: badlyMarshaledEDSResponse,
			wantErr:     true,
			wantUpdate:  nil,
			wantUpdateMD: xdsresource.UpdateMetadata{
				Status: xdsresource.ServiceStatusNACKed,
				ErrState: &xdsresource.UpdateErrorMetadata{
					Err: cmpopts.AnyError,
				},
			},
			wantUpdateErr: false,
		},
		// Response doesn't contain resource with the right type.
		{
			name:        "no-config-in-response",
			edsResponse: badResourceTypeInEDSResponse,
			wantErr:     true,
			wantUpdate:  nil,
			wantUpdateMD: xdsresource.UpdateMetadata{
				Status: xdsresource.ServiceStatusNACKed,
				ErrState: &xdsresource.UpdateErrorMetadata{
					Err: cmpopts.AnyError,
				},
			},
			wantUpdateErr: false,
		},
		// Response contains one uninteresting ClusterLoadAssignment.
		{
			name:        "one-uninterestring-assignment",
			edsResponse: goodEDSResponse2,
			wantErr:     false,
			wantUpdate: map[string]xdsresource.EndpointsUpdateErrTuple{
				"not-goodEDSName": {Update: xdsresource.EndpointsUpdate{
					Localities: []xdsresource.Locality{
						{
							Endpoints: []xdsresource.Endpoint{{Address: "addr1:314", Weight: 1}},
							ID:        internal.LocalityID{SubZone: "locality-1"},
							Priority:  0,
							Weight:    1,
						},
					},
					Raw: marshaledGoodCLA2,
				}},
			},
			wantUpdateMD: xdsresource.UpdateMetadata{
				Status: xdsresource.ServiceStatusACKed,
			},
			wantUpdateErr: false,
		},
		// Response contains one good ClusterLoadAssignment.
		{
			name:        "one-good-assignment",
			edsResponse: goodEDSResponse1,
			wantErr:     false,
			wantUpdate: map[string]xdsresource.EndpointsUpdateErrTuple{
				goodEDSName: {Update: xdsresource.EndpointsUpdate{
					Localities: []xdsresource.Locality{
						{
							Endpoints: []xdsresource.Endpoint{{Address: "addr1:314", Weight: 1}},
							ID:        internal.LocalityID{SubZone: "locality-1"},
							Priority:  1,
							Weight:    1,
						},
						{
							Endpoints: []xdsresource.Endpoint{{Address: "addr2:159", Weight: 1}},
							ID:        internal.LocalityID{SubZone: "locality-2"},
							Priority:  0,
							Weight:    1,
						},
					},
					Raw: marshaledGoodCLA1,
				}},
			},
			wantUpdateMD: xdsresource.UpdateMetadata{
				Status: xdsresource.ServiceStatusACKed,
			},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				rType:            xdsresource.EndpointsResource,
				resourceName:     goodEDSName,
				responseToHandle: test.edsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateMD:     test.wantUpdateMD,
				wantUpdateErr:    test.wantUpdateErr,
			})
		})
	}
}

// TestEDSHandleResponseWithoutWatch tests the case where the v2Client
// receives an EDS response without a registered EDS watcher.
func (s) TestEDSHandleResponseWithoutWatch(t *testing.T) {
	fakeServer, cleanup := startServer(t)
	defer cleanup()

	v2c, err := newTestController(&testUpdateReceiver{
		f: func(xdsresource.ResourceType, map[string]interface{}, xdsresource.UpdateMetadata) {},
	}, fakeServer.Address, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()

	if _, _, _, err := v2c.handleResponse(badResourceTypeInEDSResponse); err == nil {
		t.Fatal("v2c.handleEDSResponse() succeeded, should have failed")
	}

	if _, _, _, err := v2c.handleResponse(goodEDSResponse1); err != nil {
		t.Fatal("v2c.handleEDSResponse() succeeded, should have failed")
	}
}
