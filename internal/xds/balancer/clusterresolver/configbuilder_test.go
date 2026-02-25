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
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/balancer/ringhash"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/internal/balancer/weight"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/hierarchy"
	iringhash "google.golang.org/grpc/internal/ringhash"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
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
	testMaxRequests     = 314
	testEDSServiceName  = "service-name-from-parent"
	testDropCategory    = "test-drops"
	testDropOverMillion = 1
)

var (
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

// makeLocalityID creates a clients.Locality with Zone set to
// "test-zone-{idx}".
func makeLocalityID(idx int) clients.Locality {
	return clients.Locality{Zone: fmt.Sprintf("test-zone-%d", idx)}
}

// makeEndpoint creates a test xdsresource.Endpoint with a healthy status, the
// specified endpoint weight, and three addresses: "addr-{li}-{ei}",
// "addr-{li}-{ei}-additional-1", and "addr-{li}-{ei}-additional-2".
func makeEndpoint(li, ei int, ew uint32) xdsresource.Endpoint {
	addr := fmt.Sprintf("addr-%d-%d", li, ei)
	return xdsresource.Endpoint{
		HealthStatus: xdsresource.EndpointHealthStatusHealthy,
		ResolverEndpoint: resolver.Endpoint{
			Addresses: []resolver.Address{
				{Addr: addr},
				{Addr: fmt.Sprintf("%s-additional-1", addr)},
				{Addr: fmt.Sprintf("%s-additional-2", addr)},
			},
		},
		Weight: ew,
	}
}

// makeResolverEndpoint creates a resolver.Endpoint with a single address
// "addr-{li}-{ei}".
func makeResolverEndpoint(li, ei int) resolver.Endpoint {
	return resolver.Endpoint{
		Addresses: []resolver.Address{{Addr: fmt.Sprintf("addr-%d-%d", li, ei)}},
	}
}

// makeLocality creates an xdsresource.Locality with endpointCount endpoints,
// each with the given endpoint weight. The locality has the specified weight
// and priority. The locality ID and endpoint addresses are derived from
// localityIdx.
func makeLocality(localityIdx int, localityWeight, priority uint32, endpointCount int, endpointWeight uint32) xdsresource.Locality {
	endpoints := make([]xdsresource.Endpoint, endpointCount)
	for j := range endpointCount {
		endpoints[j] = makeEndpoint(localityIdx, j, endpointWeight)
	}
	return xdsresource.Locality{
		Endpoints: endpoints,
		ID:        makeLocalityID(localityIdx),
		Weight:    localityWeight,
		Priority:  priority,
	}
}

// TestBuildPriorityConfigJSON is a sanity check that the built balancer config
// can be parsed. The behavior test is covered by TestBuildPriorityConfig.
func (s) TestBuildPriorityConfigJSON(t *testing.T) {
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
					makeLocality(0, 20, 0, 2, 1),
					makeLocality(1, 80, 0, 2, 1),
					makeLocality(2, 20, 1, 2, 1),
					makeLocality(3, 80, 1, 2, 1),
				},
			},
			childNameGen: newNameGenerator(0),
		},
		{
			mechanism: DiscoveryMechanism{
				Type: DiscoveryMechanismTypeLogicalDNS,
			},
			endpoints: []resolver.Endpoint{makeResolverEndpoint(4, 0), makeResolverEndpoint(4, 1)},
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
func (s) TestBuildPriorityConfig(t *testing.T) {
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
					makeLocality(0, 20, 0, 2, 1),
					makeLocality(1, 80, 0, 2, 1),
					makeLocality(2, 20, 1, 2, 1),
					makeLocality(3, 80, 1, 2, 1),
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
			endpoints:    []resolver.Endpoint{makeResolverEndpoint(4, 0), makeResolverEndpoint(4, 1)},
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
								Cluster: testClusterName2,
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

func testEndpointForDNS(endpoints []resolver.Endpoint, localityWeight uint32, path []string) resolver.Endpoint {
	retEndpoint := resolver.Endpoint{}
	for _, e := range endpoints {
		retEndpoint.Addresses = append(retEndpoint.Addresses, e.Addresses...)
	}
	retEndpoint = hierarchy.SetInEndpoint(retEndpoint, path)
	retEndpoint = wrrlocality.SetAddrInfo(retEndpoint, wrrlocality.AddrInfo{LocalityWeight: localityWeight})
	return retEndpoint
}

func (s) TestBuildClusterImplConfigForDNS(t *testing.T) {
	for _, tt := range []struct {
		name        string
		endpoints   []resolver.Endpoint
		xdsLBPolicy *iserviceconfig.BalancerConfig
	}{
		{
			name:        "one_endpoint_one_address",
			endpoints:   []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: "addr-0-0"}}}},
			xdsLBPolicy: &iserviceconfig.BalancerConfig{Name: pickfirst.Name},
		},
		{
			name: "one_endpoint_multiple_addresses",
			endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{
				{Addr: "addr-0-0"},
				{Addr: "addr-0-1"},
			}}},
			xdsLBPolicy: &iserviceconfig.BalancerConfig{Name: wrrlocality.Name},
		},
		{
			name: "multiple_endpoints_one_address_each",
			endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "addr-0-0"}}},
				{Addresses: []resolver.Address{{Addr: "addr-0-1"}}},
			},
			xdsLBPolicy: &iserviceconfig.BalancerConfig{Name: roundrobin.Name},
		},
		{
			name: "multiple_endpoints_multiple_addresses",
			endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{
					{Addr: "addr-0-0"},
					{Addr: "addr-0-1"},
				}},
				{Addresses: []resolver.Address{
					{Addr: "addr-1-0"},
					{Addr: "addr-1-1"},
				}},
			},
			xdsLBPolicy: &iserviceconfig.BalancerConfig{Name: roundrobin.Name},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotConfig, gotEndpoints := buildClusterImplConfigForDNS(newNameGenerator(3), tt.endpoints, DiscoveryMechanism{Cluster: testClusterName2, Type: DiscoveryMechanismTypeLogicalDNS}, tt.xdsLBPolicy)
			const wantName = "priority-3"
			if diff := cmp.Diff(wantName, gotName); diff != "" {
				t.Errorf("buildClusterImplConfigForDNS() diff (-want +got) %v", diff)
			}

			wantConfig := &clusterimpl.LBConfig{
				Cluster:     testClusterName2,
				ChildPolicy: tt.xdsLBPolicy,
			}
			if diff := cmp.Diff(wantConfig, gotConfig); diff != "" {
				t.Errorf("buildClusterImplConfigForDNS() diff (-want +got) %v", diff)
			}

			wantEndpoints := []resolver.Endpoint{testEndpointForDNS(tt.endpoints, 1, []string{wantName, xdsinternal.LocalityString(clients.Locality{})})}
			if diff := cmp.Diff(wantEndpoints, gotEndpoints, endpointCmpOpts); diff != "" {
				t.Errorf("buildClusterImplConfigForDNS() diff (-want +got) %v", diff)
			}
		})
	}
}

