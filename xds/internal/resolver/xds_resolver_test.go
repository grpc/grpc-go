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

package resolver

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/internal/grpctest"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer" // To parse LB config
	"google.golang.org/grpc/xds/internal/balancer/clustermanager"
	"google.golang.org/grpc/xds/internal/client"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const (
	targetStr               = "target"
	routeStr                = "route"
	cluster                 = "cluster"
	defaultTestTimeout      = 1 * time.Second
	defaultTestShortTimeout = 100 * time.Microsecond
)

var target = resolver.Target{Endpoint: targetStr}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestRegister(t *testing.T) {
	b := resolver.Get(xdsScheme)
	if b == nil {
		t.Errorf("scheme %v is not registered", xdsScheme)
	}
}

// testClientConn is a fake implemetation of resolver.ClientConn. All is does
// is to store the state received from the resolver locally and signal that
// event through a channel.
type testClientConn struct {
	resolver.ClientConn
	stateCh *testutils.Channel
	errorCh *testutils.Channel
}

func (t *testClientConn) UpdateState(s resolver.State) {
	t.stateCh.Send(s)
}

func (t *testClientConn) ReportError(err error) {
	t.errorCh.Send(err)
}

func (t *testClientConn) ParseServiceConfig(jsonSC string) *serviceconfig.ParseResult {
	return internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(jsonSC)
}

func newTestClientConn() *testClientConn {
	return &testClientConn{
		stateCh: testutils.NewChannel(),
		errorCh: testutils.NewChannel(),
	}
}

// TestResolverBuilder tests the xdsResolverBuilder's Build method with
// different parameters.
func (s) TestResolverBuilder(t *testing.T) {
	tests := []struct {
		name          string
		xdsClientFunc func() (xdsClientInterface, error)
		wantErr       bool
	}{
		{
			name: "simple-good",
			xdsClientFunc: func() (xdsClientInterface, error) {
				return fakeclient.NewClient(), nil
			},
			wantErr: false,
		},
		{
			name: "newXDSClient-throws-error",
			xdsClientFunc: func() (xdsClientInterface, error) {
				return nil, errors.New("newXDSClient-throws-error")
			},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Fake out the xdsClient creation process by providing a fake.
			oldClientMaker := newXDSClient
			newXDSClient = test.xdsClientFunc
			defer func() {
				newXDSClient = oldClientMaker
			}()

			builder := resolver.Get(xdsScheme)
			if builder == nil {
				t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
			}

			r, err := builder.Build(target, newTestClientConn(), resolver.BuildOptions{})
			if (err != nil) != test.wantErr {
				t.Fatalf("builder.Build(%v) returned err: %v, wantErr: %v", target, err, test.wantErr)
			}
			if err != nil {
				// This is the case where we expect an error and got it.
				return
			}
			r.Close()
		})
	}
}

type setupOpts struct {
	xdsClientFunc func() (xdsClientInterface, error)
}

func testSetup(t *testing.T, opts setupOpts) (*xdsResolver, *testClientConn, func()) {
	t.Helper()

	oldClientMaker := newXDSClient
	newXDSClient = opts.xdsClientFunc
	cancel := func() {
		newXDSClient = oldClientMaker
	}

	builder := resolver.Get(xdsScheme)
	if builder == nil {
		t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
	}

	tcc := newTestClientConn()
	r, err := builder.Build(target, tcc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("builder.Build(%v) returned err: %v", target, err)
	}
	return r.(*xdsResolver), tcc, cancel
}

// waitForWatchListener waits for the WatchListener method to be called on the
// xdsClient within a reasonable amount of time, and also verifies that the
// watch is called with the expected target.
func waitForWatchListener(ctx context.Context, t *testing.T, xdsC *fakeclient.Client, wantTarget string) {
	t.Helper()

	gotTarget, err := xdsC.WaitForWatchListener(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchService failed with error: %v", err)
	}
	if gotTarget != wantTarget {
		t.Fatalf("xdsClient.WatchService() called with target: %v, want %v", gotTarget, wantTarget)
	}
}

// waitForWatchRouteConfig waits for the WatchRoute method to be called on the
// xdsClient within a reasonable amount of time, and also verifies that the
// watch is called with the expected target.
func waitForWatchRouteConfig(ctx context.Context, t *testing.T, xdsC *fakeclient.Client, wantTarget string) {
	t.Helper()

	gotTarget, err := xdsC.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchService failed with error: %v", err)
	}
	if gotTarget != wantTarget {
		t.Fatalf("xdsClient.WatchService() called with target: %v, want %v", gotTarget, wantTarget)
	}
}

