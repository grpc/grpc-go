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
	"google.golang.org/grpc"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
)

type watchHandleTestcase struct {
	typeURL      string
	resourceName string

	responseToHandle *xdspb.DiscoveryResponse
	wantHandleErr    bool
	wantUpdate       interface{}
	wantUpdateErr    bool
}

type testUpdateReceiver struct {
	f func(typeURL string, d map[string]interface{})
}

func (t *testUpdateReceiver) newLDSUpdate(d map[string]ldsUpdate) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(ldsURL, dd)
}

func (t *testUpdateReceiver) newRDSUpdate(d map[string]rdsUpdate) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(rdsURL, dd)
}

func (t *testUpdateReceiver) newCDSUpdate(d map[string]ClusterUpdate) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(cdsURL, dd)
}

func (t *testUpdateReceiver) newEDSUpdate(d map[string]EndpointsUpdate) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(edsURL, dd)
}

func (t *testUpdateReceiver) newUpdate(typeURL string, d map[string]interface{}) {
	t.f(typeURL, d)
}

// testWatchHandle is called to test response handling for each xDS.
//
// It starts the xDS watch as configured in test, waits for the fake xds server
// to receive the request (so watch callback is installed), and calls
// handleXDSResp with responseToHandle (if it's set). It then compares the
// update received by watch callback with the expected results.
func testWatchHandle(t *testing.T, test *watchHandleTestcase) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	type updateErr struct {
		u   interface{}
		err error
	}
	gotUpdateCh := testutils.NewChannel()

	v2c := newV2Client(&testUpdateReceiver{
		f: func(typeURL string, d map[string]interface{}) {
			if typeURL == test.typeURL {
				if u, ok := d[test.resourceName]; ok {
					gotUpdateCh.Send(updateErr{u, nil})
				}
			}
		},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()

	// RDS needs an existin LDS watch for the hostname.
	if test.typeURL == rdsURL {
		doLDS(t, v2c, fakeServer)
	}

	// Register the watcher, this will also trigger the v2Client to send the xDS
	// request.
	v2c.addWatch(test.typeURL, test.resourceName)

	// Wait till the request makes it to the fakeServer. This ensures that
	// the watch request has been processed by the v2Client.
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout waiting for an xDS request: %v", err)
	}

	// Directly push the response through a call to handleXDSResp. This bypasses
	// the fakeServer, so it's only testing the handle logic. Client response
	// processing is covered elsewhere.
	//
	// Also note that this won't trigger ACK, so there's no need to clear the
	// request channel afterwards.
	var handleXDSResp func(response *xdspb.DiscoveryResponse) error
	switch test.typeURL {
	case ldsURL:
		handleXDSResp = v2c.handleLDSResponse
	case rdsURL:
		handleXDSResp = v2c.handleRDSResponse
	case cdsURL:
		handleXDSResp = v2c.handleCDSResponse
	case edsURL:
		handleXDSResp = v2c.handleEDSResponse
	}
	if err := handleXDSResp(test.responseToHandle); (err != nil) != test.wantHandleErr {
		t.Fatalf("v2c.handleRDSResponse() returned err: %v, wantErr: %v", err, test.wantHandleErr)
	}

	// If the test doesn't expect the callback to be invoked, verify that no
	// update or error is pushed to the callback.
	//
	// Cannot directly compare test.wantUpdate with nil (typed vs non-typed nil:
	// https://golang.org/doc/faq#nil_error).
	if c := test.wantUpdate; c == nil || (reflect.ValueOf(c).Kind() == reflect.Ptr && reflect.ValueOf(c).IsNil()) {
		update, err := gotUpdateCh.Receive()
		if err == testutils.ErrRecvTimeout {
			return
		}
		t.Fatalf("Unexpected update: +%v", update)
	}

	wantUpdate := reflect.ValueOf(test.wantUpdate).Elem().Interface()
	uErr, err := gotUpdateCh.Receive()
	if err == testutils.ErrRecvTimeout {
		t.Fatal("Timeout expecting xDS update")
	}
	gotUpdate := uErr.(updateErr).u
	opt := cmp.AllowUnexported(rdsUpdate{}, ldsUpdate{}, ClusterUpdate{}, EndpointsUpdate{})
	if diff := cmp.Diff(gotUpdate, wantUpdate, opt); diff != "" {
		t.Fatalf("got update : %+v, want %+v, diff: %s", gotUpdate, wantUpdate, diff)
	}
	gotUpdateErr := uErr.(updateErr).err
	if (gotUpdateErr != nil) != test.wantUpdateErr {
		t.Fatalf("got xDS update error {%v}, wantErr: %v", gotUpdateErr, test.wantUpdateErr)
	}
}

// startServerAndGetCC starts a fake XDS server and also returns a ClientConn
// connected to it.
func startServerAndGetCC(t *testing.T) (*fakeserver.Server, *grpc.ClientConn, func()) {
	t.Helper()

	fs, sCleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}

	cc, ccCleanup, err := fs.XDSClientConn()
	if err != nil {
		sCleanup()
		t.Fatalf("Failed to get a clientConn to the fake xDS server: %v", err)
	}
	return fs, cc, func() {
		sCleanup()
		ccCleanup()
	}
}

// waitForNilErr waits for a nil error value to be received on the
// provided channel.
func waitForNilErr(t *testing.T, ch *testutils.Channel) {
	t.Helper()

	val, err := ch.Receive()
	if err == testutils.ErrRecvTimeout {
		t.Fatalf("Timeout expired when expecting update")
	}
	if val != nil {
		if cbErr := val.(error); cbErr != nil {
			t.Fatal(cbErr)
		}
	}
}
