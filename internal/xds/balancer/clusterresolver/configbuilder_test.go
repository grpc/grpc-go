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

package clusterresolver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/ringhash"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/internal/balancer/weight"
	"google.golang.org/grpc/internal/hierarchy"
	iringhash "google.golang.org/grpc/internal/ringhash"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	xdsinternal "google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/internal/xds/balancer/clusterimpl"
	"google.golang.org/grpc/internal/xds/balancer/outlierdetection"
	"google.golang.org/grpc/internal/xds/balancer/priority"
	"google.golang.org/grpc/internal/xds/balancer/wrrlocality"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/resolver"
)

const (
	testLRSServer       = "test-lrs-server"
	testMaxRequests     = 314
	testEDSServiceName  = "service-name-from-parent"
	testDropCategory    = "test-drops"
	testDropOverMillion = 1

	localityCount       = 5
	endpointPerLocality = 2
)

var (
	testLocalityIDs       []clients.Locality
	testResolverEndpoints [][]resolver.Endpoint
	testEndpoints         [][]xdsresource.Endpoint

	testLocalitiesP0, testLocalitiesP1 []xdsresource.Locality

	endpointCmpOpts = cmp.Options{
		cmp.AllowUnexported(attributes.Attributes{}),
		cmp.Transformer("SortEndpoints", func(in []resolver.Endpoint) []resolver.Endpoint {
			out := append([]resolver.Endpoint(nil), in...) // Copy input to avoid mutating it
			sort.Slice(out, func(i, j int) bool {
				return out[i].Addresses[0].Addr < out[j].Addresses[0].Addr
			})
			return out
		}),
	}

	noopODCfg = outlierdetection.LBConfig{
		Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
		BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
		MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
		MaxEjectionPercent: 10,
	}
)

func init() {
	for i := 0; i < localityCount; i++ {
		testLocalityIDs = append(testLocalityIDs, clients.Locality{Zone: fmt.Sprintf("test-zone-%d", i)})
		var (
			endpoints []resolver.Endpoint
			ends      []xdsresource.Endpoint
		)
		for j := 0; j < endpointPerLocality; j++ {
			addr := fmt.Sprintf("addr-%d-%d", i, j)
			endpoints = append(endpoints, resolver.Endpoint{Addresses: []resolver.Address{{Addr: addr}}})
			ends = append(ends, xdsresource.Endpoint{
				HealthStatus: xdsresource.EndpointHealthStatusHealthy,
				Addresses: []string{
					addr,
					fmt.Sprintf("addr-%d-%d-additional-1", i, j),
					fmt.Sprintf("addr-%d-%d-additional-2", i, j),
				},
			})
		}
		testResolverEndpoints = append(testResolverEndpoints, endpoints)
		testEndpoints = append(testEndpoints, ends)
	}

	testLocalitiesP0 = []xdsresource.Locality{
		{
			Endpoints: testEndpoints[0],
			ID:        testLocalityIDs[0],
			Weight:    20,
			Priority:  0,
		},
		{
			Endpoints: testEndpoints[1],
			ID:        testLocalityIDs[1],
			Weight:    80,
			Priority:  0,
		},
	}
	testLocalitiesP1 = []xdsresource.Locality{
		{
			Endpoints: testEndpoints[2],
			ID:        testLocalityIDs[2],
			Weight:    20,
			Priority:  1,
		},
		{
			Endpoints: testEndpoints[3],
			ID:        testLocalityIDs[3],
			Weight:    80,
			Priority:  1,
		},
	}
}

