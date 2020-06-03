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

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	testXDSServer   = "xds-server"
	chanRecvTimeout = 100 * time.Millisecond

	testLDSName = "test-lds"
	testRDSName = "test-rds"
	testCDSName = "test-cds"
	testEDSName = "test-eds"
)

func clientOpts(balancerName string) Options {
	return Options{
		Config: bootstrap.Config{
			BalancerName: balancerName,
			Creds:        grpc.WithInsecure(),
			NodeProto:    &corepb.Node{},
		},
	}
}

func (s) TestNew(t *testing.T) {
	fakeServer, cleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer cleanup()

	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{name: "empty-opts", opts: Options{}, wantErr: true},
		{
			name: "empty-balancer-name",
			opts: Options{
				Config: bootstrap.Config{
					Creds:     grpc.WithInsecure(),
					NodeProto: &corepb.Node{},
				},
			},
			wantErr: true,
		},
		{
			name: "empty-dial-creds",
			opts: Options{
				Config: bootstrap.Config{
					BalancerName: "dummy",
					NodeProto:    &corepb.Node{},
				},
			},
			wantErr: true,
		},
		{
			name: "empty-node-proto",
			opts: Options{
				Config: bootstrap.Config{
					BalancerName: "dummy",
					Creds:        grpc.WithInsecure(),
				},
			},
			wantErr: true,
		},
		{
			name:    "happy-case",
			opts:    clientOpts(fakeServer.Address),
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := New(test.opts)
			if err == nil {
				defer c.Close()
			}
			if (err != nil) != test.wantErr {
				t.Fatalf("New(%+v) = %v, wantErr: %v", test.opts, err, test.wantErr)
			}
		})
	}
}

type testXDSV2Client struct {
	r updateHandler

	addWatches    map[string]*testutils.Channel
	removeWatches map[string]*testutils.Channel
}

func overrideNewXDSV2Client() (<-chan *testXDSV2Client, func()) {
	oldNewXDSV2Client := newXDSV2Client
	ch := make(chan *testXDSV2Client, 1)
	newXDSV2Client = func(parent *Client, cc *grpc.ClientConn, nodeProto *corepb.Node, backoff func(int) time.Duration, logger *grpclog.PrefixLogger) xdsv2Client {
		ret := newTestXDSV2Client(parent)
		ch <- ret
		return ret
	}
	return ch, func() { newXDSV2Client = oldNewXDSV2Client }
}

func newTestXDSV2Client(r updateHandler) *testXDSV2Client {
	addWatches := make(map[string]*testutils.Channel)
	addWatches[ldsURL] = testutils.NewChannel()
	addWatches[rdsURL] = testutils.NewChannel()
	addWatches[cdsURL] = testutils.NewChannel()
	addWatches[edsURL] = testutils.NewChannel()
	removeWatches := make(map[string]*testutils.Channel)
	removeWatches[ldsURL] = testutils.NewChannel()
	removeWatches[rdsURL] = testutils.NewChannel()
	removeWatches[cdsURL] = testutils.NewChannel()
	removeWatches[edsURL] = testutils.NewChannel()
	return &testXDSV2Client{
		r:             r,
		addWatches:    addWatches,
		removeWatches: removeWatches,
	}
}

func (c *testXDSV2Client) addWatch(resourceType, resourceName string) {
	c.addWatches[resourceType].Send(resourceName)
}

func (c *testXDSV2Client) removeWatch(resourceType, resourceName string) {
	c.removeWatches[resourceType].Send(resourceName)
}

func (c *testXDSV2Client) close() {}

// TestWatchCallAnotherWatch covers the case where watch() is called inline by a
// callback. It makes sure it doesn't cause a deadlock.
func (s) TestWatchCallAnotherWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	firstTime := true
	c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
		// Calls another watch inline, to ensure there's deadlock.
		c.WatchCluster("another-random-name", func(ClusterUpdate, error) {})
		if _, err := v2Client.addWatches[cdsURL].Receive(); firstTime && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		firstTime = false
	})
	if _, err := v2Client.addWatches[cdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.newCDSUpdate(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	wantUpdate2 := ClusterUpdate{ServiceName: testEDSName + "2"}
	v2Client.r.newCDSUpdate(map[string]ClusterUpdate{
		testCDSName: wantUpdate2,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
}