func TestBuildClusterImplConfigForEDS_PickFirstWeightedShuffling_Disabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.PickFirstWeightedShuffling, false)

	testLRSServerConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}

	// Create test localities with 2 endpoints each, endpoint weight 1.
	// Localities are listed in non-priority order to verify sorting.
	loc0 := makeLocality(0, 20, 0, 2, 1)
	loc1 := makeLocality(1, 80, 0, 2, 1)
	loc2 := makeLocality(2, 20, 1, 2, 1)
	loc3 := makeLocality(3, 80, 1, 2, 1)

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
			Localities: []xdsresource.Locality{loc3, loc1, loc2, loc0},
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

	wantNames := []string{"priority-2-0", "priority-2-1"}
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
	// Endpoint weight is the product of locality weight and endpoint weight.
	wantEndpoints := []resolver.Endpoint{
		testEndpointWithAttrs(loc0.Endpoints[0].ResolverEndpoint, 20, 20*1, "priority-2-0", &loc0.ID),
		testEndpointWithAttrs(loc0.Endpoints[1].ResolverEndpoint, 20, 20*1, "priority-2-0", &loc0.ID),
		testEndpointWithAttrs(loc1.Endpoints[0].ResolverEndpoint, 80, 80*1, "priority-2-0", &loc1.ID),
		testEndpointWithAttrs(loc1.Endpoints[1].ResolverEndpoint, 80, 80*1, "priority-2-0", &loc1.ID),
		testEndpointWithAttrs(loc2.Endpoints[0].ResolverEndpoint, 20, 20*1, "priority-2-1", &loc2.ID),
		testEndpointWithAttrs(loc2.Endpoints[1].ResolverEndpoint, 20, 20*1, "priority-2-1", &loc2.ID),
		testEndpointWithAttrs(loc3.Endpoints[0].ResolverEndpoint, 80, 80*1, "priority-2-1", &loc3.ID),
		testEndpointWithAttrs(loc3.Endpoints[1].ResolverEndpoint, 80, 80*1, "priority-2-1", &loc3.ID),
	}

	if diff := cmp.Diff(wantNames, gotNames); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-want +got) %v", diff)
	}
	if diff := cmp.Diff(wantConfigs, gotConfigs); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-want +got) %v", diff)
	}
	if diff := cmp.Diff(wantEndpoints, gotEndpoints, endpointCmpOpts); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-want +got) %v", diff)
	}
}

