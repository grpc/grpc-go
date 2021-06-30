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

package balancerconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/balancer/weightedroundrobin"
	"google.golang.org/grpc/internal/hierarchy"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/clusterimpl"
	"google.golang.org/grpc/xds/internal/balancer/priority"
	"google.golang.org/grpc/xds/internal/balancer/weightedtarget"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

const (
	testClusterName     = "test-cluster-name"
	testLRSServer       = "test-lrs-server"
	testMaxRequests     = 314
	testEDSServcie      = "test-eds-service-name"
	testEDSServiceName  = "service-name-from-parent"
	testDropCategory    = "test-drops"
	testDropOverMillion = 1

	localityCount      = 5
	addressPerLocality = 2
)

var (
	testLocalityIDs []internal.LocalityID
	testAddressStrs [][]string
	testEndpoints   [][]xdsclient.Endpoint

	testLocalitiesP0, testLocalitiesP1 []xdsclient.Locality

	addrCmpOpts = cmp.Options{
		cmp.AllowUnexported(attributes.Attributes{}),
		cmp.Transformer("SortAddrs", func(in []resolver.Address) []resolver.Address {
			out := append([]resolver.Address(nil), in...) // Copy input to avoid mutating it
			sort.Slice(out, func(i, j int) bool {
				return out[i].Addr < out[j].Addr
			})
			return out
		})}
)

