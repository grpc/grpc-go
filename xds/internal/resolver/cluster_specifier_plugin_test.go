/*
 *
 * Copyright 2021 gRPC authors.
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
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/clustermanager"
	"google.golang.org/grpc/xds/internal/clusterspecifier"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

func init() {
	balancer.Register(cspB{})
}

type cspB struct{}

func (cspB) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return nil
}

func (cspB) Name() string {
	return "csp_experimental"
}

type cspConfig struct {
	ArbitraryField string `json:"arbitrary_field"`
}

// TestXDSResolverClusterSpecifierPlugin tests that cluster specifier plugins
// produce the correct service config, and that the config selector routes to a
// cluster specifier plugin supported by this service config (i.e. prefixed with
// a cluster specifier plugin prefix).
func (s) TestXDSResolverClusterSpecifierPlugin(t *testing.T) {
	xdsR, xdsC, tcc, cancel := testSetup(t, setupOpts{target: target})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsresource.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)

	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
	xdsC.InvokeWatchRouteConfigCallback("", xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), ClusterSpecifierPlugin: "cspA"}},
			},
		},
		// Top level csp config here - the value of cspA should get directly
		// placed as a child policy of xds cluster manager.
		ClusterSpecifierPlugins: map[string]clusterspecifier.BalancerConfig{"cspA": []map[string]interface{}{{"csp_experimental": cspConfig{ArbitraryField: "anything"}}}},
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
        "cluster_specifier_plugin:cspA":{
          "childPolicy":[{"csp_experimental":{"arbitrary_field":"anything"}}]
        }
      }
    }}]}`

	wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantJSON)
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
	clusterWant := clusterSpecifierPluginPrefix + "cspA"
	if cluster != clusterWant {
		t.Fatalf("cluster: %+v, want: %+v", cluster, clusterWant)
	}
}

// TestXDSResolverClusterSpecifierPluginConfigUpdate tests that cluster
// specifier plugins produce the correct service config, and that on an update
// to the CSP Configuration, the new config is accounted for in the output
// service config.
func (s) TestXDSResolverClusterSpecifierPluginConfigUpdate(t *testing.T) {
	xdsR, xdsC, tcc, cancel := testSetup(t, setupOpts{target: target})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsresource.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)

	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
	xdsC.InvokeWatchRouteConfigCallback("", xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), ClusterSpecifierPlugin: "cspA"}},
			},
		},
		// Top level csp config here - the value of cspA should get directly
		// placed as a child policy of xds cluster manager.
		ClusterSpecifierPlugins: map[string]clusterspecifier.BalancerConfig{"cspA": []map[string]interface{}{{"csp_experimental": cspConfig{ArbitraryField: "anything"}}}},
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
        "cluster_specifier_plugin:cspA":{
          "childPolicy":[{"csp_experimental":{"arbitrary_field":"anything"}}]
        }
      }
    }}]}`

	wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantJSON)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Fatal("want: ", cmp.Diff(nil, wantSCParsed.Config))
	}

	xdsC.InvokeWatchRouteConfigCallback("", xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), ClusterSpecifierPlugin: "cspA"}},
			},
		},
		// Top level csp config here - the value of cspA should get directly
		// placed as a child policy of xds cluster manager.
		ClusterSpecifierPlugins: map[string]clusterspecifier.BalancerConfig{"cspA": []map[string]interface{}{{"csp_experimental": cspConfig{ArbitraryField: "changed"}}}},
	}, nil)

	gotState, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error waiting for UpdateState to be called: %v", err)
	}
	rState = gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantJSON = `{"loadBalancingConfig":[{
    "xds_cluster_manager_experimental":{
      "children":{
        "cluster_specifier_plugin:cspA":{
          "childPolicy":[{"csp_experimental":{"arbitrary_field":"changed"}}]
        }
      }
    }}]}`

	wantSCParsed = internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantJSON)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Fatal("want: ", cmp.Diff(nil, wantSCParsed.Config))
	}
}

// TestXDSResolverDelayedOnCommittedCSP tests that cluster specifier plugins and
// their corresponding configurations remain in service config if RPCs are in
// flight.
func (s) TestXDSResolverDelayedOnCommittedCSP(t *testing.T) {
	xdsR, xdsC, tcc, cancel := testSetup(t, setupOpts{target: target})
	defer xdsR.Close()
	defer cancel()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForWatchListener(ctx, t, xdsC, targetStr)
	xdsC.InvokeWatchListenerCallback(xdsresource.ListenerUpdate{RouteConfigName: routeStr, HTTPFilters: routerFilterList}, nil)
	waitForWatchRouteConfig(ctx, t, xdsC, routeStr)

	xdsC.InvokeWatchRouteConfigCallback("", xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), ClusterSpecifierPlugin: "cspA"}},
			},
		},
		// Top level csp config here - the value of cspA should get directly
		// placed as a child policy of xds cluster manager.
		ClusterSpecifierPlugins: map[string]clusterspecifier.BalancerConfig{"cspA": []map[string]interface{}{{"csp_experimental": cspConfig{ArbitraryField: "anythingA"}}}},
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
        "cluster_specifier_plugin:cspA":{
          "childPolicy":[{"csp_experimental":{"arbitrary_field":"anythingA"}}]
        }
      }
    }}]}`

	wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantJSON)
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
	clusterWant := clusterSpecifierPluginPrefix + "cspA"
	if cluster != clusterWant {
		t.Fatalf("cluster: %+v, want: %+v", cluster, clusterWant)
	}
	// delay res.OnCommitted()

	// Perform TWO updates to ensure the old config selector does not hold a reference to cspA
	xdsC.InvokeWatchRouteConfigCallback("", xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), ClusterSpecifierPlugin: "cspB"}},
			},
		},
		// Top level csp config here - the value of cspB should get directly
		// placed as a child policy of xds cluster manager.
		ClusterSpecifierPlugins: map[string]clusterspecifier.BalancerConfig{"cspB": []map[string]interface{}{{"csp_experimental": cspConfig{ArbitraryField: "anythingB"}}}},
	}, nil)
	tcc.stateCh.Receive(ctx) // Ignore the first update.

	xdsC.InvokeWatchRouteConfigCallback("", xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), ClusterSpecifierPlugin: "cspB"}},
			},
		},
		// Top level csp config here - the value of cspB should get directly
		// placed as a child policy of xds cluster manager.
		ClusterSpecifierPlugins: map[string]clusterspecifier.BalancerConfig{"cspB": []map[string]interface{}{{"csp_experimental": cspConfig{ArbitraryField: "anythingB"}}}},
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
        "cluster_specifier_plugin:cspA":{
          "childPolicy":[{"csp_experimental":{"arbitrary_field":"anythingA"}}]
        },
        "cluster_specifier_plugin:cspB":{
          "childPolicy":[{"csp_experimental":{"arbitrary_field":"anythingB"}}]
        }
      }
    }}]}`

	wantSCParsed2 := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantJSON2)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed2.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Fatal("want: ", cmp.Diff(nil, wantSCParsed2.Config))
	}

	// Invoke OnCommitted; should lead to a service config update that deletes
	// cspA.
	res.OnCommitted()

	xdsC.InvokeWatchRouteConfigCallback("", xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{targetStr},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), ClusterSpecifierPlugin: "cspB"}},
			},
		},
		// Top level csp config here - the value of cspB should get directly
		// placed as a child policy of xds cluster manager.
		ClusterSpecifierPlugins: map[string]clusterspecifier.BalancerConfig{"cspB": []map[string]interface{}{{"csp_experimental": cspConfig{ArbitraryField: "anythingB"}}}},
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
        "cluster_specifier_plugin:cspB":{
          "childPolicy":[{"csp_experimental":{"arbitrary_field":"anythingB"}}]
        }
      }
    }}]}`

	wantSCParsed3 := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantJSON3)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed3.Config) {
		t.Errorf("ClientConn.UpdateState received different service config")
		t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
		t.Fatal("want: ", cmp.Diff(nil, wantSCParsed3.Config))
	}
}
