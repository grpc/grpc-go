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
	"reflect"
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/client/fakexds"
)

type watchHandleConfig struct {
	typeURL string

	// Only one of the following should be non-nil. The one corresponding with
	// typeURL will be called.
	ldsWatch func(target string, ldsCb ldsCallback) (cancel func())
	rdsWatch func(routeName string, rdsCb rdsCallback) (cancel func())
	cdsWatch func(clusterName string, cdsCb cdsCallback) (cancel func())
	edsWatch func(clusterName string, edsCb edsCallback) (cancel func())

	handle func(response *xdspb.DiscoveryResponse) error
}

type watchHandleTestcase struct {
	responseToHandle *xdspb.DiscoveryResponse
	wantErr          bool
	wantUpdate       interface{}
	wantUpdateErr    bool
}

func testWatchHandle(t *testing.T, test *watchHandleTestcase, testConfig *watchHandleConfig, fakeServer *fakexds.Server) {
	// t.Helper()
	gotUpdateCh := make(chan interface{}, 1)
	gotUpdateErrCh := make(chan error, 1)

	var cancelWatch func()
	switch testConfig.typeURL {
	case listenerURL:
	case routeURL:
		// Register a watcher, to trigger the v2Client to send an RDS request.
		cancelWatch = testConfig.rdsWatch(goodRouteName1, func(u rdsUpdate, err error) {
			t.Logf("in v2c.watchRDS callback, rdsUpdate: %+v, err: %v", u, err)
			gotUpdateCh <- u
			gotUpdateErrCh <- err
		})
	case clusterURL:
	case endpointURL:
		// Register a watcher, to trigger the v2Client to send an EDS request.
		cancelWatch = testConfig.edsWatch(goodEDSName, func(u *EDSUpdate, err error) {
			t.Logf("in v2c.watchEDS callback, edsUpdate: %+v, err: %v", u, err)
			gotUpdateCh <- *u
			gotUpdateErrCh <- err
		})
	default:
		t.Fatalf("unknown typeURL: %s", testConfig.typeURL)
	}
	defer cancelWatch()

	// Wait till the request makes it to the fakeServer. This ensures that
	// the watch request has been processed by the v2Client.
	<-fakeServer.RequestChan

	// Directly push the response through a call to handleRDSResponse,
	// thereby bypassing the fakeServer.
	if err := testConfig.handle(test.responseToHandle); (err != nil) != test.wantErr {
		t.Fatalf("v2c.handleRDSResponse() returned err: %v, wantErr: %v", err, test.wantErr)
	}

	// If the test needs the callback to be invoked, verify the update and
	// error pushed to the callback.
	//
	// Cannot directly compare test.wantUpdate with nil (typed vs non-types nil).
	if c := test.wantUpdate; c == nil || (reflect.ValueOf(c).Kind() == reflect.Ptr && reflect.ValueOf(c).IsNil()) {
		timer := time.NewTimer(time.Millisecond * 500)
		select {
		case <-timer.C:
			return
		case <-gotUpdateCh:
			t.Fatal("Unexpected update")
		}
	}

	wantUpdate := reflect.ValueOf(test.wantUpdate).Elem().Interface()
	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout expecting RDS update")
	case gotUpdate := <-gotUpdateCh:
		timer.Stop()
		if diff := cmp.Diff(gotUpdate, wantUpdate, cmp.AllowUnexported(rdsUpdate{})); diff != "" {
			t.Fatalf("got update : %+v, want %+v, diff: %s", gotUpdate, wantUpdate, diff)
		}
	}
	// Since the callback that we registered pushes to both channels at
	// the same time, this channel read should return immediately.
	gotUpdateErr := <-gotUpdateErrCh
	if (gotUpdateErr != nil) != test.wantUpdateErr {
		t.Fatalf("got RDS update error {%v}, wantErr: %v", gotUpdateErr, test.wantUpdateErr)
	}
}
