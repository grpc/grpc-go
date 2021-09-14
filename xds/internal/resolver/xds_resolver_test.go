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
	"strings"
	"testing"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/internal/grpctest"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/internal/xds/env"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer" // To parse LB config
	"google.golang.org/grpc/xds/internal/balancer/clustermanager"
	"google.golang.org/grpc/xds/internal/balancer/ringhash"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/grpc/xds/internal/httpfilter/router"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
)

const (
	targetStr               = "target"
	routeStr                = "route"
	cluster                 = "cluster"
	defaultTestTimeout      = 1 * time.Second
	defaultTestShortTimeout = 100 * time.Microsecond
)

var target = resolver.Target{Endpoint: targetStr}

var routerFilter = xdsclient.HTTPFilter{Name: "rtr", Filter: httpfilter.Get(router.TypeURL)}
var routerFilterList = []xdsclient.HTTPFilter{routerFilter}

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

func (t *testClientConn) UpdateState(s resolver.State) error {
	t.stateCh.Send(s)
	return nil
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
		xdsClientFunc func() (xdsclient.XDSClient, error)
		wantErr       bool
	}{
		{
			name: "simple-good",
			xdsClientFunc: func() (xdsclient.XDSClient, error) {
				return fakeclient.NewClient(), nil
			},
			wantErr: false,
		},
		{
			name: "newXDSClient-throws-error",
			xdsClientFunc: func() (xdsclient.XDSClient, error) {
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

// TestResolverBuilder_xdsCredsBootstrapMismatch tests the case where an xds
// resolver is built with xds credentials being specified by the user. The
// bootstrap file does not contain any certificate provider configuration
// though, and therefore we expect the resolver build to fail.
func (s) TestResolverBuilder_xdsCredsBootstrapMismatch(t *testing.T) {
	// Fake out the xdsClient creation process by providing a fake, which does
	// not have any certificate provider configuration.
	oldClientMaker := newXDSClient
	newXDSClient = func() (xdsclient.XDSClient, error) {
		fc := fakeclient.NewClient()
		fc.SetBootstrapConfig(&bootstrap.Config{})
		return fc, nil
	}
	defer func() { newXDSClient = oldClientMaker }()

	builder := resolver.Get(xdsScheme)
	if builder == nil {
		t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
	}

	// Create xds credentials to be passed to resolver.Build().
	creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("xds.NewClientCredentials() failed: %v", err)
	}

	// Since the fake xds client is not configured with any certificate provider
	// configs, and we are specifying xds credentials in the call to
	// resolver.Build(), we expect it to fail.
	if _, err := builder.Build(target, newTestClientConn(), resolver.BuildOptions{DialCreds: creds}); err == nil {
		t.Fatal("builder.Build() succeeded when expected to fail")
	}
}

type setupOpts struct {
	xdsClientFunc func() (xdsclient.XDSClient, error)
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
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Call the watchAPI callback after closing the resolver, and make sure no
	// update is triggerred on the ClientConn.
	xdsR.Close()
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)

	if gotVal, gotErr := tcc.stateCh.Receive(ctx); gotErr != context.DeadlineExceeded {
		t.Fatalf("ClientConn.UpdateState called after xdsResolver is closed: %v", gotVal)
	}
}

// TestXDSResolverCloseClosesXDSClient tests that the XDS resolver's Close
// method closes the XDS client.
func (s) TestXDSResolverCloseClosesXDSClient(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, _, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer cancel()
	xdsR.Close()
	if !xdsC.Closed.HasFired() {
		t.Fatalf("xds client not closed by xds resolver Close method")
	}
}

// TestXDSResolverBadServiceUpdate tests the case the xdsClient returns a bad
// service update.
func (s) TestXDSResolverBadServiceUpdate(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a bad service update and wait for the
	// ReportError method to be called on the ClientConn.
	suErr := errors.New("bad serviceupdate")
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{}, suErr)

	if gotErrVal, gotErr := tcc.errorCh.Receive(ctx); gotErr != nil || gotErrVal != suErr {
		t.Fatalf("ClientConn.ReportError() received %v, want %v", gotErrVal, suErr)
	}
}

