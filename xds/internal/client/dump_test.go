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

package client

import (
	"fmt"
	"testing"
	"time"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
)

func (s) TestLDSConfigDump(t *testing.T) {
	const testVersion = "test-version-lds"
	var (
		ldsTargets       = []string{"lds.target.good:0000", "lds.target.good:1111"}
		routeConfigNames = []string{"route-config-0", "route-config-1"}
		listenerRaws     = make(map[string]*anypb.Any, len(ldsTargets))
	)

	for i := range ldsTargets {
		listenersT := &v3listenerpb.Listener{
			Name: ldsTargets[i],
			ApiListener: &v3listenerpb.ApiListener{
				ApiListener: func() *anypb.Any {
					mcm, _ := ptypes.MarshalAny(&v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
							Rds: &v3httppb.Rds{
								ConfigSource: &v3corepb.ConfigSource{
									ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
								},
								RouteConfigName: routeConfigNames[i],
							},
						},
						CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
							MaxStreamDuration: durationpb.New(time.Second),
						},
					})
					return mcm
				}(),
			},
		}
		anyT, err := ptypes.MarshalAny(listenersT)
		if err != nil {
			t.Fatalf("failed to marshal proto to any: %v", err)
		}
		listenerRaws[ldsTargets[i]] = anyT
	}

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Expected unknown.
	compareDump(t, client.DumpLDS, "", map[string]LDSUpdateWithMD{})

	wantRequested := make(map[string]LDSUpdateWithMD)
	for _, n := range ldsTargets {
		cancel := client.WatchListener(n, func(update ListenerUpdate, err error) {})
		defer cancel()
		wantRequested[n] = LDSUpdateWithMD{MD: UpdateMetadata{Status: ServiceStatusRequested}}
	}
	// Expected requested.
	compareDump(t, client.DumpLDS, "", wantRequested)

	update0 := make(map[string]ListenerUpdate)
	want0 := make(map[string]LDSUpdateWithMD)
	for n, r := range listenerRaws {
		update0[n] = ListenerUpdate{Raw: r}
		want0[n] = LDSUpdateWithMD{
			Update: ListenerUpdate{Raw: r},
			MD:     UpdateMetadata{Version: testVersion},
		}
	}
	client.NewListeners(update0, UpdateMetadata{Version: testVersion})

	// Expect ACK.
	compareDump(t, client.DumpLDS, testVersion, want0)

	const nackVersion = "lds-version-nack"
	var nackErr = fmt.Errorf("lds nack error")
	client.NewListeners(
		map[string]ListenerUpdate{
			ldsTargets[0]: {},
		},
		UpdateMetadata{
			ErrState: &UpdateErrorMetadata{
				Version: nackVersion,
				Err:     nackErr,
			},
		},
	)

	// Expect NACK for [0], but old ACK for [1].
	wantDump := make(map[string]LDSUpdateWithMD)
	// Though resource 0 was NACKed, the dump should show the previous ACKed raw
	// message, as well as the NACK error.
	wantDump[ldsTargets[0]] = LDSUpdateWithMD{
		Update: ListenerUpdate{Raw: listenerRaws[ldsTargets[0]]},
		MD: UpdateMetadata{
			Version: testVersion,
			ErrState: &UpdateErrorMetadata{
				Version: nackVersion,
				Err:     nackErr,
			},
		},
	}

	wantDump[ldsTargets[1]] = LDSUpdateWithMD{
		Update: ListenerUpdate{Raw: listenerRaws[ldsTargets[1]]},
		MD:     UpdateMetadata{Version: testVersion},
	}
	compareDump(t, client.DumpLDS, nackVersion, wantDump)
}