func TestBuildClusterImplConfigForEDS_PickFirstWeightedShuffling_Enabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.PickFirstWeightedShuffling, true)

	testLRSServerConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}

	// Create test localities with 2 endpoints each, endpoint weight 1.
	// Localities are listed in non-priority order to verify sorting.
	loc0 := makeLocality(0, 20, 0, 2, 1)
	loc1 := makeLocality(1, 80, 0, 2, 1)
	loc2 := makeLocality(2, 20, 1, 2, 1)
	loc3 := makeLocality(3, 80, 1, 2, 1)

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
			Localities: []xdsresource.Locality{loc3, loc1, loc2, loc0},
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

	wantNames := []string{"priority-2-0", "priority-2-1"}
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
	// Endpoints weights are the product of normalized locality weight and
	// endpoint weight, represented as a fixed-point number in uQ1.31 format.
	// Locality weights are normalized as:
	//   P1: locality 3: 80 / (100) = 0.8
	//   P0: locality 1: 80 / (100) = 0.8
	//   P1: locality 2: 20 / (100) = 0.2
	//   P0: locality 0: 20 / (100) = 0.2
	// In fixed-point uQ1.31 format, the weights are:
	//   locality 3: 0.8 * 2^31 = 1717986918
	//   locality 1: 0.8 * 2^31 = 1717986918
	//   locality 2: 0.2 * 2^31 =  429496729
	//   locality 0: 0.2 * 2^31 =  429496729
	//
	// There are two endpoints in each locality, each with weight 1. So, their
	// normalized weights are 0.5 each. And the final endpoint weights are a
	// product of their locality weights and 0.5, which turns out to be either
	//   1717986918 * 0.5 = 858993459, or,
	//    429496729 * 0.5 = 214748364
	wantEndpoints := []resolver.Endpoint{
		testEndpointWithAttrs(loc0.Endpoints[0].ResolverEndpoint, 20, 214748364, "priority-2-0", &loc0.ID),
		testEndpointWithAttrs(loc0.Endpoints[1].ResolverEndpoint, 20, 214748364, "priority-2-0", &loc0.ID),
		testEndpointWithAttrs(loc1.Endpoints[0].ResolverEndpoint, 80, 858993459, "priority-2-0", &loc1.ID),
		testEndpointWithAttrs(loc1.Endpoints[1].ResolverEndpoint, 80, 858993459, "priority-2-0", &loc1.ID),
		testEndpointWithAttrs(loc2.Endpoints[0].ResolverEndpoint, 20, 214748364, "priority-2-1", &loc2.ID),
		testEndpointWithAttrs(loc2.Endpoints[1].ResolverEndpoint, 20, 214748364, "priority-2-1", &loc2.ID),
		testEndpointWithAttrs(loc3.Endpoints[0].ResolverEndpoint, 80, 858993459, "priority-2-1", &loc3.ID),
		testEndpointWithAttrs(loc3.Endpoints[1].ResolverEndpoint, 80, 858993459, "priority-2-1", &loc3.ID),
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
	// Create localities for two priorities (p0 and p1).
	p0Loc0 := makeLocality(0, 20, 0, 2, 1)
	p0Loc1 := makeLocality(1, 80, 0, 2, 1)
	p1Loc0 := makeLocality(2, 20, 1, 2, 1)
	p1Loc1 := makeLocality(3, 80, 1, 2, 1)

	tests := []struct {
		name           string
		localities     []xdsresource.Locality
		wantLocalities [][]xdsresource.Locality
	}{
		{
			name:       "1 locality 1 priority",
			localities: []xdsresource.Locality{p0Loc0},
			wantLocalities: [][]xdsresource.Locality{
				{p0Loc0},
			},
		},
		{
			name:       "2 locality 1 priority",
			localities: []xdsresource.Locality{p0Loc0, p0Loc1},
			wantLocalities: [][]xdsresource.Locality{
				{p0Loc0, p0Loc1},
			},
		},
		{
			name:       "1 locality in each",
			localities: []xdsresource.Locality{p0Loc0, p1Loc0},
			wantLocalities: [][]xdsresource.Locality{
				{p0Loc0},
				{p1Loc0},
			},
		},
		{
			name: "2 localities in each sorted",
			localities: []xdsresource.Locality{
				p0Loc0, p0Loc1,
				p1Loc0, p1Loc1},
			wantLocalities: [][]xdsresource.Locality{
				{p0Loc0, p0Loc1},
				{p1Loc0, p1Loc1},
			},
		},
		{
			// The localities are given in order [p1, p0, p1, p0], but the
			// returned priority list must be sorted [p0, p1], because the list
			// order is the priority order.
			name: "2 localities in each needs to sort",
			localities: []xdsresource.Locality{
				p1Loc1, p0Loc1,
				p1Loc0, p0Loc0},
			wantLocalities: [][]xdsresource.Locality{
				{p0Loc1, p0Loc0},
				{p1Loc1, p1Loc0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLocalities := groupLocalitiesByPriority(tt.localities)
			if diff := cmp.Diff(tt.wantLocalities, gotLocalities); diff != "" {
				t.Errorf("groupLocalitiesByPriority() diff(-want +got) %v", diff)
			}
		})
	}
}

func TestPriorityLocalitiesToClusterImpl_PickFirstWeightedShuffling_Disabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.PickFirstWeightedShuffling, false)
	tests := []struct {
		name          string
		localities    []xdsresource.Locality
		priorityName  string
		mechanism     DiscoveryMechanism
		childPolicy   *iserviceconfig.BalancerConfig
		wantConfig    *clusterimpl.LBConfig
		wantEndpoints []resolver.Endpoint
		wantErr       bool
	}{
		{
			name: "round_robin_as_child_no_LRS",
			localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-1"}}},
							Weight:           90,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-2"}}},
							Weight:           10,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
					},
					ID:     clients.Locality{Zone: "test-zone-1"},
					Weight: 20,
				},
				{
					Endpoints: []xdsresource.Endpoint{
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-1"}}},
							Weight:           90,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-2"}}},
							Weight:           10,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
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
			// Endpoint weight is the product of locality weight and endpoint weight.
			wantEndpoints: []resolver.Endpoint{
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-1"}}}, 20, 20*90, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-2"}}}, 20, 20*10, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-1"}}}, 80, 80*90, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-2"}}}, 80, 80*10, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
			},
		},
		{
			name: "ring_hash_as_child",
			localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-1"}}},
							Weight:           90,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-2"}}},
							Weight:           10,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
					},
					ID:     clients.Locality{Zone: "test-zone-1"},
					Weight: 20,
				},
				{
					Endpoints: []xdsresource.Endpoint{
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-1"}}},
							Weight:           90,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-2"}}},
							Weight:           10,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
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
			// Endpoint weight is the product of locality weight and endpoint weight.
			wantEndpoints: []resolver.Endpoint{
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-1"}}}, 20, 20*90, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-2"}}}, 20, 20*10, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-1"}}}, 80, 80*90, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-2"}}}, 80, 80*10, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotConfig, gotEndpoints, err := priorityLocalitiesToClusterImpl(tt.localities, tt.priorityName, tt.mechanism, nil, tt.childPolicy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("priorityLocalitiesToClusterImpl() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(gotConfig, tt.wantConfig); diff != "" {
				t.Errorf("priorityLocalitiesToClusterImpl() diff (-got +want) %v", diff)
			}
			if diff := cmp.Diff(gotEndpoints, tt.wantEndpoints, cmp.AllowUnexported(attributes.Attributes{})); diff != "" {
				t.Errorf("priorityLocalitiesToClusterImpl() diff (-got +want) %v", diff)
			}
		})
	}
}