// TestXDSResolverGoodServiceUpdate tests the happy case where the resolver
// gets a good service update from the xdsClient.
func (s) TestXDSResolverGoodServiceUpdate(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
	defer replaceRandNumGenerator(0)()

	for _, tt := range []struct {
		routes       []*xdsclient.Route
		wantJSON     string
		wantClusters map[string]bool
	}{
		{
			routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{"test-cluster-1": {Weight: 1}}}},
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
			routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{
				"cluster_1": {Weight: 75},
				"cluster_2": {Weight: 25},
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
			routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{
				"cluster_1": {Weight: 75},
				"cluster_2": {Weight: 25},
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
		xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
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
			t.Fatalf("Error waiting for UpdateState to be called: %v", err)
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

// TestXDSResolverRequestHash tests a case where a resolver receives a RouteConfig update
// with a HashPolicy specifying to generate a hash. The configSelector generated should
// successfully generate a Hash.
func (s) TestXDSResolverRequestHash(t *testing.T) {
	oldRH := env.RingHashSupport
	env.RingHashSupport = true
	defer func() { env.RingHashSupport = oldRH }()

	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
	// Invoke watchAPI callback with a good service update (with hash policies
	// specified) and wait for UpdateState method to be called on ClientConn.
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes: []*xdsclient.Route{{
					Prefix: newStringP(""),
					WeightedClusters: map[string]xdsclient.WeightedCluster{
						"cluster_1": {Weight: 75},
						"cluster_2": {Weight: 25},
					},
					HashPolicies: []*xdsclient.HashPolicy{{
						HashPolicyType: xdsclient.HashPolicyTypeHeader,
						HeaderName:     ":path",
					}},
				}},
			},
		},
	}, nil)

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState := gotState.(resolver.State)
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Error("received nil config selector")
	}
	// Selecting a config when there was a hash policy specified in the route
	// that will be selected should put a request hash in the config's context.
	res, err := cs.SelectConfig(iresolver.RPCInfo{Context: metadata.NewOutgoingContext(context.Background(), metadata.Pairs(":path", "/products"))})
	if err != nil {
		t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
	}
	requestHashGot := ringhash.GetRequestHashForTesting(res.Context)
	requestHashWant := xxhash.Sum64String("/products")
	if requestHashGot != requestHashWant {
		t.Fatalf("requestHashGot = %v, requestHashWant = %v", requestHashGot, requestHashWant)
	}
}

// TestXDSResolverRemovedWithRPCs tests the case where a config selector sends
// an empty update to the resolver after the resource is removed.
func (s) TestXDSResolverRemovedWithRPCs(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer cancel()
	defer xdsR.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{"test-cluster-1": {Weight: 1}}}},
			},
		},
	}, nil)

	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}

	// "Make an RPC" by invoking the config selector.
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatalf("received nil config selector")
	}

	res, err := cs.SelectConfig(iresolver.RPCInfo{Context: context.Background()})
	if err != nil {
		t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
	}

	// Delete the resource
	suErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "resource removed error")
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{}, suErr)

	if _, err = tcc.stateCh.Receive(ctx); err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}

	// "Finish the RPC"; this could cause a panic if the resolver doesn't
	// handle it correctly.
	res.OnCommitted()
}