func (s) TestRDSConfigDump(t *testing.T) {
	const testVersion = "test-version-rds"
	var (
		listenerNames = []string{"lds.target.good:0000", "lds.target.good:1111"}
		rdsTargets    = []string{"route-config-0", "route-config-1"}
		clusterNames  = []string{"cluster-0", "cluster-1"}
		routeRaws     = make(map[string]*anypb.Any, len(rdsTargets))
	)

	for i := range rdsTargets {
		routeConfigT := &v3routepb.RouteConfiguration{
			Name: rdsTargets[i],
			VirtualHosts: []*v3routepb.VirtualHost{
				{
					Domains: []string{listenerNames[i]},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterNames[i]},
							},
						},
					}},
				},
			},
		}

		anyT, err := ptypes.MarshalAny(routeConfigT)
		if err != nil {
			t.Fatalf("failed to marshal proto to any: %v", err)
		}
		routeRaws[rdsTargets[i]] = anyT
	}

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Expected unknown.
	compareDump(t, client.DumpRDS, "", map[string]RDSUpdateWithMD{})

	wantRequested := make(map[string]RDSUpdateWithMD)
	for _, n := range rdsTargets {
		cancel := client.WatchRouteConfig(n, func(update RouteConfigUpdate, err error) {})
		defer cancel()
		wantRequested[n] = RDSUpdateWithMD{MD: UpdateMetadata{Status: ServiceStatusRequested}}
	}
	// Expected requested.
	compareDump(t, client.DumpRDS, "", wantRequested)

	update0 := make(map[string]RouteConfigUpdate)
	want0 := make(map[string]RDSUpdateWithMD)
	for n, r := range routeRaws {
		update0[n] = RouteConfigUpdate{Raw: r}
		want0[n] = RDSUpdateWithMD{
			Update: RouteConfigUpdate{Raw: r},
			MD:     UpdateMetadata{Version: testVersion},
		}
	}
	client.NewRouteConfigs(update0, UpdateMetadata{Version: testVersion})

	// Expect ACK.
	compareDump(t, client.DumpRDS, testVersion, want0)

	const nackVersion = "rds-version-nack"
	var nackErr = fmt.Errorf("rds nack error")
	client.NewRouteConfigs(
		map[string]RouteConfigUpdate{
			rdsTargets[0]: {},
		},
		UpdateMetadata{
			ErrState: &UpdateErrorMetadata{
				Version: nackVersion,
				Err:     nackErr,
			},
		},
	)

	// Expect NACK for [0], but old ACK for [1].
	wantDump := make(map[string]RDSUpdateWithMD)
	// Though resource 0 was NACKed, the dump should show the previous ACKed raw
	// message, as well as the NACK error.
	wantDump[rdsTargets[0]] = RDSUpdateWithMD{
		Update: RouteConfigUpdate{Raw: routeRaws[rdsTargets[0]]},
		MD: UpdateMetadata{
			Version: testVersion,
			ErrState: &UpdateErrorMetadata{
				Version: nackVersion,
				Err:     nackErr,
			},
		},
	}
	wantDump[rdsTargets[1]] = RDSUpdateWithMD{
		Update: RouteConfigUpdate{Raw: routeRaws[rdsTargets[1]]},
		MD:     UpdateMetadata{Version: testVersion},
	}
	compareDump(t, client.DumpRDS, nackVersion, wantDump)
}

func (s) TestCDSConfigDump(t *testing.T) {
	const testVersion = "test-version-cds"
	var (
		cdsTargets   = []string{"cluster-0", "cluster-1"}
		serviceNames = []string{"service-0", "service-1"}
		clusterRaws  = make(map[string]*anypb.Any, len(cdsTargets))
	)

	for i := range cdsTargets {
		clusterT := &v3clusterpb.Cluster{
			Name:                 cdsTargets[i],
			ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
			EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
				EdsConfig: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
						Ads: &v3corepb.AggregatedConfigSource{},
					},
				},
				ServiceName: serviceNames[i],
			},
			LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			LrsServer: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
					Self: &v3corepb.SelfConfigSource{},
				},
			},
		}

		anyT, err := ptypes.MarshalAny(clusterT)
		if err != nil {
			t.Fatalf("failed to marshal proto to any: %v", err)
		}
		clusterRaws[cdsTargets[i]] = anyT
	}

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Expected unknown.
	compareDump(t, client.DumpCDS, "", map[string]CDSUpdateWithMD{})

	wantRequested := make(map[string]CDSUpdateWithMD)
	for _, n := range cdsTargets {
		cancel := client.WatchCluster(n, func(update ClusterUpdate, err error) {})
		defer cancel()
		wantRequested[n] = CDSUpdateWithMD{MD: UpdateMetadata{Status: ServiceStatusRequested}}
	}
	// Expected requested.
	compareDump(t, client.DumpCDS, "", wantRequested)

	update0 := make(map[string]ClusterUpdate)
	want0 := make(map[string]CDSUpdateWithMD)
	for n, r := range clusterRaws {
		update0[n] = ClusterUpdate{Raw: r}
		want0[n] = CDSUpdateWithMD{
			Update: ClusterUpdate{Raw: r},
			MD:     UpdateMetadata{Version: testVersion},
		}
	}
	client.NewClusters(update0, UpdateMetadata{Version: testVersion})

	// Expect ACK.
	compareDump(t, client.DumpCDS, testVersion, want0)

	const nackVersion = "cds-version-nack"
	var nackErr = fmt.Errorf("cds nack error")
	client.NewClusters(
		map[string]ClusterUpdate{
			cdsTargets[0]: {},
		},
		UpdateMetadata{
			ErrState: &UpdateErrorMetadata{
				Version: nackVersion,
				Err:     nackErr,
			},
		},
	)

	// Expect NACK for [0], but old ACK for [1].
	wantDump := make(map[string]CDSUpdateWithMD)
	// Though resource 0 was NACKed, the dump should show the previous ACKed raw
	// message, as well as the NACK error.
	wantDump[cdsTargets[0]] = CDSUpdateWithMD{
		Update: ClusterUpdate{Raw: clusterRaws[cdsTargets[0]]},
		MD: UpdateMetadata{
			Version: testVersion,
			ErrState: &UpdateErrorMetadata{
				Version: nackVersion,
				Err:     nackErr,
			},
		},
	}
	wantDump[cdsTargets[1]] = CDSUpdateWithMD{
		Update: ClusterUpdate{Raw: clusterRaws[cdsTargets[1]]},
		MD:     UpdateMetadata{Version: testVersion},
	}
	compareDump(t, client.DumpCDS, nackVersion, wantDump)
}