func init() {
	for i := 0; i < localityCount; i++ {
		testLocalityIDs = append(testLocalityIDs, internal.LocalityID{Zone: fmt.Sprintf("test-zone-%d", i)})
		var (
			addrs []string
			ends  []xdsclient.Endpoint
		)
		for j := 0; j < addressPerLocality; j++ {
			addr := fmt.Sprintf("addr-%d-%d", i, j)
			addrs = append(addrs, addr)
			ends = append(ends, xdsclient.Endpoint{
				Address:      addr,
				HealthStatus: xdsclient.EndpointHealthStatusHealthy,
			})
		}
		testAddressStrs = append(testAddressStrs, addrs)
		testEndpoints = append(testEndpoints, ends)
	}

	testLocalitiesP0 = []xdsclient.Locality{
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
	testLocalitiesP1 = []xdsclient.Locality{
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
	gotConfig, _, err := BuildPriorityConfigJSON([]PriorityConfig{
		{
			Mechanism: DiscoveryMechanism{
				Cluster:                 testClusterName,
				LoadReportingServerName: newString(testLRSServer),
				MaxConcurrentRequests:   newUint32(testMaxRequests),
				Type:                    DiscoveryMechanismTypeEDS,
				EDSServiceName:          testEDSServiceName,
			},
			EDSResp: xdsclient.EndpointsUpdate{
				Drops: []xdsclient.OverloadDropConfig{
					{
						Category:    testDropCategory,
						Numerator:   testDropOverMillion,
						Denominator: million,
					},
				},
				Localities: []xdsclient.Locality{
					testLocalitiesP0[0],
					testLocalitiesP0[1],
					testLocalitiesP1[0],
					testLocalitiesP1[1],
				},
			},
		},
		{
			Mechanism: DiscoveryMechanism{
				Type: DiscoveryMechanismTypeLogicalDNS,
			},
			Addresses: testAddressStrs[4],
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

func TestBuildPriorityConfig(t *testing.T) {
	gotConfig, gotAddrs := buildPriorityConfig([]PriorityConfig{
		{
			Mechanism: DiscoveryMechanism{
				Cluster:                 testClusterName,
				LoadReportingServerName: newString(testLRSServer),
				MaxConcurrentRequests:   newUint32(testMaxRequests),
				Type:                    DiscoveryMechanismTypeEDS,
				EDSServiceName:          testEDSServiceName,
			},
			EDSResp: xdsclient.EndpointsUpdate{
				Drops: []xdsclient.OverloadDropConfig{
					{
						Category:    testDropCategory,
						Numerator:   testDropOverMillion,
						Denominator: million,
					},
				},
				Localities: []xdsclient.Locality{
					testLocalitiesP0[0],
					testLocalitiesP0[1],
					testLocalitiesP1[0],
					testLocalitiesP1[1],
				},
			},
		},
		{
			Mechanism: DiscoveryMechanism{
				Type: DiscoveryMechanismTypeLogicalDNS,
			},
			Addresses: testAddressStrs[4],
		},
	}, nil)

	wantConfig := &priority.LBConfig{
		Children: map[string]*priority.Child{
			"priority-0-0": {
				Config: &internalserviceconfig.BalancerConfig{
					Name: clusterimpl.Name,
					Config: &clusterimpl.LBConfig{
						Cluster:                 testClusterName,
						EDSServiceName:          testEDSServiceName,
						LoadReportingServerName: newString(testLRSServer),
						MaxConcurrentRequests:   newUint32(testMaxRequests),
						DropCategories: []clusterimpl.DropConfig{
							{
								Category:           testDropCategory,
								RequestsPerMillion: testDropOverMillion,
							},
						},
						ChildPolicy: &internalserviceconfig.BalancerConfig{
							Name: weightedtarget.Name,
							Config: &weightedtarget.LBConfig{
								Targets: map[string]weightedtarget.Target{
									assertString(testLocalityIDs[0].ToString): {
										Weight:      20,
										ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
									},
									assertString(testLocalityIDs[1].ToString): {
										Weight:      80,
										ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
									},
								},
							},
						},
					},
				},
				IgnoreReresolutionRequests: true,
			},
			"priority-0-1": {
				Config: &internalserviceconfig.BalancerConfig{
					Name: clusterimpl.Name,
					Config: &clusterimpl.LBConfig{
						Cluster:                 testClusterName,
						EDSServiceName:          testEDSServiceName,
						LoadReportingServerName: newString(testLRSServer),
						MaxConcurrentRequests:   newUint32(testMaxRequests),
						DropCategories: []clusterimpl.DropConfig{
							{
								Category:           testDropCategory,
								RequestsPerMillion: testDropOverMillion,
							},
						},
						ChildPolicy: &internalserviceconfig.BalancerConfig{
							Name: weightedtarget.Name,
							Config: &weightedtarget.LBConfig{
								Targets: map[string]weightedtarget.Target{
									assertString(testLocalityIDs[2].ToString): {
										Weight:      20,
										ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
									},
									assertString(testLocalityIDs[3].ToString): {
										Weight:      80,
										ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
									},
								},
							},
						},
					},
				},
				IgnoreReresolutionRequests: true,
			},
			"priority-1": {
				Config: &internalserviceconfig.BalancerConfig{
					Name: clusterimpl.Name,
					Config: &clusterimpl.LBConfig{
						ChildPolicy: &internalserviceconfig.BalancerConfig{Name: "pick_first"},
					},
				},
				IgnoreReresolutionRequests: false,
			},
		},
		Priorities: []string{"priority-0-0", "priority-0-1", "priority-1"},
	}
	wantAddrs := []resolver.Address{
		testAddrWithAttrs(testAddressStrs[0][0], nil, "priority-0-0", &testLocalityIDs[0]),
		testAddrWithAttrs(testAddressStrs[0][1], nil, "priority-0-0", &testLocalityIDs[0]),
		testAddrWithAttrs(testAddressStrs[1][0], nil, "priority-0-0", &testLocalityIDs[1]),
		testAddrWithAttrs(testAddressStrs[1][1], nil, "priority-0-0", &testLocalityIDs[1]),
		testAddrWithAttrs(testAddressStrs[2][0], nil, "priority-0-1", &testLocalityIDs[2]),
		testAddrWithAttrs(testAddressStrs[2][1], nil, "priority-0-1", &testLocalityIDs[2]),
		testAddrWithAttrs(testAddressStrs[3][0], nil, "priority-0-1", &testLocalityIDs[3]),
		testAddrWithAttrs(testAddressStrs[3][1], nil, "priority-0-1", &testLocalityIDs[3]),
		testAddrWithAttrs(testAddressStrs[4][0], nil, "priority-1", nil),
		testAddrWithAttrs(testAddressStrs[4][1], nil, "priority-1", nil),
	}

	if diff := cmp.Diff(gotConfig, wantConfig); diff != "" {
		t.Errorf("buildPriorityConfig() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotAddrs, wantAddrs, addrCmpOpts); diff != "" {
		t.Errorf("buildPriorityConfig() diff (-got +want) %v", diff)
	}
}

func TestBuildClusterImplConfigForDNS(t *testing.T) {
	gotName, gotConfig, gotAddrs := buildClusterImplConfigForDNS(3, testAddressStrs[0])
	wantName := "priority-3"
	wantConfig := &clusterimpl.LBConfig{
		ChildPolicy: &internalserviceconfig.BalancerConfig{
			Name: "pick_first",
		},
	}
	wantAddrs := []resolver.Address{
		hierarchy.Set(resolver.Address{Addr: testAddressStrs[0][0]}, []string{"priority-3"}),
		hierarchy.Set(resolver.Address{Addr: testAddressStrs[0][1]}, []string{"priority-3"}),
	}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("buildClusterImplConfigForDNS() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotConfig, wantConfig); diff != "" {
		t.Errorf("buildClusterImplConfigForDNS() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotAddrs, wantAddrs, addrCmpOpts); diff != "" {
		t.Errorf("buildClusterImplConfigForDNS() diff (-got +want) %v", diff)
	}
}

func TestBuildClusterImplConfigForEDS(t *testing.T) {
	gotNames, gotConfigs, gotAddrs := buildClusterImplConfigForEDS(
		2,
		xdsclient.EndpointsUpdate{
			Drops: []xdsclient.OverloadDropConfig{
				{
					Category:    testDropCategory,
					Numerator:   testDropOverMillion,
					Denominator: million,
				},
			},
			Localities: []xdsclient.Locality{
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
			Cluster:                 testClusterName,
			MaxConcurrentRequests:   newUint32(testMaxRequests),
			LoadReportingServerName: newString(testLRSServer),
			Type:                    DiscoveryMechanismTypeEDS,
			EDSServiceName:          testEDSServiceName,
		},
		nil,
	)

	wantNames := []string{
		fmt.Sprintf("priority-%v-%v", 2, 0),
		fmt.Sprintf("priority-%v-%v", 2, 1),
	}
	wantConfigs := map[string]*clusterimpl.LBConfig{
		"priority-2-0": {
			Cluster:                 testClusterName,
			EDSServiceName:          testEDSServiceName,
			LoadReportingServerName: newString(testLRSServer),
			MaxConcurrentRequests:   newUint32(testMaxRequests),
			DropCategories: []clusterimpl.DropConfig{
				{
					Category:           testDropCategory,
					RequestsPerMillion: testDropOverMillion,
				},
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: weightedtarget.Name,
				Config: &weightedtarget.LBConfig{
					Targets: map[string]weightedtarget.Target{
						assertString(testLocalityIDs[0].ToString): {
							Weight:      20,
							ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
						},
						assertString(testLocalityIDs[1].ToString): {
							Weight:      80,
							ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
						},
					},
				},
			},
		},
		"priority-2-1": {
			Cluster:                 testClusterName,
			EDSServiceName:          testEDSServiceName,
			LoadReportingServerName: newString(testLRSServer),
			MaxConcurrentRequests:   newUint32(testMaxRequests),
			DropCategories: []clusterimpl.DropConfig{
				{
					Category:           testDropCategory,
					RequestsPerMillion: testDropOverMillion,
				},
			},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: weightedtarget.Name,
				Config: &weightedtarget.LBConfig{
					Targets: map[string]weightedtarget.Target{
						assertString(testLocalityIDs[2].ToString): {
							Weight:      20,
							ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
						},
						assertString(testLocalityIDs[3].ToString): {
							Weight:      80,
							ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
						},
					},
				},
			},
		},
	}
	wantAddrs := []resolver.Address{
		testAddrWithAttrs(testAddressStrs[0][0], nil, "priority-2-0", &testLocalityIDs[0]),
		testAddrWithAttrs(testAddressStrs[0][1], nil, "priority-2-0", &testLocalityIDs[0]),
		testAddrWithAttrs(testAddressStrs[1][0], nil, "priority-2-0", &testLocalityIDs[1]),
		testAddrWithAttrs(testAddressStrs[1][1], nil, "priority-2-0", &testLocalityIDs[1]),
		testAddrWithAttrs(testAddressStrs[2][0], nil, "priority-2-1", &testLocalityIDs[2]),
		testAddrWithAttrs(testAddressStrs[2][1], nil, "priority-2-1", &testLocalityIDs[2]),
		testAddrWithAttrs(testAddressStrs[3][0], nil, "priority-2-1", &testLocalityIDs[3]),
		testAddrWithAttrs(testAddressStrs[3][1], nil, "priority-2-1", &testLocalityIDs[3]),
	}

	if diff := cmp.Diff(gotNames, wantNames); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotConfigs, wantConfigs); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-got +want) %v", diff)
	}
	if diff := cmp.Diff(gotAddrs, wantAddrs, addrCmpOpts); diff != "" {
		t.Errorf("buildClusterImplConfigForEDS() diff (-got +want) %v", diff)
	}

}

func TestGroupLocalitiesByPriority(t *testing.T) {
	tests := []struct {
		name           string
		localities     []xdsclient.Locality
		wantPriorities []string
		wantLocalities map[string][]xdsclient.Locality
	}{
		{
			name:           "1 locality 1 priority",
			localities:     []xdsclient.Locality{testLocalitiesP0[0]},
			wantPriorities: []string{"0"},
			wantLocalities: map[string][]xdsclient.Locality{
				"0": {testLocalitiesP0[0]},
			},
		},
		{
			name:           "2 locality 1 priority",
			localities:     []xdsclient.Locality{testLocalitiesP0[0], testLocalitiesP0[1]},
			wantPriorities: []string{"0"},
			wantLocalities: map[string][]xdsclient.Locality{
				"0": {testLocalitiesP0[0], testLocalitiesP0[1]},
			},
		},
		{
			name:           "1 locality in each",
			localities:     []xdsclient.Locality{testLocalitiesP0[0], testLocalitiesP1[0]},
			wantPriorities: []string{"0", "1"},
			wantLocalities: map[string][]xdsclient.Locality{
				"0": {testLocalitiesP0[0]},
				"1": {testLocalitiesP1[0]},
			},
		},
		{
			name: "2 localities in each sorted",
			localities: []xdsclient.Locality{
				testLocalitiesP0[0], testLocalitiesP0[1],
				testLocalitiesP1[0], testLocalitiesP1[1]},
			wantPriorities: []string{"0", "1"},
			wantLocalities: map[string][]xdsclient.Locality{
				"0": {testLocalitiesP0[0], testLocalitiesP0[1]},
				"1": {testLocalitiesP1[0], testLocalitiesP1[1]},
			},
		},
		{
			// The localities are given in order [p1, p0, p1, p0], but the
			// returned priority list must be sorted [p0, p1], because the list
			// order is the priority order.
			name: "2 localities in each needs to sort",
			localities: []xdsclient.Locality{
				testLocalitiesP1[1], testLocalitiesP0[1],
				testLocalitiesP1[0], testLocalitiesP0[0]},
			wantPriorities: []string{"0", "1"},
			wantLocalities: map[string][]xdsclient.Locality{
				"0": {testLocalitiesP0[1], testLocalitiesP0[0]},
				"1": {testLocalitiesP1[1], testLocalitiesP1[0]},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPriorities, gotLocalities := groupLocalitiesByPriority(tt.localities)
			if diff := cmp.Diff(gotPriorities, tt.wantPriorities); diff != "" {
				t.Errorf("groupLocalitiesByPriority() diff(-got +want) %v", diff)
			}
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

func TestLocalitiesToWeightedTarget(t *testing.T) {
	tests := []struct {
		name         string
		localities   []xdsclient.Locality
		priorityName string
		childPolicy  *internalserviceconfig.BalancerConfig
		lrsServer    *string
		cluster      string
		edsService   string
		wantConfig   *weightedtarget.LBConfig
		wantAddrs    []resolver.Address
	}{
		{
			name: "roundrobin as child, with LRS",
			localities: []xdsclient.Locality{
				{
					Endpoints: []xdsclient.Endpoint{
						{Address: "addr-1-1", HealthStatus: xdsclient.EndpointHealthStatusHealthy},
						{Address: "addr-1-2", HealthStatus: xdsclient.EndpointHealthStatusHealthy},
					},
					ID:     internal.LocalityID{Zone: "test-zone-1"},
					Weight: 20,
				},
				{
					Endpoints: []xdsclient.Endpoint{
						{Address: "addr-2-1", HealthStatus: xdsclient.EndpointHealthStatusHealthy},
						{Address: "addr-2-2", HealthStatus: xdsclient.EndpointHealthStatusHealthy},
					},
					ID:     internal.LocalityID{Zone: "test-zone-2"},
					Weight: 80,
				},
			},
			priorityName: "test-priority",
			childPolicy:  &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
			lrsServer:    newString("test-lrs-server"),
			cluster:      "test-cluster",
			edsService:   "test-eds-service",
			wantConfig: &weightedtarget.LBConfig{
				Targets: map[string]weightedtarget.Target{
					assertString(internal.LocalityID{Zone: "test-zone-1"}.ToString): {
						Weight:      20,
						ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
					},
					assertString(internal.LocalityID{Zone: "test-zone-2"}.ToString): {
						Weight:      80,
						ChildPolicy: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
					},
				},
			},
			wantAddrs: []resolver.Address{
				testAddrWithAttrs("addr-1-1", nil, "test-priority", &internal.LocalityID{Zone: "test-zone-1"}),
				testAddrWithAttrs("addr-1-2", nil, "test-priority", &internal.LocalityID{Zone: "test-zone-1"}),
				testAddrWithAttrs("addr-2-1", nil, "test-priority", &internal.LocalityID{Zone: "test-zone-2"}),
				testAddrWithAttrs("addr-2-2", nil, "test-priority", &internal.LocalityID{Zone: "test-zone-2"}),
			},
		},
		{
			name: "roundrobin as child, no LRS",
			localities: []xdsclient.Locality{
				{
					Endpoints: []xdsclient.Endpoint{
						{Address: "addr-1-1", HealthStatus: xdsclient.EndpointHealthStatusHealthy},
						{Address: "addr-1-2", HealthStatus: xdsclient.EndpointHealthStatusHealthy},
					},
					ID:     internal.LocalityID{Zone: "test-zone-1"},
					Weight: 20,
				},
				{
					Endpoints: []xdsclient.Endpoint{
						{Address: "addr-2-1", HealthStatus: xdsclient.EndpointHealthStatusHealthy},
						{Address: "addr-2-2", HealthStatus: xdsclient.EndpointHealthStatusHealthy},
					},
					ID:     internal.LocalityID{Zone: "test-zone-2"},
					Weight: 80,
				},
			},
			priorityName: "test-priority",
			childPolicy:  &internalserviceconfig.BalancerConfig{Name: roundrobin.Name},
			// lrsServer is nil, so LRS policy will not be used.
			wantConfig: &weightedtarget.LBConfig{
				Targets: map[string]weightedtarget.Target{
					assertString(internal.LocalityID{Zone: "test-zone-1"}.ToString): {
						Weight: 20,
						ChildPolicy: &internalserviceconfig.BalancerConfig{
							Name: roundrobin.Name,
						},
					},
					assertString(internal.LocalityID{Zone: "test-zone-2"}.ToString): {
						Weight: 80,
						ChildPolicy: &internalserviceconfig.BalancerConfig{
							Name: roundrobin.Name,
						},
					},
				},
			},
			wantAddrs: []resolver.Address{
				testAddrWithAttrs("addr-1-1", nil, "test-priority", &internal.LocalityID{Zone: "test-zone-1"}),
				testAddrWithAttrs("addr-1-2", nil, "test-priority", &internal.LocalityID{Zone: "test-zone-1"}),
				testAddrWithAttrs("addr-2-1", nil, "test-priority", &internal.LocalityID{Zone: "test-zone-2"}),
				testAddrWithAttrs("addr-2-2", nil, "test-priority", &internal.LocalityID{Zone: "test-zone-2"}),
			},
		},
		{
			name: "weighted round robin as child, no LRS",
			localities: []xdsclient.Locality{
				{
					Endpoints: []xdsclient.Endpoint{
						{Address: "addr-1-1", HealthStatus: xdsclient.EndpointHealthStatusHealthy, Weight: 90},
						{Address: "addr-1-2", HealthStatus: xdsclient.EndpointHealthStatusHealthy, Weight: 10},
					},
					ID:     internal.LocalityID{Zone: "test-zone-1"},
					Weight: 20,
				},
				{
					Endpoints: []xdsclient.Endpoint{
						{Address: "addr-2-1", HealthStatus: xdsclient.EndpointHealthStatusHealthy, Weight: 90},
						{Address: "addr-2-2", HealthStatus: xdsclient.EndpointHealthStatusHealthy, Weight: 10},
					},
					ID:     internal.LocalityID{Zone: "test-zone-2"},
					Weight: 80,
				},
			},
			priorityName: "test-priority",
			childPolicy:  &internalserviceconfig.BalancerConfig{Name: weightedroundrobin.Name},
			// lrsServer is nil, so LRS policy will not be used.
			wantConfig: &weightedtarget.LBConfig{
				Targets: map[string]weightedtarget.Target{
					assertString(internal.LocalityID{Zone: "test-zone-1"}.ToString): {
						Weight: 20,
						ChildPolicy: &internalserviceconfig.BalancerConfig{
							Name: weightedroundrobin.Name,
						},
					},
					assertString(internal.LocalityID{Zone: "test-zone-2"}.ToString): {
						Weight: 80,
						ChildPolicy: &internalserviceconfig.BalancerConfig{
							Name: weightedroundrobin.Name,
						},
					},
				},
			},
			wantAddrs: []resolver.Address{
				testAddrWithAttrs("addr-1-1", newUint32(90), "test-priority", &internal.LocalityID{Zone: "test-zone-1"}),
				testAddrWithAttrs("addr-1-2", newUint32(10), "test-priority", &internal.LocalityID{Zone: "test-zone-1"}),
				testAddrWithAttrs("addr-2-1", newUint32(90), "test-priority", &internal.LocalityID{Zone: "test-zone-2"}),
				testAddrWithAttrs("addr-2-2", newUint32(10), "test-priority", &internal.LocalityID{Zone: "test-zone-2"}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := localitiesToWeightedTarget(tt.localities, tt.priorityName, tt.childPolicy, tt.cluster, tt.edsService)
			if diff := cmp.Diff(got, tt.wantConfig); diff != "" {
				t.Errorf("localitiesToWeightedTarget() diff (-got +want) %v", diff)
			}
			if diff := cmp.Diff(got1, tt.wantAddrs, cmp.AllowUnexported(attributes.Attributes{})); diff != "" {
				t.Errorf("localitiesToWeightedTarget() diff (-got +want) %v", diff)
			}
		})
	}
}

func newString(s string) *string {
	return &s
}

func newUint32(i uint32) *uint32 {
	return &i
}

func assertString(f func() (string, error)) string {
	s, err := f()
	if err != nil {
		panic(err.Error())
	}
	return s
}

func testAddrWithAttrs(addrStr string, weight *uint32, priority string, lID *internal.LocalityID) resolver.Address {
	addr := resolver.Address{Addr: addrStr}
	if weight != nil {
		addr = weightedroundrobin.SetAddrInfo(addr, weightedroundrobin.AddrInfo{Weight: *weight})
	}
	path := []string{priority}
	if lID != nil {
		path = append(path, assertString(lID.ToString))
		addr = internal.SetLocalityID(addr, *lID)
	}
	addr = hierarchy.Set(addr, path)
	return addr
}