func TestPriorityLocalitiesToClusterImpl_PickFirstWeightedShuffling_Enabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.PickFirstWeightedShuffling, true)
	tests := []struct {
		name          string
		localities    []xdsresource.Locality
		priorityName  string
		mechanism     DiscoveryMechanism
		childPolicy   *iserviceconfig.BalancerConfig
		wantConfig    *clusterimpl.LBConfig
		wantEndpoints []resolver.Endpoint
		wantErr       bool
	}{
		{
			name: "round_robin_as_child_no_LRS",
			localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-1"}}},
							Weight:           90,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-2"}}},
							Weight:           10,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
					},
					ID:     clients.Locality{Zone: "test-zone-1"},
					Weight: 20,
				},
				{
					Endpoints: []xdsresource.Endpoint{
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-1"}}},
							Weight:           90,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-2"}}},
							Weight:           10,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
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
			// Endpoints weights are the product of normalized locality weight and
			// endpoint weight, represented as a fixed-point number in uQ1.31 format.
			// Locality weights are normalized as:
			//   locality 0: 20 / (100) = 0.2
			//   locality 1: 80 / (100) = 0.8
			// In fixed-point uQ1.31 format, the weights are:
			//   locality 0: 0.2 * 2^31 =  429496729
			//   locality 1: 0.8 * 2^31 = 1717986918
			//
			// The normalized weights of endpoints in each locality are:
			//   locality 0: endpoint 0: 90 / (100) = 0.9, endpoint 1: 10 / (100) = 0.1
			//   locality 1: endpoint 0: 90 / (100) = 0.9, endpoint 1: 10 / (100) = 0.1
			//
			// The final endpoint weights are a product of the above normalized weights,
			// which turns out to be:
			//   locality 0, endpoint 0:  0.2 * 0.9   =  386547056
			//   locality 0, endpoint 1:  0.2 * 0.1   =   42949672
			//   locality 1, endpoint 0:  0.8 * 0.9   = 1546188226
			//   locality 1, endpoint 1:  0.8 * 0.1   =  171798691
			wantEndpoints: []resolver.Endpoint{
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-1"}}}, 20, 386547056, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-2"}}}, 20, 42949672, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-1"}}}, 80, 1546188226, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-2"}}}, 80, 171798691, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
			},
		},
		{
			name: "ring_hash_as_child",
			localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-1"}}},
							Weight:           90,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-2"}}},
							Weight:           10,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
					},
					ID:     clients.Locality{Zone: "test-zone-1"},
					Weight: 20,
				},
				{
					Endpoints: []xdsresource.Endpoint{
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-1"}}},
							Weight:           90,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
						{
							ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-2"}}},
							Weight:           10,
							HealthStatus:     xdsresource.EndpointHealthStatusHealthy,
						},
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
			// Endpoints weights are the product of normalized locality weight and
			// endpoint weight, represented as a fixed-point number in uQ1.31 format.
			// Locality weights are normalized as:
			//   locality 0: 20 / (100) = 0.2
			//   locality 1: 80 / (100) = 0.8
			// In fixed-point uQ1.31 format, the weights are:
			//   locality 0: 0.2 * 2^31 =  429496729
			//   locality 1: 0.8 * 2^31 = 1717986918
			//
			// The normalized weights of endpoints in each locality are:
			//   locality 0: endpoint 0: 90 / (100) = 0.9, endpoint 1: 10 / (100) = 0.1
			//   locality 1: endpoint 0: 90 / (100) = 0.9, endpoint 1: 10 / (100) = 0.1
			//
			// The final endpoint weights are a product of the above normalized weights,
			// which turns out to be:
			//   locality 0, endpoint 0:  0.2 * 0.9   =  386547056
			//   locality 0, endpoint 1:  0.2 * 0.1   =   42949672
			//   locality 1, endpoint 0:  0.8 * 0.9   = 1546188226
			//   locality 1, endpoint 1:  0.8 * 0.1   =  171798691
			wantEndpoints: []resolver.Endpoint{
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-1"}}}, 20, 386547056, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1-2"}}}, 20, 42949672, "test-priority", &clients.Locality{Zone: "test-zone-1"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-1"}}}, 80, 1546188226, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
				testEndpointWithAttrs(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2-2"}}}, 80, 171798691, "test-priority", &clients.Locality{Zone: "test-zone-2"}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotConfig, gotEndpoints, err := priorityLocalitiesToClusterImpl(tt.localities, tt.priorityName, tt.mechanism, nil, tt.childPolicy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("priorityLocalitiesToClusterImpl() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(gotConfig, tt.wantConfig); diff != "" {
				t.Errorf("priorityLocalitiesToClusterImpl() diff (-got +want) %v", diff)
			}
			if diff := cmp.Diff(gotEndpoints, tt.wantEndpoints, cmp.AllowUnexported(attributes.Attributes{})); diff != "" {
				t.Errorf("priorityLocalitiesToClusterImpl() diff (-got +want) %v", diff)
			}
		})
	}
}