// TestXDSResolverWatchCallbackAfterClose tests the case where a service update
// from the underlying xdsClient is received after the resolver is closed.
func (s) TestXDSResolverWatchCallbackAfterClose(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsClientInterface, error) { return xdsC, nil },
	})
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Call the watchAPI callback after closing the resolver, and make sure no
	// update is triggerred on the ClientConn.
	xdsR.Close()
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{cluster: 1}}},
			},
		},
	}, nil)

	if gotVal, gotErr := tcc.stateCh.Receive(ctx); gotErr != context.DeadlineExceeded {
		t.Fatalf("ClientConn.UpdateState called after xdsResolver is closed: %v", gotVal)
	}
}

// TestXDSResolverBadServiceUpdate tests the case the xdsClient returns a bad
// service update.
func (s) TestXDSResolverBadServiceUpdate(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsClientInterface, error) { return xdsC, nil },
	})
	defer func() {
		cancel()
		xdsR.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a bad service update and wait for the
	// ReportError method to be called on the ClientConn.
	suErr := errors.New("bad serviceupdate")
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{}, suErr)

	if gotErrVal, gotErr := tcc.errorCh.Receive(ctx); gotErr != nil || gotErrVal != suErr {
		t.Fatalf("ClientConn.ReportError() received %v, want %v", gotErrVal, suErr)
	}
}

// TestXDSResolverGoodServiceUpdate tests the happy case where the resolver
// gets a good service update from the xdsClient.
func (s) TestXDSResolverGoodServiceUpdate(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsClientInterface, error) { return xdsC, nil },
	})
	defer func() {
		cancel()
		xdsR.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
	defer replaceRandNumGenerator(0)()

	for _, tt := range []struct {
		routes       []*xdsclient.Route
		wantJSON     string
		wantClusters map[string]bool
	}{
		{
			routes: []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{"test-cluster-1": 1}}},
			wantJSON: `{"loadBalancingConfig":[{
    "xds_cluster_manager_experimental":{
      "children":{
        "test-cluster-1":{
          "childPolicy":[{"cds_experimental":{"cluster":"test-cluster-1"}}]
        }
      }
    }}]}`,
			wantClusters: map[string]bool{"test-cluster-1": true},
		},
		{
			routes: []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{
				"cluster_1": 75,
				"cluster_2": 25,
			}}},
			// This update contains the cluster from the previous update as
			// well as this update, as the previous config selector still
			// references the old cluster when the new one is pushed.
			wantJSON: `{"loadBalancingConfig":[{
    "xds_cluster_manager_experimental":{
      "children":{
        "test-cluster-1":{
          "childPolicy":[{"cds_experimental":{"cluster":"test-cluster-1"}}]
        },
        "cluster_1":{
          "childPolicy":[{"cds_experimental":{"cluster":"cluster_1"}}]
        },
        "cluster_2":{
          "childPolicy":[{"cds_experimental":{"cluster":"cluster_2"}}]
        }
      }
    }}]}`,
			wantClusters: map[string]bool{"cluster_1": true, "cluster_2": true},
		},
		{
			routes: []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{
				"cluster_1": 75,
				"cluster_2": 25,
			}}},
			// With this redundant update, the old config selector has been
			// stopped, so there are no more references to the first cluster.
			// Only the second update's clusters should remain.
			wantJSON: `{"loadBalancingConfig":[{
    "xds_cluster_manager_experimental":{
      "children":{
        "cluster_1":{
          "childPolicy":[{"cds_experimental":{"cluster":"cluster_1"}}]
        },
        "cluster_2":{
          "childPolicy":[{"cds_experimental":{"cluster":"cluster_2"}}]
        }
      }
    }}]}`,
			wantClusters: map[string]bool{"cluster_1": true, "cluster_2": true},
		},
	} {
		// Invoke the watchAPI callback with a good service update and wait for the
		// UpdateState method to be called on the ClientConn.
		xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{
			VirtualHosts: []*xdsclient.VirtualHost{
				{
					Domains: []string{targetStr},
					Routes:  tt.routes,
				},
			},
		}, nil)

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		gotState, err := tcc.stateCh.Receive(ctx)
		if err != nil {
			t.Fatalf("ClientConn.UpdateState returned error: %v", err)
		}
		rState := gotState.(resolver.State)
		if err := rState.ServiceConfig.Err; err != nil {
			t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
		}

		wantSCParsed := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(tt.wantJSON)
		if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
			t.Errorf("ClientConn.UpdateState received different service config")
			t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
			t.Error("want: ", cmp.Diff(nil, wantSCParsed.Config))
		}

		cs := iresolver.GetConfigSelector(rState)
		if cs == nil {
			t.Error("received nil config selector")
			continue
		}

		pickedClusters := make(map[string]bool)
		// Odds of picking 75% cluster 100 times in a row: 1 in 3E-13.  And
		// with the random number generator stubbed out, we can rely on this
		// to be 100% reproducible.
		for i := 0; i < 100; i++ {
			res, err := cs.SelectConfig(iresolver.RPCInfo{Context: context.Background()})
			if err != nil {
				t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
			}
			cluster := clustermanager.GetPickedClusterForTesting(res.Context)
			pickedClusters[cluster] = true
			res.OnCommitted()
		}
		if !reflect.DeepEqual(pickedClusters, tt.wantClusters) {
			t.Errorf("Picked clusters: %v; want: %v", pickedClusters, tt.wantClusters)
		}
	}
}