// TestXDSResolverRemovedResource tests for proper behavior after a resource is
// removed.
func (s) TestXDSResolverRemovedResource(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer cancel()
	defer xdsR.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{"test-cluster-1": {Weight: 1}}}},
			},
		},
	}, nil)
	wantJSON := `{"loadBalancingConfig":[{
    "xds_cluster_manager_experimental":{
      "children":{
        "test-cluster-1":{
          "childPolicy":[{"cds_experimental":{"cluster":"test-cluster-1"}}]
        }
      }
    }}]}`
	wantSCParsed := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(wantJSON)

	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Error("want: ", cmp.Diff(nil, wantSCParsed.Config))
	}

	// "Make an RPC" by invoking the config selector.
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatalf("received nil config selector")
	}

	res, err := cs.SelectConfig(iresolver.RPCInfo{Context: context.Background()})
	if err != nil {
		t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
	}

	// "Finish the RPC"; this could cause a panic if the resolver doesn't
	// handle it correctly.
	res.OnCommitted()

	// Delete the resource.  The channel should receive a service config with the
	// original cluster but with an erroring config selector.
	suErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "resource removed error")
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{}, suErr)

	if gotState, err = tcc.stateCh.Receive(ctx); err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState = gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Error("want: ", cmp.Diff(nil, wantSCParsed.Config))
	}

	// "Make another RPC" by invoking the config selector.
	cs = iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatalf("received nil config selector")
	}

	res, err = cs.SelectConfig(iresolver.RPCInfo{Context: context.Background()})
	if err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("Expected UNAVAILABLE error from cs.SelectConfig(_); got %v, %v", res, err)
	}

	// In the meantime, an empty ServiceConfig update should have been sent.
	if gotState, err = tcc.stateCh.Receive(ctx); err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState = gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantSCParsed = internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)("{}")
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Error("want: ", cmp.Diff(nil, wantSCParsed.Config))
	}
}

func (s) TestXDSResolverWRR(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	defer func(oldNewWRR func() wrr.WRR) { newWRR = oldNewWRR }(newWRR)
	newWRR = xdstestutils.NewTestWRR

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes: []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{
					"A": {Weight: 5},
					"B": {Weight: 10},
				}}},
			},
		},
	}, nil)

	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}

	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("received nil config selector")
	}

	picks := map[string]int{}
	for i := 0; i < 30; i++ {
		res, err := cs.SelectConfig(iresolver.RPCInfo{Context: context.Background()})
		if err != nil {
			t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
		}
		picks[clustermanager.GetPickedClusterForTesting(res.Context)]++
		res.OnCommitted()
	}
	want := map[string]int{"A": 10, "B": 20}
	if !reflect.DeepEqual(picks, want) {
		t.Errorf("picked clusters = %v; want %v", picks, want)
	}
}

func (s) TestXDSResolverMaxStreamDuration(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, MaxStreamDuration: time.Second, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	defer func(oldNewWRR func() wrr.WRR) { newWRR = oldNewWRR }(newWRR)
	newWRR = xdstestutils.NewTestWRR

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes: []*xdsclient.Route{{
					Prefix:            newStringP("/foo"),
					WeightedClusters:  map[string]xdsclient.WeightedCluster{"A": {Weight: 1}},
					MaxStreamDuration: newDurationP(5 * time.Second),
				}, {
					Prefix:            newStringP("/bar"),
					WeightedClusters:  map[string]xdsclient.WeightedCluster{"B": {Weight: 1}},
					MaxStreamDuration: newDurationP(0),
				}, {
					Prefix:           newStringP(""),
					WeightedClusters: map[string]xdsclient.WeightedCluster{"C": {Weight: 1}},
				}},
			},
		},
	}, nil)

	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}

	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("received nil config selector")
	}

	testCases := []struct {
		name   string
		method string
		want   *time.Duration
	}{{
		name:   "RDS setting",
		method: "/foo/method",
		want:   newDurationP(5 * time.Second),
	}, {
		name:   "explicit zero in RDS; ignore LDS",
		method: "/bar/method",
		want:   nil,
	}, {
		name:   "no config in RDS; fallback to LDS",
		method: "/baz/method",
		want:   newDurationP(time.Second),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := iresolver.RPCInfo{
				Method:  tc.method,
				Context: context.Background(),
			}
			res, err := cs.SelectConfig(req)
			if err != nil {
				t.Errorf("Unexpected error from cs.SelectConfig(%v): %v", req, err)
				return
			}
			res.OnCommitted()
			got := res.MethodConfig.Timeout
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("For method %q: res.MethodConfig.Timeout = %v; want %v", tc.method, got, tc.want)
			}
		})
	}
}