func (s) TestEDSConfigDump(t *testing.T) {
	const testVersion = "test-version-cds"
	var (
		edsTargets    = []string{"cluster-0", "cluster-1"}
		localityNames = []string{"locality-0", "locality-1"}
		addrs         = []string{"addr0:123", "addr1:456"}
		endpointRaws  = make(map[string]*anypb.Any, len(edsTargets))
	)

	for i := range edsTargets {
		clab0 := newClaBuilder(edsTargets[i], nil)
		clab0.addLocality(localityNames[i], 1, 1, []string{addrs[i]}, nil)
		claT := clab0.Build()

		anyT, err := ptypes.MarshalAny(claT)
		if err != nil {
			t.Fatalf("failed to marshal proto to any: %v", err)
		}
		endpointRaws[edsTargets[i]] = anyT
	}

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Expected unknown.
	compareDump(t, client.DumpEDS, "", map[string]EDSUpdateWithMD{})

	wantRequested := make(map[string]EDSUpdateWithMD)
	for _, n := range edsTargets {
		cancel := client.WatchEndpoints(n, func(update EndpointsUpdate, err error) {})
		defer cancel()
		wantRequested[n] = EDSUpdateWithMD{MD: UpdateMetadata{Status: ServiceStatusRequested}}
	}
	// Expected requested.
	compareDump(t, client.DumpEDS, "", wantRequested)

	update0 := make(map[string]EndpointsUpdate)
	want0 := make(map[string]EDSUpdateWithMD)
	for n, r := range endpointRaws {
		update0[n] = EndpointsUpdate{Raw: r}
		want0[n] = EDSUpdateWithMD{
			Update: EndpointsUpdate{Raw: r},
			MD:     UpdateMetadata{Version: testVersion},
		}
	}
	client.NewEndpoints(update0, UpdateMetadata{Version: testVersion})

	// Expect ACK.
	compareDump(t, client.DumpEDS, testVersion, want0)

	const nackVersion = "eds-version-nack"
	var nackErr = fmt.Errorf("eds nack error")
	client.NewEndpoints(
		map[string]EndpointsUpdate{
			edsTargets[0]: {},
		},
		UpdateMetadata{
			ErrState: &UpdateErrorMetadata{
				Version: nackVersion,
				Err:     nackErr,
			},
		},
	)

	// Expect NACK for [0], but old ACK for [1].
	wantDump := make(map[string]EDSUpdateWithMD)
	// Though resource 0 was NACKed, the dump should show the previous ACKed raw
	// message, as well as the NACK error.
	wantDump[edsTargets[0]] = EDSUpdateWithMD{
		Update: EndpointsUpdate{Raw: endpointRaws[edsTargets[0]]},
		MD: UpdateMetadata{
			Version: testVersion,
			ErrState: &UpdateErrorMetadata{
				Version: nackVersion,
				Err:     nackErr,
			},
		},
	}
	wantDump[edsTargets[1]] = EDSUpdateWithMD{
		Update: EndpointsUpdate{Raw: endpointRaws[edsTargets[1]]},
		MD:     UpdateMetadata{Version: testVersion},
	}
	compareDump(t, client.DumpEDS, nackVersion, wantDump)
}

func compareDump(t *testing.T, dumpFunc interface{}, wantVersion string, wantDump interface{}) {
	t.Helper()

	var (
		v    string
		dump interface{}
	)
	switch dumpFuncT := dumpFunc.(type) {
	case func() (string, map[string]LDSUpdateWithMD):
		v, dump = dumpFuncT()
	case func() (string, map[string]RDSUpdateWithMD):
		v, dump = dumpFuncT()
	case func() (string, map[string]CDSUpdateWithMD):
		v, dump = dumpFuncT()
	case func() (string, map[string]EDSUpdateWithMD):
		v, dump = dumpFuncT()
	}
	if v != wantVersion {
		t.Fatalf("Dump returned version %q, want %q", v, wantVersion)
	}
	if diff := cmp.Diff(dump, wantDump, cmpOpts); diff != "" {
		t.Fatalf("Dump returned unexpected dump, diff (-got +want): %s", diff)
	}
}