// testEndpointWithAttrs creates a resolver.Endpoint with the attributes that
// priorityLocalitiesToClusterImpl is expected to set: hierarchy path, locality
// ID, locality weight (for xds_wrr_locality), and final endpoint weight
// (for pick_first weighted shuffling). The endpointWeight should be the
// expected final computed weight, not the raw endpoint weight from xDS.
func testEndpointWithAttrs(endpoint resolver.Endpoint, localityWeight, endpointWeight uint32, priority string, lID *clients.Locality) resolver.Endpoint {
	path := []string{priority}
	if lID != nil {
		path = append(path, xdsinternal.LocalityString(*lID))
		endpoint = xdsinternal.SetLocalityIDInEndpoint(endpoint, *lID)
	}
	endpoint = hierarchy.SetInEndpoint(endpoint, path)
	endpoint = wrrlocality.SetAddrInfo(endpoint, wrrlocality.AddrInfo{LocalityWeight: localityWeight})
	endpoint = weight.Set(endpoint, weight.EndpointInfo{Weight: endpointWeight})
	return endpoint
}

func (s) TestConvertClusterImplMapToOutlierDetection(t *testing.T) {
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
			if diff := cmp.Diff(test.wantODCfgs, got); diff != "" {
				t.Fatalf("convertClusterImplMapToOutlierDetection() diff(-want +got) %v", diff)
			}
		})
	}
}