// TestBuildPriorityConfigJSON is a sanity check that the built balancer config
// can be parsed. The behavior test is covered by TestBuildPriorityConfig.
func TestBuildPriorityConfigJSON(t *testing.T) {
	testLRSServerConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}

	gotConfig, _, err := buildPriorityConfigJSON([]priorityConfig{
		{
			mechanism: DiscoveryMechanism{
				Cluster:               testClusterName,
				LoadReportingServer:   testLRSServerConfig,
				MaxConcurrentRequests: newUint32(testMaxRequests),
				Type:                  DiscoveryMechanismTypeEDS,
				EDSServiceName:        testEDSServiceName,
			},
			edsResp: xdsresource.EndpointsUpdate{
				Drops: []xdsresource.OverloadDropConfig{
					{
						Category:    testDropCategory,
						Numerator:   testDropOverMillion,
						Denominator: million,
					},
				},
				Localities: []xdsresource.Locality{
					testLocalitiesP0[0],
					testLocalitiesP0[1],
					testLocalitiesP1[0],
					testLocalitiesP1[1],
				},
			},
			childNameGen: newNameGenerator(0),
		},
		{
			mechanism: DiscoveryMechanism{
				Type: DiscoveryMechanismTypeLogicalDNS,
			},
			endpoints:    testResolverEndpoints[4],
			childNameGen: newNameGenerator(1),
		},
	}, nil)
	if err != nil {
		t.Fatalf("buildPriorityConfigJSON(...) failed: %v", err)
	}

	var prettyGot bytes.Buffer
	if err := json.Indent(&prettyGot, gotConfig, ">>> ", "  "); err != nil {
		t.Fatalf("json.Indent() failed: %v", err)
	}
	// Print the indented json if this test fails.
	t.Log(prettyGot.String())

	priorityB := balancer.Get(priority.Name)
	if _, err = priorityB.(balancer.ConfigParser).ParseConfig(gotConfig); err != nil {
		t.Fatalf("ParseConfig(%+v) failed: %v", gotConfig, err)
	}
}

// TestBuildPriorityConfig tests the priority config generation. Each top level
// balancer per priority should be an Outlier Detection balancer, with a Cluster
// Impl Balancer as a child.
func TestBuildPriorityConfig(t *testing.T) {
	gotConfig, _, _ := buildPriorityConfig([]priorityConfig{
		{
			// EDS - OD config should be the top level for both of the EDS
			// priorities balancer This EDS priority will have multiple sub
			// priorities. The Outlier Detection configuration specified in the
			// Discovery Mechanism should be the top level for each sub
			// priorities balancer.
			mechanism: DiscoveryMechanism{
				Cluster:          testClusterName,
				Type:             DiscoveryMechanismTypeEDS,
				EDSServiceName:   testEDSServiceName,
				outlierDetection: noopODCfg,
			},
			edsResp: xdsresource.EndpointsUpdate{
				Localities: []xdsresource.Locality{
					testLocalitiesP0[0],
					testLocalitiesP0[1],
					testLocalitiesP1[0],
					testLocalitiesP1[1],
				},
			},
			childNameGen: newNameGenerator(0),
		},
		{
			// This OD config should wrap the Logical DNS priorities balancer.
			mechanism: DiscoveryMechanism{
				Cluster:          testClusterName2,
				Type:             DiscoveryMechanismTypeLogicalDNS,
				outlierDetection: noopODCfg,
			},
			endpoints:    testResolverEndpoints[4],
			childNameGen: newNameGenerator(1),
		},
	}, nil)

	wantConfig := &priority.LBConfig{
		Children: map[string]*priority.Child{
			"priority-0-0": {
				Config: &iserviceconfig.BalancerConfig{
					Name: outlierdetection.Name,
					Config: &outlierdetection.LBConfig{
						Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
						BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
						MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
						MaxEjectionPercent: 10,
						ChildPolicy: &iserviceconfig.BalancerConfig{
							Name: clusterimpl.Name,
							Config: &clusterimpl.LBConfig{
								Cluster:        testClusterName,
								EDSServiceName: testEDSServiceName,
								DropCategories: []clusterimpl.DropConfig{},
							},
						},
					},
				},
				IgnoreReresolutionRequests: true,
			},
			"priority-0-1": {
				Config: &iserviceconfig.BalancerConfig{
					Name: outlierdetection.Name,
					Config: &outlierdetection.LBConfig{
						Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
						BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
						MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
						MaxEjectionPercent: 10,
						ChildPolicy: &iserviceconfig.BalancerConfig{
							Name: clusterimpl.Name,
							Config: &clusterimpl.LBConfig{
								Cluster:        testClusterName,
								EDSServiceName: testEDSServiceName,
								DropCategories: []clusterimpl.DropConfig{},
							},
						},
					},
				},
				IgnoreReresolutionRequests: true,
			},
			"priority-1": {
				Config: &iserviceconfig.BalancerConfig{
					Name: outlierdetection.Name,
					Config: &outlierdetection.LBConfig{
						Interval:           iserviceconfig.Duration(10 * time.Second), // default interval
						BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
						MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
						MaxEjectionPercent: 10,
						ChildPolicy: &iserviceconfig.BalancerConfig{
							Name: clusterimpl.Name,
							Config: &clusterimpl.LBConfig{
								Cluster:     testClusterName2,
								ChildPolicy: &iserviceconfig.BalancerConfig{Name: "pick_first"},
							},
						},
					},
				},
				IgnoreReresolutionRequests: false,
			},
		},
		Priorities: []string{"priority-0-0", "priority-0-1", "priority-1"},
	}
	if diff := cmp.Diff(gotConfig, wantConfig); diff != "" {
		t.Errorf("buildPriorityConfig() diff (-got +want) %v", diff)
	}
}

