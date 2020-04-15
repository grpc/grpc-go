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
 */

package client

import (
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/golang/protobuf/ptypes"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal"
)

func (s) TestEDSParseRespProto(t *testing.T) {
	tests := []struct {
		name    string
		m       *xdspb.ClusterLoadAssignment
		want    EndpointsUpdate
		wantErr bool
	}{
		{
			name: "missing-priority",
			m: func() *xdspb.ClusterLoadAssignment {
				clab0 := NewClusterLoadAssignmentBuilder("test", nil)
				clab0.AddLocality("locality-1", 1, 0, []string{"addr1:314"}, nil)
				clab0.AddLocality("locality-2", 1, 2, []string{"addr2:159"}, nil)
				return clab0.Build()
			}(),
			want:    EndpointsUpdate{},
			wantErr: true,
		},
		{
			name: "missing-locality-ID",
			m: func() *xdspb.ClusterLoadAssignment {
				clab0 := NewClusterLoadAssignmentBuilder("test", nil)
				clab0.AddLocality("", 1, 0, []string{"addr1:314"}, nil)
				return clab0.Build()
			}(),
			want:    EndpointsUpdate{},
			wantErr: true,
		},
		{
			name: "good",
			m: func() *xdspb.ClusterLoadAssignment {
				clab0 := NewClusterLoadAssignmentBuilder("test", nil)
				clab0.AddLocality("locality-1", 1, 1, []string{"addr1:314"}, &AddLocalityOptions{
					Health: []corepb.HealthStatus{corepb.HealthStatus_UNHEALTHY},
					Weight: []uint32{271},
				})
				clab0.AddLocality("locality-2", 1, 0, []string{"addr2:159"}, &AddLocalityOptions{
					Health: []corepb.HealthStatus{corepb.HealthStatus_DRAINING},
					Weight: []uint32{828},
				})
				return clab0.Build()
			}(),
			want: EndpointsUpdate{
				Drops: nil,
				Localities: []Locality{
					{
						Endpoints: []Endpoint{{
							Address:      "addr1:314",
							HealthStatus: EndpointHealthStatusUnhealthy,
							Weight:       271,
						}},
						ID:       internal.LocalityID{SubZone: "locality-1"},
						Priority: 1,
						Weight:   1,
					},
					{
						Endpoints: []Endpoint{{
							Address:      "addr2:159",
							HealthStatus: EndpointHealthStatusDraining,
							Weight:       828,
						}},
						ID:       internal.LocalityID{SubZone: "locality-2"},
						Priority: 0,
						Weight:   1,
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEDSRespProto(tt.m)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseEDSRespProto() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if d := cmp.Diff(got, tt.want); d != "" {
				t.Errorf("ParseEDSRespProto() got = %v, want %v, diff: %v", got, tt.want, d)
			}
		})
	}
}

var (
	badlyMarshaledEDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: edsURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: edsURL,
	}
	badResourceTypeInEDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
		TypeUrl: edsURL,
	}
	goodEDSResponse1 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			func() *anypb.Any {
				clab0 := NewClusterLoadAssignmentBuilder(goodEDSName, nil)
				clab0.AddLocality("locality-1", 1, 1, []string{"addr1:314"}, nil)
				clab0.AddLocality("locality-2", 1, 0, []string{"addr2:159"}, nil)
				a, _ := ptypes.MarshalAny(clab0.Build())
				return a
			}(),
		},
		TypeUrl: edsURL,
	}
	goodEDSResponse2 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			func() *anypb.Any {
				clab0 := NewClusterLoadAssignmentBuilder("not-goodEDSName", nil)
				clab0.AddLocality("locality-1", 1, 1, []string{"addr1:314"}, nil)
				clab0.AddLocality("locality-2", 1, 0, []string{"addr2:159"}, nil)
				a, _ := ptypes.MarshalAny(clab0.Build())
				return a
			}(),
		},
		TypeUrl: edsURL,
	}
)

func (s) TestEDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		edsResponse   *xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *EndpointsUpdate
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
			wantUpdate: &EndpointsUpdate{
				Localities: []Locality{
					{
						Endpoints: []Endpoint{{Address: "addr1:314"}},
						ID:        internal.LocalityID{SubZone: "locality-1"},
						Priority:  1,
						Weight:    1,
					},
					{
						Endpoints: []Endpoint{{Address: "addr2:159"}},
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
				typeURL:          edsURL,
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

	v2c := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()

	if v2c.handleEDSResponse(badResourceTypeInEDSResponse) == nil {
		t.Fatal("v2c.handleEDSResponse() succeeded, should have failed")
	}

	if v2c.handleEDSResponse(goodEDSResponse1) != nil {
		t.Fatal("v2c.handleEDSResponse() succeeded, should have failed")
	}
}