// TestXDSResolverDelayedOnCommitted tests that clusters remain in service
// config if RPCs are in flight.
func (s) TestXDSResolverDelayedOnCommitted(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{"test-cluster-1": {Weight: 1}}}},
			},
		},
	}, nil)

	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
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
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{"NEW": {Weight: 1}}}},
			},
		},
	}, nil)
	tcc.stateCh.Receive(ctx) // Ignore the first update.

	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{"NEW": {Weight: 1}}}},
			},
		},
	}, nil)

	gotState, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
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

	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{"NEW": {Weight: 1}}}},
			},
		},
	}, nil)
	gotState, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
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
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a bad service update and wait for the
	// ReportError method to be called on the ClientConn.
	suErr := errors.New("bad serviceupdate")
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{}, suErr)

	if gotErrVal, gotErr := tcc.errorCh.Receive(ctx); gotErr != nil || gotErrVal != suErr {
		t.Fatalf("ClientConn.ReportError() received %v, want %v", gotErrVal, suErr)
	}

	// Invoke the watchAPI callback with a good service update and wait for the
	// UpdateState method to be called on the ClientConn.
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
		VirtualHosts: []*xdsclient.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsclient.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsclient.WeightedCluster{cluster: {Weight: 1}}}},
			},
		},
	}, nil)
	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}

	// Invoke the watchAPI callback with a bad service update and wait for the
	// ReportError method to be called on the ClientConn.
	suErr2 := errors.New("bad serviceupdate 2")
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{}, suErr2)
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
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	// Invoke the watchAPI callback with a bad service update and wait for the
	// ReportError method to be called on the ClientConn.
	suErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "resource removed error")
	xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{}, suErr)

	if gotErrVal, gotErr := tcc.errorCh.Receive(ctx); gotErr != context.DeadlineExceeded {
		t.Fatalf("ClientConn.ReportError() received %v, %v, want channel recv timeout", gotErrVal, gotErr)
	}

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
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

// TestXDSResolverMultipleLDSUpdates tests the case where two LDS updates with
// the same RDS name to watch are received without an RDS in between. Those LDS
// updates shouldn't trigger service config update.
//
// This test case also makes sure the resolver doesn't panic.
func (s) TestXDSResolverMultipleLDSUpdates(t *testing.T) {
	xdsC := fakeclient.NewClient()
	xdsR, tcc, cancel := testSetup(t, setupOpts{
		xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
	})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
	defer replaceRandNumGenerator(0)()

	// Send a new LDS update, with the same fields.
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	// Should NOT trigger a state update.
	gotState, err := tcc.stateCh.Receive(ctx)
	if err == nil {
		t.Fatalf("ClientConn.UpdateState received %v, want timeout error", gotState)
	}

	// Send a new LDS update, with the same RDS name, but different fields.
	xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: routeStr, MaxStreamDuration: time.Second, HTTPFilters: routerFilterList}, nil)
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	gotState, err = tcc.stateCh.Receive(ctx)
	if err == nil {
		t.Fatalf("ClientConn.UpdateState received %v, want timeout error", gotState)
	}
}

type filterBuilder struct {
	httpfilter.Filter // embedded as we do not need to implement registry / parsing in this test.
	path              *[]string
}

var _ httpfilter.ClientInterceptorBuilder = &filterBuilder{}

func (fb *filterBuilder) BuildClientInterceptor(config, override httpfilter.FilterConfig) (iresolver.ClientInterceptor, error) {
	if config == nil {
		panic("unexpected missing config")
	}
	*fb.path = append(*fb.path, "build:"+config.(filterCfg).s)
	err := config.(filterCfg).newStreamErr
	if override != nil {
		*fb.path = append(*fb.path, "override:"+override.(filterCfg).s)
		err = override.(filterCfg).newStreamErr
	}

	return &filterInterceptor{path: fb.path, s: config.(filterCfg).s, err: err}, nil
}

type filterInterceptor struct {
	path *[]string
	s    string
	err  error
}

func (fi *filterInterceptor) NewStream(ctx context.Context, ri iresolver.RPCInfo, done func(), newStream func(ctx context.Context, done func()) (iresolver.ClientStream, error)) (iresolver.ClientStream, error) {
	*fi.path = append(*fi.path, "newstream:"+fi.s)
	if fi.err != nil {
		return nil, fi.err
	}
	d := func() {
		*fi.path = append(*fi.path, "done:"+fi.s)
		done()
	}
	cs, err := newStream(ctx, d)
	if err != nil {
		return nil, err
	}
	return &clientStream{ClientStream: cs, path: fi.path, s: fi.s}, nil
}