func TestBuildClusterImplConfigForDNS(t *testing.T) {
	gotName, gotConfig, gotEndpoints := buildClusterImplConfigForDNS(newNameGenerator(3), testResolverEndpoints[0], DiscoveryMechanism{Cluster: testClusterName2, Type: DiscoveryMechanismTypeLogicalDNS})
	wantName := "priority-3"
	wantConfig := &clusterimpl.LBConfig{
		Cluster: testClusterName2,
		ChildPolicy: &iserviceconfig.BalancerConfig{
			Name: "pick_first",
		},
	}
	e1 := resolver.Endpoint{Addresses: []resolver.Address{{Addr: testEndpoints[0][0].Addresses[0]}}}
	e2 := resolver.Endpoint{Addresses: []resolver.Address{{Addr: testEndpoints[0][1].Addresses[0]}}}
	wantEndpoints := []resolver.Endpoint{
		hierarchy.SetInEndpoint(e1, []string{"priority-3"}),
		hierarchy.SetInEndpoint(e2, []string{"priority-3"}),
	}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("buildClusterImplConfigForDNS() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotConfig, wantConfig); diff != "" {
		t.Errorf("buildClusterImplConfigForDNS() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotEndpoints, wantEndpoints, endpointCmpOpts); diff != "" {
		t.Errorf("buildClusterImplConfigForDNS() diff (-got +want) %v", diff)
	}
}

func TestBuildClusterImplConfigForEDS(t *testing.T) {
	testLRSServerConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}

	gotNames, gotConfigs, gotEndpoints, _ := buildClusterImplConfigForEDS(
		newNameGenerator(2),
		xdsresource.EndpointsUpdate{
			Drops: []xdsresource.OverloadDropConfig{
				{
					Category:    testDropCategory,
					Numerator:   testDropOverMillion,
					Denominator: million,
				},
			},
			Localities: []xdsresource.Locality{
				{
					Endpoints: testEndpoints[3],
					ID:        testLocalityIDs[3],
					Weight:    80,
					Priority:  1,
				}, {
					Endpoints: testEndpoints[1],
					ID:        testLocalityIDs[1],
					Weight:    80,
					Priority:  0,
				}, {
					Endpoints: testEndpoints[2],
					ID:        testLocalityIDs[2],
					Weight:    20,
					Priority:  1,
				}, {
					Endpoints: testEndpoints[0],
					ID:        testLocalityIDs[0],
					Weight:    20,
					Priority:  0,
				},
			},
		},
		DiscoveryMechanism{
			Cluster:               testClusterName,
			MaxConcurrentRequests: newUint32(testMaxRequests),
			LoadReportingServer:   testLRSServerConfig,
			Type:                  DiscoveryMechanismTypeEDS,
			EDSServiceName:        testEDSServiceName,
		},
		nil,
	)

	wantNames := []string{
		fmt.Sprintf("priority-%v-%v", 2, 0),
		fmt.Sprintf("priority-%v-%v", 2, 1),
	}
	wantConfigs := map[string]*clusterimpl.LBConfig{
		"priority-2-0": {
			Cluster:               testClusterName,
			EDSServiceName:        testEDSServiceName,
			LoadReportingServer:   testLRSServerConfig,
			MaxConcurrentRequests: newUint32(testMaxRequests),
			DropCategories: []clusterimpl.DropConfig{
				{
					Category:           testDropCategory,
					RequestsPerMillion: testDropOverMillion,
				},
			},
		},
		"priority-2-1": {
			Cluster:               testClusterName,
			EDSServiceName:        testEDSServiceName,
			LoadReportingServer:   testLRSServerConfig,
			MaxConcurrentRequests: newUint32(testMaxRequests),
			DropCategories: []clusterimpl.DropConfig{
				{
					Category:           testDropCategory,
					RequestsPerMillion: testDropOverMillion,
				},
			},
		},
	}
	wantEndpoints := []resolver.Endpoint{
		testEndpointWithAttrs(testEndpoints[0][0].Addresses, 20, 1, "priority-2-0", &testLocalityIDs[0]),
		testEndpointWithAttrs(testEndpoints[0][1].Addresses, 20, 1, "priority-2-0", &testLocalityIDs[0]),
		testEndpointWithAttrs(testEndpoints[1][0].Addresses, 80, 1, "priority-2-0", &testLocalityIDs[1]),
		testEndpointWithAttrs(testEndpoints[1][1].Addresses, 80, 1, "priority-2-0", &testLocalityIDs[1]),
		testEndpointWithAttrs(testEndpoints[2][0].Addresses, 20, 1, "priority-2-1", &testLocalityIDs[2]),
		testEndpointWithAttrs(testEndpoints[2][1].Addresses, 20, 1, "priority-2-1", &testLocalityIDs[2]),
		testEndpointWithAttrs(testEndpoints[3][0].Addresses, 80, 1, "priority-2-1", &testLocalityIDs[3]),
		testEndpointWithAttrs(testEndpoints[3][1].Addresses, 80, 1, "priority-2-1", &testLocalityIDs[3]),
	}

	if diff := cmp.Diff(gotNames, wantNames); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotConfigs, wantConfigs); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotEndpoints, wantEndpoints, endpointCmpOpts); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-got +want) %v", diff)
	}

}

