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

package v2

import (
	"context"
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
)

// doLDS makes a LDS watch, and waits for the response and ack to finish.
//
// This is called by RDS tests to start LDS first, because LDS is a
// pre-requirement for RDS, and RDS handle would fail without an existing LDS
// watch.
func doLDS(ctx context.Context, t *testing.T, v2c xdsclient.APIClient, fakeServer *fakeserver.Server) {
	v2c.AddWatch(xdsclient.ListenerResource, goodLDSTarget1)
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for LDS request: %v", err)
	}
}

// TestRDSHandleResponseWithRouting starts a fake xDS server, makes a ClientConn
// to it, and creates a v2Client using it. Then, it registers an LDS and RDS
// watcher and tests different RDS responses.
func (s) TestRDSHandleResponseWithRouting(t *testing.T) {
	tests := []struct {
		name          string
		rdsResponse   *xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *xdsclient.RouteConfigUpdate
		wantUpdateErr bool
	}{
		// Badly marshaled RDS response.
		{
			name:          "badly-marshaled-response",
			rdsResponse:   badlyMarshaledRDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response does not contain RouteConfiguration proto.
		{
			name:          "no-route-config-in-response",
			rdsResponse:   badResourceTypeInRDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// No VirtualHosts in the response. Just one test case here for a bad
		// RouteConfiguration, since the others are covered in
		// TestGetClusterFromRouteConfiguration.
		{
			name:        "no-virtual-hosts-in-response",
			rdsResponse: noVirtualHostsInRDSResponse,
			wantErr:     false,
			wantUpdate: &xdsclient.RouteConfigUpdate{
				VirtualHosts: nil,
			},
			wantUpdateErr: false,
		},
		// Response contains one good RouteConfiguration, uninteresting though.
		{
			name:          "one-uninteresting-route-config",
			rdsResponse:   goodRDSResponse2,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one good interesting RouteConfiguration.
		{
			name:        "one-good-route-config",
			rdsResponse: goodRDSResponse1,
			wantErr:     false,
			wantUpdate: &xdsclient.RouteConfigUpdate{
				VirtualHosts: []*xdsclient.VirtualHost{
					{
						Domains: []string{uninterestingDomain},
						Routes:  []*xdsclient.Route{{Prefix: newStringP(""), Action: map[string]uint32{uninterestingClusterName: 1}}},
					},
					{
						Domains: []string{goodLDSTarget1},
						Routes:  []*xdsclient.Route{{Prefix: newStringP(""), Action: map[string]uint32{goodClusterName1: 1}}},
					},
				},
			},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				rType:            xdsclient.RouteConfigResource,
				resourceName:     goodRouteName1,
				responseToHandle: test.rdsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateErr:    test.wantUpdateErr,
			})
		})
	}
}

// TestRDSHandleResponseWithoutRDSWatch tests the case where the v2Client
// receives an RDS response without a registered RDS watcher.
func (s) TestRDSHandleResponseWithoutRDSWatch(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(xdsclient.ResourceType, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	doLDS(ctx, t, v2c, fakeServer)

	if v2c.handleRDSResponse(badResourceTypeInRDSResponse) == nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}

	if v2c.handleRDSResponse(goodRDSResponse1) != nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}
}