type fakeWRR struct {
	t       *testing.T
	weights map[string]int64
	ch      chan string
}

func (f *fakeWRR) Add(item interface{}, weight int64) {
	// Called when clusters are added.  "item" is the cluster name.
	cluster := item.(string)
	if f.weights[cluster] != weight {
		f.t.Errorf("unexpected item or weight added to WRR: %q, %v", cluster, weight)
	}
	delete(f.weights, cluster)
}

func (f *fakeWRR) Next() interface{} {
	return <-f.ch
}

func (s) TestXDSResolverWRR(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsClientInterface, error) { return xdsC, nil },
	})
	defer func() {
		cancel()
		xdsR.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	fakePick := &fakeWRR{t: t, weights: map[string]int64{"A": 75, "B": 25}, ch: make(chan string, 1)}
	defer func(oldNewWRR func() wrr.WRR) { newWRR = oldNewWRR }(newWRR)
	newWRR = func() wrr.WRR { return fakePick }

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes: []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{
					"A": 75,
					"B": 25,
				}}},
			},
		},
	}, nil)

	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("ClientConn.UpdateState returned error: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}

	if len(fakePick.weights) != 0 {
		t.Fatalf("Not all expected clusters added to WRR. Remaining: %v", fakePick.weights)
	}
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("received nil config selector")
	}

	for i := 0; i < 100; i++ {
		want := "A"
		if i%2 == 1 {
			want = "B"
		}
		fakePick.ch <- want
		res, err := cs.SelectConfig(iresolver.RPCInfo{Context: context.Background()})
		if err != nil {
			t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
		}
		got := clustermanager.GetPickedClusterForTesting(res.Context)
		if got != want {
			t.Fatalf("Picked cluster %q; want %q", got, want)
		}
		res.OnCommitted()
	}
}