func TestGroupLocalitiesByPriority(t *testing.T) {
	tests := []struct {
		name           string
		localities     []xdsresource.Locality
		wantLocalities [][]xdsresource.Locality
	}{
		{
			name:       "1 locality 1 priority",
			localities: []xdsresource.Locality{testLocalitiesP0[0]},
			wantLocalities: [][]xdsresource.Locality{
				{testLocalitiesP0[0]},
			},
		},
		{
			name:       "2 locality 1 priority",
			localities: []xdsresource.Locality{testLocalitiesP0[0], testLocalitiesP0[1]},
			wantLocalities: [][]xdsresource.Locality{
				{testLocalitiesP0[0], testLocalitiesP0[1]},
			},
		},
		{
			name:       "1 locality in each",
			localities: []xdsresource.Locality{testLocalitiesP0[0], testLocalitiesP1[0]},
			wantLocalities: [][]xdsresource.Locality{
				{testLocalitiesP0[0]},
				{testLocalitiesP1[0]},
			},
		},
		{
			name: "2 localities in each sorted",
			localities: []xdsresource.Locality{
				testLocalitiesP0[0], testLocalitiesP0[1],
				testLocalitiesP1[0], testLocalitiesP1[1]},
			wantLocalities: [][]xdsresource.Locality{
				{testLocalitiesP0[0], testLocalitiesP0[1]},
				{testLocalitiesP1[0], testLocalitiesP1[1]},
			},
		},
		{
			// The localities are given in order [p1, p0, p1, p0], but the
			// returned priority list must be sorted [p0, p1], because the list
			// order is the priority order.
			name: "2 localities in each needs to sort",
			localities: []xdsresource.Locality{
				testLocalitiesP1[1], testLocalitiesP0[1],
				testLocalitiesP1[0], testLocalitiesP0[0]},
			wantLocalities: [][]xdsresource.Locality{
				{testLocalitiesP0[1], testLocalitiesP0[0]},
				{testLocalitiesP1[1], testLocalitiesP1[0]},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLocalities := groupLocalitiesByPriority(tt.localities)
			if diff := cmp.Diff(gotLocalities, tt.wantLocalities); diff != "" {
				t.Errorf("groupLocalitiesByPriority() diff(-got +want) %v", diff)
			}
		})
	}
}

