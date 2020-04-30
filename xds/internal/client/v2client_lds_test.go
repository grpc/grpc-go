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

package client

import (
	"testing"
	"time"

	"github.com/golang/protobuf/proto"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	basepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	anypb "github.com/golang/protobuf/ptypes/any"
)

func (s) TestLDSGetRouteConfig(t *testing.T) {
	tests := []struct {
		name      string
		lis       *xdspb.Listener
		wantRoute string
		wantErr   bool
	}{
		{
			name:      "no-apiListener-field",
			lis:       &xdspb.Listener{},
			wantRoute: "",
			wantErr:   true,
		},
		{
			name:      "badly-marshaled-apiListener",
			lis:       badAPIListener1,
			wantRoute: "",
			wantErr:   true,
		},
		{
			name:      "wrong-type-in-apiListener",
			lis:       badResourceListener,
			wantRoute: "",
			wantErr:   true,
		},
		{
			name:      "empty-httpConnMgr-in-apiListener",
			lis:       listenerWithEmptyHTTPConnMgr,
			wantRoute: "",
			wantErr:   true,
		},
		{
			name:      "scopedRoutes-routeConfig-in-apiListener",
			lis:       listenerWithScopedRoutesRouteConfig,
			wantRoute: "",
			wantErr:   true,
		},
		{
			name: "rds.ConfigSource-in-apiListener-is-not-ADS",
			lis: &xdspb.Listener{
				Name: goodLDSTarget1,
				ApiListener: &listenerpb.ApiListener{
					ApiListener: &anypb.Any{
						TypeUrl: httpConnManagerURL,
						Value: func() []byte {
							cm := &httppb.HttpConnectionManager{
								RouteSpecifier: &httppb.HttpConnectionManager_Rds{
									Rds: &httppb.Rds{
										ConfigSource: &basepb.ConfigSource{
											ConfigSourceSpecifier: &basepb.ConfigSource_Path{
												Path: "/some/path",
											},
										},
										RouteConfigName: goodRouteName1}}}
							mcm, _ := proto.Marshal(cm)
							return mcm
						}()}}},
			wantRoute: "",
			wantErr:   true,
		},
		{
			name:      "goodListener1",
			lis:       goodListener1,
			wantRoute: goodRouteName1,
			wantErr:   false,
		},
	}
	_, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()
	v2c := newV2Client(nil, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotRoute, err := v2c.getRouteConfigNameFromListener(test.lis)
			if gotRoute != test.wantRoute {
				t.Errorf("getRouteConfigNameFromListener(%+v) = %v, want %v", test.lis, gotRoute, test.wantRoute)
			}
			if (err != nil) != test.wantErr {
				t.Errorf("getRouteConfigNameFromListener(%+v) = %v, want %v", test.lis, err, test.wantErr)
			}
		})
	}
}

// TestLDSHandleResponse starts a fake xDS server, makes a ClientConn to it,
// and creates a v2Client using it. Then, it registers a watchLDS and tests
// different LDS responses.
func (s) TestLDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		ldsResponse   *xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *ldsUpdate
		wantUpdateErr bool
	}{
		// Badly marshaled LDS response.
		{
			name:          "badly-marshaled-response",
			ldsResponse:   badlyMarshaledLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response does not contain Listener proto.
		{
			name:          "no-listener-proto-in-response",
			ldsResponse:   badResourceTypeInLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// No APIListener in the response. Just one test case here for a bad
		// ApiListener, since the others are covered in
		// TestGetRouteConfigNameFromListener.
		{
			name:          "no-apiListener-in-response",
			ldsResponse:   noAPIListenerLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one listener and it is good.
		{
			name:          "one-good-listener",
			ldsResponse:   goodLDSResponse1,
			wantErr:       false,
			wantUpdate:    &ldsUpdate{routeName: goodRouteName1},
			wantUpdateErr: false,
		},
		// Response contains multiple good listeners, including the one we are
		// interested in.
		{
			name:          "multiple-good-listener",
			ldsResponse:   ldsResponseWithMultipleResources,
			wantErr:       false,
			wantUpdate:    &ldsUpdate{routeName: goodRouteName1},
			wantUpdateErr: false,
		},
		// Response contains two good listeners (one interesting and one
		// uninteresting), and one badly marshaled listener. This will cause a
		// nack because the uninteresting listener will still be parsed.
		{
			name:          "good-bad-ugly-listeners",
			ldsResponse:   goodBadUglyLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one listener, but we are not interested in it.
		{
			name:          "one-uninteresting-listener",
			ldsResponse:   goodLDSResponse2,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response constains no resources. This is the case where the server
		// does not know about the target we are interested in.
		{
			name:          "empty-response",
			ldsResponse:   emptyLDSResponse,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				typeURL:          ldsURL,
				resourceName:     goodLDSTarget1,
				responseToHandle: test.ldsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateErr:    test.wantUpdateErr,
			})
		})
	}
}

// TestLDSHandleResponseWithoutWatch tests the case where the v2Client receives
// an LDS response without a registered watcher.
func (s) TestLDSHandleResponseWithoutWatch(t *testing.T) {
	_, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()

	if v2c.handleLDSResponse(badResourceTypeInLDSResponse) == nil {
		t.Fatal("v2c.handleLDSResponse() succeeded, should have failed")
	}

	if v2c.handleLDSResponse(goodLDSResponse1) != nil {
		t.Fatal("v2c.handleLDSResponse() succeeded, should have failed")
	}
}