// TestXDSResolverDelayedOnCommitted tests that clusters remain in service
// config if RPCs are in flight.
func (s) TestXDSResolverDelayedOnCommitted(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsClientInterface, error) { return xdsC, nil },
	})
	defer func() {
		cancel()
		xdsR.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{"test-cluster-1": 1}}},
			},
		},
	}, nil)

	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("ClientConn.UpdateState returned error: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}

	wantJSON := `{"loadBalancingConfig":[{
    "xds_cluster_manager_experimental":{
      "children":{
        "test-cluster-1":{
          "childPolicy":[{"cds_experimental":{"cluster":"test-cluster-1"}}]
        }
      }
    }}]}`
	wantSCParsed := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(wantJSON)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Fatal("want: ", cmp.Diff(nil, wantSCParsed.Config))
	}

	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("received nil config selector")
	}

	res, err := cs.SelectConfig(iresolver.RPCInfo{Context: context.Background()})
	if err != nil {
		t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
	}
	cluster := clustermanager.GetPickedClusterForTesting(res.Context)
	if cluster != "test-cluster-1" {
		t.Fatalf("")
	}
	// delay res.OnCommitted()

	// Perform TWO updates to ensure the old config selector does not hold a
	// reference to test-cluster-1.
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{"NEW": 1}}},
			},
		},
	}, nil)
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{"NEW": 1}}},
			},
		},
	}, nil)

	tcc.stateCh.Receive(ctx) // Ignore the first update
	gotState, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("ClientConn.UpdateState returned error: %v", err)
	}
	rState = gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantJSON2 := `{"loadBalancingConfig":[{
    "xds_cluster_manager_experimental":{
      "children":{
        "test-cluster-1":{
          "childPolicy":[{"cds_experimental":{"cluster":"test-cluster-1"}}]
        },
        "NEW":{
          "childPolicy":[{"cds_experimental":{"cluster":"NEW"}}]
        }
      }
    }}]}`
	wantSCParsed2 := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(wantJSON2)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed2.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Fatal("want: ", cmp.Diff(nil, wantSCParsed2.Config))
	}

	// Invoke OnCommitted; should lead to a service config update that deletes
	// test-cluster-1.
	res.OnCommitted()

	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{"NEW": 1}}},
			},
		},
	}, nil)
	gotState, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("ClientConn.UpdateState returned error: %v", err)
	}
	rState = gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantJSON3 := `{"loadBalancingConfig":[{
    "xds_cluster_manager_experimental":{
      "children":{
        "NEW":{
          "childPolicy":[{"cds_experimental":{"cluster":"NEW"}}]
        }
      }
    }}]}`
	wantSCParsed3 := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(wantJSON3)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed3.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Fatal("want: ", cmp.Diff(nil, wantSCParsed3.Config))
	}
}

// TestXDSResolverUpdates tests the cases where the resolver gets a good update
// after an error, and an error after the good update.
func (s) TestXDSResolverGoodUpdateAfterError(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsClientInterface, error) { return xdsC, nil },
	})
	defer func() {
		cancel()
		xdsR.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a bad service update and wait for the
	// ReportError method to be called on the ClientConn.
	suErr := errors.New("bad serviceupdate")
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{}, suErr)

	if gotErrVal, gotErr := tcc.errorCh.Receive(ctx); gotErr != nil || gotErrVal != suErr {
		t.Fatalf("ClientConn.ReportError() received %v, want %v", gotErrVal, suErr)
	}

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*client.Route{{Prefix: newStringP(""), Action: map[string]uint32{cluster: 1}}},
			},
		},
	}, nil)
	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("ClientConn.UpdateState returned error: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}

	// Invoke the watchAPI callback with a bad service update and wait for the
	// ReportError method to be called on the ClientConn.
	suErr2 := errors.New("bad serviceupdate 2")
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{}, suErr2)
	if gotErrVal, gotErr := tcc.errorCh.Receive(ctx); gotErr != nil || gotErrVal != suErr2 {
		t.Fatalf("ClientConn.ReportError() received %v, want %v", gotErrVal, suErr2)
	}
}

// TestXDSResolverResourceNotFoundError tests the cases where the resolver gets
// a ResourceNotFoundError. It should generate a service config picking
// weighted_target, but no child balancers.
func (s) TestXDSResolverResourceNotFoundError(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsClientInterface, error) { return xdsC, nil },
	})
	defer func() {
		cancel()
		xdsR.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a bad service update and wait for the
	// ReportError method to be called on the ClientConn.
	suErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "resource removed error")
	xdsC.InvokeWatchRouteConfigCallback(xdsclient.RouteConfigUpdate{}, suErr)

	if gotErrVal, gotErr := tcc.errorCh.Receive(ctx); gotErr != context.DeadlineExceeded {
		t.Fatalf("ClientConn.ReportError() received %v, %v, want channel recv timeout", gotErrVal, gotErr)
	}

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("ClientConn.UpdateState returned error: %v", err)
	}
	rState := gotState.(resolver.State)
	wantParsedConfig := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)("{}")
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantParsedConfig.Config) {
		t.Error("ClientConn.UpdateState got wrong service config")
		t.Errorf("gotParsed: %s", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Errorf("wantParsed: %s", cmp.Diff(nil, wantParsedConfig.Config))
	}
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}
}

func replaceRandNumGenerator(start int64) func() {
	nextInt := start
	grpcrandInt63n = func(int64) (ret int64) {
		ret = nextInt
		nextInt++
		return
	}
	return func() {
		grpcrandInt63n = grpcrand.Int63n
	}
}