type clientStream struct {
	iresolver.ClientStream
	path *[]string
	s    string
}

type filterCfg struct {
	httpfilter.FilterConfig
	s            string
	newStreamErr error
}

func (s) TestXDSResolverHTTPFilters(t *testing.T) {
	var path []string
	testCases := []struct {
		name         string
		ldsFilters   []xdsclient.HTTPFilter
		vhOverrides  map[string]httpfilter.FilterConfig
		rtOverrides  map[string]httpfilter.FilterConfig
		clOverrides  map[string]httpfilter.FilterConfig
		rpcRes       map[string][][]string
		selectErr    string
		newStreamErr string
	}{
		{
			name: "no router filter",
			ldsFilters: []xdsclient.HTTPFilter{
				{Name: "foo", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo1"}},
			},
			rpcRes: map[string][][]string{
				"1": {
					{"build:foo1", "override:foo2", "build:bar1", "override:bar2", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
				},
			},
			selectErr: "no router filter present",
		},
		{
			name: "ignored after router filter",
			ldsFilters: []xdsclient.HTTPFilter{
				{Name: "foo", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo1"}},
				routerFilter,
				{Name: "foo2", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo2"}},
			},
			rpcRes: map[string][][]string{
				"1": {
					{"build:foo1", "newstream:foo1", "done:foo1"},
				},
				"2": {
					{"build:foo1", "newstream:foo1", "done:foo1"},
					{"build:foo1", "newstream:foo1", "done:foo1"},
					{"build:foo1", "newstream:foo1", "done:foo1"},
				},
			},
		},
		{
			name: "NewStream error; ensure earlier interceptor Done is still called",
			ldsFilters: []xdsclient.HTTPFilter{
				{Name: "foo", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo1"}},
				{Name: "bar", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "bar1", newStreamErr: errors.New("bar newstream err")}},
				routerFilter,
			},
			rpcRes: map[string][][]string{
				"1": {
					{"build:foo1", "build:bar1", "newstream:foo1", "newstream:bar1" /* <err in bar1 NewStream> */, "done:foo1"},
				},
				"2": {
					{"build:foo1", "build:bar1", "newstream:foo1", "newstream:bar1" /* <err in bar1 NewSteam> */, "done:foo1"},
				},
			},
			newStreamErr: "bar newstream err",
		},
		{
			name: "all overrides",
			ldsFilters: []xdsclient.HTTPFilter{
				{Name: "foo", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo1", newStreamErr: errors.New("this is overridden to nil")}},
				{Name: "bar", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "bar1"}},
				routerFilter,
			},
			vhOverrides: map[string]httpfilter.FilterConfig{"foo": filterCfg{s: "foo2"}, "bar": filterCfg{s: "bar2"}},
			rtOverrides: map[string]httpfilter.FilterConfig{"foo": filterCfg{s: "foo3"}, "bar": filterCfg{s: "bar3"}},
			clOverrides: map[string]httpfilter.FilterConfig{"foo": filterCfg{s: "foo4"}, "bar": filterCfg{s: "bar4"}},
			rpcRes: map[string][][]string{
				"1": {
					{"build:foo1", "override:foo2", "build:bar1", "override:bar2", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
					{"build:foo1", "override:foo2", "build:bar1", "override:bar2", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
				},
				"2": {
					{"build:foo1", "override:foo3", "build:bar1", "override:bar3", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
					{"build:foo1", "override:foo4", "build:bar1", "override:bar4", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
					{"build:foo1", "override:foo3", "build:bar1", "override:bar3", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
					{"build:foo1", "override:foo4", "build:bar1", "override:bar4", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
				},
			},
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			xdsC := fakeclient.NewClient()
			xdsR, tcc, cancel := testSetup(t, setupOpts{
				xdsClientFunc: func() (xdsclient.XDSClient, error) { return xdsC, nil },
			})
			defer xdsR.Close()
			defer cancel()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			waitForWatchListener(ctx, t, xdsC, targetStr)

			xdsC.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{
				RouteConfigName: routeStr,
				HTTPFilters:     tc.ldsFilters,
			}, nil)
			if i == 0 {
				waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
			}

			defer func(oldNewWRR func() wrr.WRR) { newWRR = oldNewWRR }(newWRR)
			newWRR = xdstestutils.NewTestWRR

			// Invoke the watchAPI callback with a good service update and wait for the
			// UpdateState method to be called on the ClientConn.
			xdsC.InvokeWatchRouteConfigCallback("", xdsclient.RouteConfigUpdate{
				VirtualHosts: []*xdsclient.VirtualHost{
					{
						Domains: []string{targetStr},
						Routes: []*xdsclient.Route{{
							Prefix: newStringP("1"), WeightedClusters: map[string]xdsclient.WeightedCluster{
								"A": {Weight: 1},
								"B": {Weight: 1},
							},
						}, {
							Prefix: newStringP("2"), WeightedClusters: map[string]xdsclient.WeightedCluster{
								"A": {Weight: 1},
								"B": {Weight: 1, HTTPFilterConfigOverride: tc.clOverrides},
							},
							HTTPFilterConfigOverride: tc.rtOverrides,
						}},
						HTTPFilterConfigOverride: tc.vhOverrides,
					},
				},
			}, nil)

			gotState, err := tcc.stateCh.Receive(ctx)
			if err != nil {
				t.Fatalf("Error waiting for UpdateState to be called: %v", err)
			}
			rState := gotState.(resolver.State)
			if err := rState.ServiceConfig.Err; err != nil {
				t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
			}

			cs := iresolver.GetConfigSelector(rState)
			if cs == nil {
				t.Fatal("received nil config selector")
			}

			for method, wants := range tc.rpcRes {
				// Order of wants is non-deterministic.
				remainingWant := make([][]string, len(wants))
				copy(remainingWant, wants)
				for n := range wants {
					path = nil

					res, err := cs.SelectConfig(iresolver.RPCInfo{Method: method, Context: context.Background()})
					if tc.selectErr != "" {
						if err == nil || !strings.Contains(err.Error(), tc.selectErr) {
							t.Errorf("SelectConfig(_) = _, %v; want _, Contains(%v)", err, tc.selectErr)
						}
						if err == nil {
							res.OnCommitted()
						}
						continue
					}
					if err != nil {
						t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
					}
					var doneFunc func()
					_, err = res.Interceptor.NewStream(context.Background(), iresolver.RPCInfo{}, func() {}, func(ctx context.Context, done func()) (iresolver.ClientStream, error) {
						doneFunc = done
						return nil, nil
					})
					if tc.newStreamErr != "" {
						if err == nil || !strings.Contains(err.Error(), tc.newStreamErr) {
							t.Errorf("NewStream(...) = _, %v; want _, Contains(%v)", err, tc.newStreamErr)
						}
						if err == nil {
							res.OnCommitted()
							doneFunc()
						}
						continue
					}
					if err != nil {
						t.Fatalf("unexpected error from Interceptor.NewStream: %v", err)

					}
					res.OnCommitted()
					doneFunc()

					// Confirm the desired path is found in remainingWant, and remove it.
					pass := false
					for i := range remainingWant {
						if reflect.DeepEqual(path, remainingWant[i]) {
							remainingWant[i] = remainingWant[len(remainingWant)-1]
							remainingWant = remainingWant[:len(remainingWant)-1]
							pass = true
							break
						}
					}
					if !pass {
						t.Errorf("%q:%v - path:\n%v\nwant one of:\n%v", method, n, path, remainingWant)
					}
				}
			}
		})
	}
}

func replaceRandNumGenerator(start int64) func() {
	nextInt := start
	xdsclient.RandInt63n = func(int64) (ret int64) {
		ret = nextInt
		nextInt++
		return
	}
	return func() {
		xdsclient.RandInt63n = grpcrand.Int63n
	}
}

func newDurationP(d time.Duration) *time.Duration {
	return &d
}