func TestDedupSortedIntSlice(t *testing.T) {
	tests := []struct {
		name string
		a    []int
		want []int
	}{
		{
			name: "empty",
			a:    []int{},
			want: []int{},
		},
		{
			name: "no dup",
			a:    []int{0, 1, 2, 3},
			want: []int{0, 1, 2, 3},
		},
		{
			name: "with dup",
			a:    []int{0, 0, 1, 1, 1, 2, 3},
			want: []int{0, 1, 2, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dedupSortedIntSlice(tt.a); !cmp.Equal(got, tt.want) {
				t.Errorf("dedupSortedIntSlice() = %v, want %v, diff %v", got, tt.want, cmp.Diff(got, tt.want))
			}
		})
	}
}

func TestPriorityLocalitiesToClusterImpl(t *testing.T) {
	tests := []struct {
		name          string
		localities    []xdsresource.Locality
		priorityName  string
		mechanism     DiscoveryMechanism
		childPolicy   *iserviceconfig.BalancerConfig
		wantConfig    *clusterimpl.LBConfig
		wantEndpoints []resolver.Endpoint
		wantErr       bool
	}{{
		name: "round robin as child, no LRS",
		localities: []xdsresource.Locality{
			{
				Endpoints: []xdsresource.Endpoint{
					{Addresses: []string{"addr-1-1"}, HealthStatus: xdsresource.EndpointHealthStatusHealthy, Weight: 90},
					{Addresses: []string{"addr-1-2"}, HealthStatus: xdsresource.EndpointHealthStatusHealthy, Weight: 10},
				},
				ID:     clients.Locality{Zone: "test-zone-1"},
				Weight: 20,
			},
			{
				Endpoints: []xdsresource.Endpoint{
					{Addresses: []string{"addr-2-1"}, HealthStatus: xdsresource.EndpointHealthStatusHealthy, Weight: 90},
					{Addresses: []string{"addr-2-2"}, HealthStatus: xdsresource.EndpointHealthStatusHealthy, Weight: 10},
				},
				ID:     clients.Locality{Zone: "test-zone-2"},
				Weight: 80,
			},
		},
		priorityName: "test-priority",
		childPolicy:  &iserviceconfig.BalancerConfig{Name: roundrobin.Name},
		mechanism: DiscoveryMechanism{
			Cluster:        testClusterName,
			Type:           DiscoveryMechanismTypeEDS,
			EDSServiceName: testEDSService,
		},
		// lrsServer is nil, so LRS policy will not be used.
		wantConfig: &clusterimpl.LBConfig{
			Cluster:        testClusterName,
			EDSServiceName: testEDSService,
			ChildPolicy:    &iserviceconfig.BalancerConfig{Name: roundrobin.Name},
		},
		wantEndpoints: []resolver.Endpoint{
			testEndpointWithAttrs([]string{"addr-1-1"}, 20, 90, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
			testEndpointWithAttrs([]string{"addr-1-2"}, 20, 10, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
			testEndpointWithAttrs([]string{"addr-2-1"}, 80, 90, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
			testEndpointWithAttrs([]string{"addr-2-2"}, 80, 10, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
		},
	},
		{
			name: "ring_hash as child",
			localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{
						{Addresses: []string{"addr-1-1"}, HealthStatus: xdsresource.EndpointHealthStatusHealthy, Weight: 90},
						{Addresses: []string{"addr-1-2"}, HealthStatus: xdsresource.EndpointHealthStatusHealthy, Weight: 10},
					},
					ID:     clients.Locality{Zone: "test-zone-1"},
					Weight: 20,
				},
				{
					Endpoints: []xdsresource.Endpoint{
						{Addresses: []string{"addr-2-1"}, HealthStatus: xdsresource.EndpointHealthStatusHealthy, Weight: 90},
						{Addresses: []string{"addr-2-2"}, HealthStatus: xdsresource.EndpointHealthStatusHealthy, Weight: 10},
					},
					ID:     clients.Locality{Zone: "test-zone-2"},
					Weight: 80,
				},
			},
			priorityName: "test-priority",
			childPolicy:  &iserviceconfig.BalancerConfig{Name: ringhash.Name, Config: &iringhash.LBConfig{MinRingSize: 1, MaxRingSize: 2}},
			// lrsServer is nil, so LRS policy will not be used.
			wantConfig: &clusterimpl.LBConfig{
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name:   ringhash.Name,
					Config: &iringhash.LBConfig{MinRingSize: 1, MaxRingSize: 2},
				},
			},
			wantEndpoints: []resolver.Endpoint{
				testEndpointWithAttrs([]string{"addr-1-1"}, 20, 90, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs([]string{"addr-1-2"}, 20, 10, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs([]string{"addr-2-1"}, 80, 90, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
				testEndpointWithAttrs([]string{"addr-2-2"}, 80, 10, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := priorityLocalitiesToClusterImpl(tt.localities, tt.priorityName, tt.mechanism, nil, tt.childPolicy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("priorityLocalitiesToClusterImpl() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(got, tt.wantConfig); diff != "" {
				t.Errorf("localitiesToWeightedTarget() diff (-got +want) %v", diff)
			}
			if diff := cmp.Diff(got1, tt.wantEndpoints, cmp.AllowUnexported(attributes.Attributes{})); diff != "" {
				t.Errorf("localitiesToWeightedTarget() diff (-got +want) %v", diff)
			}
		})
	}
}

func testEndpointWithAttrs(addrStrs []string, localityWeight, endpointWeight uint32, priority string, lID *clients.Locality) resolver.Endpoint {
	endpoint := resolver.Endpoint{}
	for _, a := range addrStrs {
		endpoint.Addresses = append(endpoint.Addresses, resolver.Address{Addr: a})
	}
	path := []string{priority}
	if lID != nil {
		path = append(path, xdsinternal.LocalityString(*lID))
		endpoint = xdsinternal.SetLocalityIDInEndpoint(endpoint, *lID)
	}
	endpoint = hierarchy.SetInEndpoint(endpoint, path)
	endpoint = wrrlocality.SetAddrInfoInEndpoint(endpoint, wrrlocality.AddrInfo{LocalityWeight: localityWeight})
	endpoint = weight.Set(endpoint, weight.EndpointInfo{Weight: localityWeight * endpointWeight})
	return endpoint
}

func TestConvertClusterImplMapToOutlierDetection(t *testing.T) {
	tests := []struct {
		name       string
		ciCfgsMap  map[string]*clusterimpl.LBConfig
		odCfg      outlierdetection.LBConfig
		wantODCfgs map[string]*outlierdetection.LBConfig
	}{
		{
			name: "single-entry-noop",
			ciCfgsMap: map[string]*clusterimpl.LBConfig{
				"child1": {
					Cluster: "cluster1",
				},
			},
			odCfg: outlierdetection.LBConfig{
				Interval: 1<<63 - 1,
			},
			wantODCfgs: map[string]*outlierdetection.LBConfig{
				"child1": {
					Interval: 1<<63 - 1,
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: clusterimpl.Name,
						Config: &clusterimpl.LBConfig{
							Cluster: "cluster1",
						},
					},
				},
			},
		},
		{
			name: "multiple-entries-noop",
			ciCfgsMap: map[string]*clusterimpl.LBConfig{
				"child1": {
					Cluster: "cluster1",
				},
				"child2": {
					Cluster: "cluster2",
				},
			},
			odCfg: outlierdetection.LBConfig{
				Interval: 1<<63 - 1,
			},
			wantODCfgs: map[string]*outlierdetection.LBConfig{
				"child1": {
					Interval: 1<<63 - 1,
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: clusterimpl.Name,
						Config: &clusterimpl.LBConfig{
							Cluster: "cluster1",
						},
					},
				},
				"child2": {
					Interval: 1<<63 - 1,
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: clusterimpl.Name,
						Config: &clusterimpl.LBConfig{
							Cluster: "cluster2",
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := convertClusterImplMapToOutlierDetection(test.ciCfgsMap, test.odCfg)
			if diff := cmp.Diff(got, test.wantODCfgs); diff != "" {
				t.Fatalf("convertClusterImplMapToOutlierDetection() diff(-got +want) %v", diff)
			}
		})
	}
}
