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
	"reflect"
	"testing"
	"time"

	discoverypb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc/xds/internal/client/fakexds"
)

func TestHandleLDSResponse(t *testing.T) {
	fakeServer, client, cleanup := fakexds.StartClientAndServer(t)
	defer cleanup()

	v2c := newV2Client(client, goodNodeProto, func(int) time.Duration { return 0 })

	tests := []struct {
		name          string
		ldsTarget     string
		ldsResponse   *discoverypb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *ldsUpdate
		wantUpdateErr bool
	}{
		// Response contains one listener and it is good.
		{
			name:          "one-good-listener",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   goodLDSResponse1,
			wantErr:       false,
			wantUpdate:    &ldsUpdate{routeName: goodRouteName1},
			wantUpdateErr: false,
		},
		// Response contains multiple good listeners, including the one we are
		// interested in.
		{
			name:          "multiple-good-listener",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   ldsResponseWithMultipleResources,
			wantErr:       false,
			wantUpdate:    &ldsUpdate{routeName: goodRouteName1},
			wantUpdateErr: false,
		},
		// Response contains two good listeners (one interesting and one
		// uninteresting), and one badly marshaled listener.
		{
			name:          "good-bad-ugly-listeners",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   goodBadUglyLDSResponse,
			wantErr:       false,
			wantUpdate:    &ldsUpdate{routeName: goodRouteName1},
			wantUpdateErr: false,
		},
		// Response contains one listener, but we are not interested in it.
		{
			name:          "one-uninteresting-listener",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   goodLDSResponse2,
			wantErr:       false,
			wantUpdate:    &ldsUpdate{routeName: ""},
			wantUpdateErr: true,
		},
		// Response constains no resources. This is the case where the server
		// does not know about the target we are interested in.
		{
			name:          "empty-response",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   &discoverypb.DiscoveryResponse{TypeUrl: listenerURL},
			wantErr:       false,
			wantUpdate:    &ldsUpdate{routeName: ""},
			wantUpdateErr: true,
		},
		// Badly marshaled LDS response.
		{
			name:          "badly-marshaled-response",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   badlyMarshaledLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response does not contain Listener proto.
		{
			name:          "no-listener-proto-in-response",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   badResourceTypeInLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// No APIListener in the response.
		{
			name:          "no-apiListener-in-response",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   noAPIListenerLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Badly marshaled APIListener in the response.
		{
			name:          "badly-marshaled-apiListener-in-response",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   badlyMarshaledAPIListenerInLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// ApiListener does not contain HttpConnectionManager
		{
			name:          "no-httpConnMrg-in-apiListener",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   badResourceTypeInAPIListenerInLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// No route config name in HttpConnectionManager.
		{
			name:          "no-routeconfig-in-httpConnMrg",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   ldsResponseWithEmptyHttpConnMgr,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// RouteConfig inline in HttpConnectionManager.
		{
			name:          "routeconfig-inline-in-httpConnMrg",
			ldsTarget:     goodLDSTarget1,
			ldsResponse:   ldsResponseWithInlineRouteConfig,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
	}

	for _, test := range tests {
		gotUpdateCh := make(chan ldsUpdate, 1)
		gotUpdateErrCh := make(chan error, 1)

		// Register a watcher, to trigger the v2Client to send an LDS request.
		cancelWatch := v2c.watchLDS(test.ldsTarget, func(u ldsUpdate, err error) {
			t.Logf("%s: in v2c.watchLDS callback, ldsUpdate: %+v, err: %v", test.name, u, err)
			gotUpdateCh <- u
			gotUpdateErrCh <- err
		})

		// Wait till the request makes it to the fakeServer. This ensures that
		// the watch request has been processed by the v2Client.
		<-fakeServer.RequestChan

		// Directly push the response through a call to handleLDSResponse,
		// thereby bypassing the fakeServer.
		if err := v2c.handleLDSResponse(test.ldsResponse); (err != nil) != test.wantErr {
			t.Fatalf("%s: v2c.handleLDSResponse() returned err: %v, wantErr: %v", test.name, err, test.wantErr)
		}

		// If the test needs the callback to be invoked, verify the update and
		// error pushed to the callback.
		if test.wantUpdate != nil {
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-timer.C:
				t.Fatal("time out when expecting LDS update")
			case gotUpdate := <-gotUpdateCh:
				if !reflect.DeepEqual(gotUpdate, *test.wantUpdate) {
					t.Fatalf("%s: got LDS update : %+v, want %+v", test.name, gotUpdate, *test.wantUpdate)
				}
			}
			// Since the callback that we registered pushes to both channels at
			// the same time, this channel read should return immediately.
			gotUpdateErr := <-gotUpdateErrCh
			if (gotUpdateErr != nil) != test.wantUpdateErr {
				t.Fatalf("%s: got LDS update error {%v}, wantErr: %v", test.name, gotUpdateErr, test.wantUpdateErr)
			}
		}
		cancelWatch()
	}
}
