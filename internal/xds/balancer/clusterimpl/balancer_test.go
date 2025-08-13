/*
 *
 * Copyright 2020 gRPC authors.
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

package clusterimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
	xdsinternal "google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/testutils/fakeclient"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultShortTestTimeout = 100 * time.Microsecond

	testClusterName = "test-cluster"
	testServiceName = "test-eds-service"

	testNamedMetricsKey1 = "test-named1"
	testNamedMetricsKey2 = "test-named2"
)

var (
	testBackendEndpoints = []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}}}
	cmpOpts              = cmp.Options{cmpopts.EquateEmpty(), cmp.AllowUnexported(loadData{}, localityData{}, requestData{}, serverLoadData{}), sortDataSlice}
	toleranceCmpOpt      = cmp.Options{cmpopts.EquateApprox(0, 1e-5), cmp.AllowUnexported(loadData{}, localityData{}, requestData{}, serverLoadData{})}
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// testLoadReporter records load data pertaining to a single cluster.
//
// It implements loadReporter interface for the picker. Tests can use it to
// override the loadStore in the picker to verify load reporting.
type testLoadReporter struct {
	cluster, service string

	mu               sync.Mutex
	drops            map[string]uint64
	localityRPCCount map[clients.Locality]*rpcCountData
}

// CallStarted records a call started for the clients.Locality.
func (lr *testLoadReporter) CallStarted(locality clients.Locality) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if _, ok := lr.localityRPCCount[locality]; !ok {
		lr.localityRPCCount[locality] = &rpcCountData{}
	}
	lr.localityRPCCount[locality].inProgress++
	lr.localityRPCCount[locality].issued++
}

// CallFinished records a call finished for the clients.Locality.
func (lr *testLoadReporter) CallFinished(locality clients.Locality, err error) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if lr.localityRPCCount == nil {
		return
	}
	lrc := lr.localityRPCCount[locality]
	lrc.inProgress--
	if err == nil {
		lrc.succeeded++
	} else {
		lrc.errored++
	}
}

// CallServerLoad records a server load for the clients.Locality.
func (lr *testLoadReporter) CallServerLoad(locality clients.Locality, name string, val float64) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if lr.localityRPCCount == nil {
		return
	}
	lrc, ok := lr.localityRPCCount[locality]
	if !ok {
		return
	}
	if lrc.serverLoads == nil {
		lrc.serverLoads = make(map[string]*rpcLoadData)
	}
	if _, ok := lrc.serverLoads[name]; !ok {
		lrc.serverLoads[name] = &rpcLoadData{}
	}
	rld := lrc.serverLoads[name]
	rld.add(val)
}

// CallDropped records a call dropped for the category.
func (lr *testLoadReporter) CallDropped(category string) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.drops[category]++
}

// stats returns and resets all loads reported for a cluster and service,
// except inProgress rpc counts.
//
// It returns nil if the store doesn't contain any (new) data.
func (lr *testLoadReporter) stats() *loadData {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	sd := newLoadData(lr.cluster, lr.service)
	for category, val := range lr.drops {
		if val == 0 {
			continue
		}
		if category != "" {
			// Skip drops without category. They are counted in total_drops, but
			// not in per category. One example is drops by circuit breaking.
			sd.drops[category] = val
		}
		sd.totalDrops += val
		lr.drops[category] = 0 // clear drops for next report
	}
	for locality, countData := range lr.localityRPCCount {
		if countData.succeeded == 0 && countData.errored == 0 && countData.inProgress == 0 && countData.issued == 0 {
			continue
		}

		ld := localityData{
			requestStats: requestData{
				succeeded:  countData.succeeded,
				errored:    countData.errored,
				inProgress: countData.inProgress,
				issued:     countData.issued,
			},
			loadStats: make(map[string]serverLoadData),
		}
		// clear localityRPCCount for next report
		countData.succeeded = 0
		countData.errored = 0
		countData.inProgress = 0
		countData.issued = 0
		for key, rld := range countData.serverLoads {
			s, c := rld.loadAndClear() // get and clear serverLoads for next report
			if c == 0 {
				continue
			}
			ld.loadStats[key] = serverLoadData{sum: s, count: c}
		}
		sd.localityStats[locality] = ld
	}
	if sd.totalDrops == 0 && len(sd.drops) == 0 && len(sd.localityStats) == 0 {
		return nil
	}
	return sd
}

// loadData contains all load data reported to the LoadStore since the most recent
// call to stats().
type loadData struct {
	// cluster is the name of the cluster this data is for.
	cluster string
	// service is the name of the EDS service this data is for.
	service string
	// totalDrops is the total number of dropped requests.
	totalDrops uint64
	// drops is the number of dropped requests per category.
	drops map[string]uint64
	// localityStats contains load reports per locality.
	localityStats map[clients.Locality]localityData
}

// localityData contains load data for a single locality.
type localityData struct {
	// requestStats contains counts of requests made to the locality.
	requestStats requestData
	// loadStats contains server load data for requests made to the locality,
	// indexed by the load type.
	loadStats map[string]serverLoadData
}

// requestData contains request counts.
type requestData struct {
	// succeeded is the number of succeeded requests.
	succeeded uint64
	// errored is the number of requests which ran into errors.
	errored uint64
	// inProgress is the number of requests in flight.
	inProgress uint64
	// issued is the total number requests that were sent.
	issued uint64
}

// serverLoadData contains server load data.
type serverLoadData struct {
	// count is the number of load reports.
	count uint64
	// sum is the total value of all load reports.
	sum float64
}

func newLoadData(cluster, service string) *loadData {
	return &loadData{
		cluster:       cluster,
		service:       service,
		drops:         make(map[string]uint64),
		localityStats: make(map[clients.Locality]localityData),
	}
}

type rpcCountData struct {
	succeeded   uint64
	errored     uint64
	inProgress  uint64
	issued      uint64
	serverLoads map[string]*rpcLoadData
}

type rpcLoadData struct {
	sum   float64
	count uint64
}

func (rld *rpcLoadData) add(v float64) {
	rld.sum += v
	rld.count++
}

func (rld *rpcLoadData) loadAndClear() (s float64, c uint64) {
	s, rld.sum = rld.sum, 0
	c, rld.count = rld.count, 0
	return s, c
}

func init() {
	NewRandomWRR = testutils.NewTestWRR
}

var sortDataSlice = cmp.Transformer("SortDataSlice", func(in []*loadData) []*loadData {
	out := append([]*loadData(nil), in...) // Copy input to avoid mutating it
	sort.Slice(out,
		func(i, j int) bool {
			if out[i].cluster < out[j].cluster {
				return true
			}
			if out[i].cluster == out[j].cluster {
				return out[i].service < out[j].service
			}
			return false
		},
	)
	return out
})

func verifyLoadStoreData(wantStoreData, gotStoreData *loadData) error {
	if diff := cmp.Diff(wantStoreData, gotStoreData, cmpOpts); diff != "" {
		return fmt.Errorf("store.stats() returned unexpected diff (-want +got):\n%s", diff)
	}
	return nil
}

// TestDropByCategory verifies that the balancer correctly drops the picks, and
// that the drops are reported.
func (s) TestDropByCategory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	defer xdsclient.ClearCounterForTesting(testClusterName, testServiceName)
	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	const (
		dropReason      = "test-dropping-category"
		dropNumerator   = 1
		dropDenominator = 2
	)
	testLRSServerConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:             testClusterName,
			EDSServiceName:      testServiceName,
			LoadReportingServer: testLRSServerConfig,
			DropCategories: []DropConfig{{
				Category:           dropReason,
				RequestsPerMillion: million * dropNumerator / dropDenominator,
			}},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	got, err := xdsC.WaitForReportLoad(ctx)
	if err != nil {
		t.Fatalf("xdsClient.ReportLoad failed with error: %v", err)
	}
	if got.Server != testLRSServerConfig {
		t.Fatalf("xdsClient.ReportLoad called with {%q}: want {%q}", got.Server, testLRSServerConfig)
	}

	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	// This should get the connecting picker.
	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatal(err.Error())
	}

	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	// Test pick with one backend.

	testClusterLoadReporter := &testLoadReporter{cluster: testClusterName, service: testServiceName, drops: make(map[string]uint64), localityRPCCount: make(map[clients.Locality]*rpcCountData)}

	const rpcCount = 24
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		// Override the loadStore in the picker with testClusterLoadReporter.
		picker := p.(*picker)
		originalLoadStore := picker.loadStore
		picker.loadStore = testClusterLoadReporter
		defer func() { picker.loadStore = originalLoadStore }()

		for i := 0; i < rpcCount; i++ {
			gotSCSt, err := p.Pick(balancer.PickInfo{})
			// Even RPCs are dropped.
			if i%2 == 0 {
				if err == nil || !strings.Contains(err.Error(), "dropped") {
					return fmt.Errorf("pick.Pick, got %v, %v, want error RPC dropped", gotSCSt, err)
				}
				continue
			}
			if err != nil || gotSCSt.SubConn != sc1 {
				return fmt.Errorf("picker.Pick, got %v, %v, want SubConn=%v", gotSCSt, err, sc1)
			}
			if gotSCSt.Done == nil {
				continue
			}
			// Fail 1/4th of the requests that are not dropped.
			if i%8 == 1 {
				gotSCSt.Done(balancer.DoneInfo{Err: fmt.Errorf("test error")})
			} else {
				gotSCSt.Done(balancer.DoneInfo{})
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err.Error())
	}

	// Dump load data from the store and compare with expected counts.
	const dropCount = rpcCount * dropNumerator / dropDenominator
	wantStatsData0 := &loadData{
		cluster:    testClusterName,
		service:    testServiceName,
		totalDrops: dropCount,
		drops:      map[string]uint64{dropReason: dropCount},
		localityStats: map[clients.Locality]localityData{
			{}: {requestStats: requestData{
				succeeded: (rpcCount - dropCount) * 3 / 4,
				errored:   (rpcCount - dropCount) / 4,
				issued:    rpcCount - dropCount,
			}},
		},
	}

	gotStatsData0 := testClusterLoadReporter.stats()
	if err := verifyLoadStoreData(wantStatsData0, gotStatsData0); err != nil {
		t.Fatal(err)
	}

	// Send an update with new drop configs.
	const (
		dropReason2      = "test-dropping-category-2"
		dropNumerator2   = 1
		dropDenominator2 = 4
	)
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:             testClusterName,
			EDSServiceName:      testServiceName,
			LoadReportingServer: testLRSServerConfig,
			DropCategories: []DropConfig{{
				Category:           dropReason2,
				RequestsPerMillion: million * dropNumerator2 / dropDenominator2,
			}},
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		// Override the loadStore in the picker with testClusterLoadReporter.
		picker := p.(*picker)
		originalLoadStore := picker.loadStore
		picker.loadStore = testClusterLoadReporter
		defer func() { picker.loadStore = originalLoadStore }()
		for i := 0; i < rpcCount; i++ {
			gotSCSt, err := p.Pick(balancer.PickInfo{})
			// Even RPCs are dropped.
			if i%4 == 0 {
				if err == nil || !strings.Contains(err.Error(), "dropped") {
					return fmt.Errorf("pick.Pick, got %v, %v, want error RPC dropped", gotSCSt, err)
				}
				continue
			}
			if err != nil || gotSCSt.SubConn != sc1 {
				return fmt.Errorf("picker.Pick, got %v, %v, want SubConn=%v", gotSCSt, err, sc1)
			}
			if gotSCSt.Done != nil {
				gotSCSt.Done(balancer.DoneInfo{})
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err.Error())
	}

	const dropCount2 = rpcCount * dropNumerator2 / dropDenominator2
	wantStatsData1 := &loadData{
		cluster:    testClusterName,
		service:    testServiceName,
		totalDrops: dropCount2,
		drops:      map[string]uint64{dropReason2: dropCount2},
		localityStats: map[clients.Locality]localityData{
			{}: {requestStats: requestData{
				succeeded: rpcCount - dropCount2,
				issued:    rpcCount - dropCount2,
			}},
		},
	}

	gotStatsData1 := testClusterLoadReporter.stats()
	if err := verifyLoadStoreData(wantStatsData1, gotStatsData1); err != nil {
		t.Fatal(err)
	}
}

// TestDropCircuitBreaking verifies that the balancer correctly drops the picks
// due to circuit breaking, and that the drops are reported.
func (s) TestDropCircuitBreaking(t *testing.T) {
	defer xdsclient.ClearCounterForTesting(testClusterName, testServiceName)
	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	var maxRequest uint32 = 50
	testLRSServerConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:               testClusterName,
			EDSServiceName:        testServiceName,
			LoadReportingServer:   testLRSServerConfig,
			MaxConcurrentRequests: &maxRequest,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	got, err := xdsC.WaitForReportLoad(ctx)
	if err != nil {
		t.Fatalf("xdsClient.ReportLoad failed with error: %v", err)
	}
	if got.Server != testLRSServerConfig {
		t.Fatalf("xdsClient.ReportLoad called with {%q}: want {%q}", got.Server, testLRSServerConfig)
	}

	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	// This should get the connecting picker.
	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatal(err.Error())
	}

	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	// Test pick with one backend.
	testClusterLoadReporter := &testLoadReporter{cluster: testClusterName, service: testServiceName, drops: make(map[string]uint64), localityRPCCount: make(map[clients.Locality]*rpcCountData)}
	const rpcCount = 100
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		dones := []func(){}
		// Override the loadStore in the picker with testClusterLoadReporter.
		picker := p.(*picker)
		originalLoadStore := picker.loadStore
		picker.loadStore = testClusterLoadReporter
		defer func() { picker.loadStore = originalLoadStore }()

		for i := 0; i < rpcCount; i++ {
			gotSCSt, err := p.Pick(balancer.PickInfo{})
			if i < 50 && err != nil {
				return fmt.Errorf("The first 50%% picks should be non-drops, got error %v", err)
			} else if i > 50 && err == nil {
				return fmt.Errorf("The second 50%% picks should be drops, got error <nil>")
			}
			dones = append(dones, func() {
				if gotSCSt.Done != nil {
					gotSCSt.Done(balancer.DoneInfo{})
				}
			})
		}
		for _, done := range dones {
			done()
		}

		dones = []func(){}
		// Pick without drops.
		for i := 0; i < 50; i++ {
			gotSCSt, err := p.Pick(balancer.PickInfo{})
			if err != nil {
				t.Errorf("The third 50%% picks should be non-drops, got error %v", err)
			}
			dones = append(dones, func() {
				if gotSCSt.Done != nil {
					// Fail these requests to test error counts in the load
					// report.
					gotSCSt.Done(balancer.DoneInfo{Err: fmt.Errorf("test error")})
				}
			})
		}
		for _, done := range dones {
			done()
		}

		return nil
	}); err != nil {
		t.Fatal(err.Error())
	}

	// Dump load data from the store and compare with expected counts.
	wantStatsData0 := &loadData{
		cluster:    testClusterName,
		service:    testServiceName,
		totalDrops: uint64(maxRequest),
		localityStats: map[clients.Locality]localityData{
			{}: {requestStats: requestData{
				succeeded: uint64(rpcCount - maxRequest),
				errored:   50,
				issued:    uint64(rpcCount - maxRequest + 50),
			}},
		},
	}

	gotStatsData0 := testClusterLoadReporter.stats()
	if err := verifyLoadStoreData(wantStatsData0, gotStatsData0); err != nil {
		t.Fatal(err)
	}
}

// TestPickerUpdateAfterClose covers the case where a child policy sends a
// picker update after the cluster_impl policy is closed. Because picker updates
// are handled in the run() goroutine, which exits before Close() returns, we
// expect the above picker update to be dropped.
func (s) TestPickerUpdateAfterClose(t *testing.T) {
	defer xdsclient.ClearCounterForTesting(testClusterName, testServiceName)
	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})

	// Create a stub balancer which waits for the cluster_impl policy to be
	// closed before sending a picker update (upon receipt of a subConn state
	// change).
	closeCh := make(chan struct{})
	const childPolicyName = "stubBalancer-TestPickerUpdateAfterClose"
	stub.Register(childPolicyName, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			// Create a subConn which will be used later on to test the race
			// between StateListener() and Close().
			sc, err := bd.ClientConn.NewSubConn(ccs.ResolverState.Addresses, balancer.NewSubConnOptions{
				StateListener: func(balancer.SubConnState) {
					go func() {
						// Wait for Close() to be called on the parent policy before
						// sending the picker update.
						<-closeCh
						bd.ClientConn.UpdateState(balancer.State{
							Picker: base.NewErrPicker(errors.New("dummy error picker")),
						})
					}()
				},
			})
			if err != nil {
				return err
			}
			sc.Connect()
			return nil
		},
	})

	var maxRequest uint32 = 50
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:               testClusterName,
			EDSServiceName:        testServiceName,
			MaxConcurrentRequests: &maxRequest,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: childPolicyName,
			},
		},
	}); err != nil {
		b.Close()
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	// Send a subConn state change to trigger a picker update. The stub balancer
	// that we use as the child policy will not send a picker update until the
	// parent policy is closed.
	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	b.Close()
	close(closeCh)

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("unexpected picker update after balancer is closed")
	case <-time.After(defaultShortTestTimeout):
	}
}

// TestClusterNameInAddressAttributes covers the case that cluster name is
// attached to the subconn address attributes.
func (s) TestClusterNameInAddressAttributes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	defer xdsclient.ClearCounterForTesting(testClusterName, testServiceName)
	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:        testClusterName,
			EDSServiceName: testServiceName,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	// This should get the connecting picker.
	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatal(err.Error())
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendEndpoints[0].Addresses[0].Addr; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	cn, ok := xdsinternal.GetXDSHandshakeClusterName(addrs1[0].Attributes)
	if !ok || cn != testClusterName {
		t.Fatalf("sc is created with addr with cluster name %v, %v, want cluster name %v", cn, ok, testClusterName)
	}

	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	// Test pick with one backend.
	if err := cc.WaitForRoundRobinPicker(ctx, sc1); err != nil {
		t.Fatal(err.Error())
	}

	const testClusterName2 = "test-cluster-2"
	var addr2 = resolver.Address{Addr: "2.2.2.2"}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{addr2}}}}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:        testClusterName2,
			EDSServiceName: testServiceName,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, addr2.Addr; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	// New addresses should have the new cluster name.
	cn2, ok := xdsinternal.GetXDSHandshakeClusterName(addrs2[0].Attributes)
	if !ok || cn2 != testClusterName2 {
		t.Fatalf("sc is created with addr with cluster name %v, %v, want cluster name %v", cn2, ok, testClusterName2)
	}
}

// TestReResolution verifies that when a SubConn turns transient failure,
// re-resolution is triggered.
func (s) TestReResolution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	defer xdsclient.ClearCounterForTesting(testClusterName, testServiceName)
	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:        testClusterName,
			EDSServiceName: testServiceName,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	// This should get the connecting picker.
	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatal(err.Error())
	}

	sc1.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   errors.New("test error"),
	})
	// This should get the transient failure picker.
	if err := cc.WaitForErrPicker(ctx); err != nil {
		t.Fatal(err.Error())
	}

	// The transient failure should trigger a re-resolution.
	select {
	case <-cc.ResolveNowCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timeout waiting for ResolveNow()")
	}

	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	// Test pick with one backend.
	if err := cc.WaitForRoundRobinPicker(ctx, sc1); err != nil {
		t.Fatal(err.Error())
	}

	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	// This should get the transient failure picker.
	if err := cc.WaitForErrPicker(ctx); err != nil {
		t.Fatal(err.Error())
	}

	// The transient failure should trigger a re-resolution.
	select {
	case <-cc.ResolveNowCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timeout waiting for ResolveNow()")
	}
}

func (s) TestLoadReporting(t *testing.T) {
	var testLocality = clients.Locality{
		Region:  "test-region",
		Zone:    "test-zone",
		SubZone: "test-sub-zone",
	}

	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	endpoints := make([]resolver.Endpoint, len(testBackendEndpoints))
	for i, e := range testBackendEndpoints {
		endpoints[i] = xdsinternal.SetLocalityIDInEndpoint(e, testLocality)
		for j, a := range e.Addresses {
			endpoints[i].Addresses[j] = xdsinternal.SetLocalityID(a, testLocality)
		}
	}
	testLRSServerConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: endpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:             testClusterName,
			EDSServiceName:      testServiceName,
			LoadReportingServer: testLRSServerConfig,
			// Locality:                testLocality,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	got, err := xdsC.WaitForReportLoad(ctx)
	if err != nil {
		t.Fatalf("xdsClient.ReportLoad failed with error: %v", err)
	}
	if got.Server != testLRSServerConfig {
		t.Fatalf("xdsClient.ReportLoad called with {%q}: want {%q}", got.Server, testLRSServerConfig)
	}

	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	// This should get the connecting picker.
	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatal(err.Error())
	}

	scs := balancer.SubConnState{ConnectivityState: connectivity.Ready}
	sca := internal.SetConnectedAddress.(func(*balancer.SubConnState, resolver.Address))
	sca(&scs, endpoints[0].Addresses[0])
	sc1.UpdateState(scs)
	// Test pick with one backend.
	testClusterLoadReporter := &testLoadReporter{cluster: testClusterName, service: testServiceName, drops: make(map[string]uint64), localityRPCCount: make(map[clients.Locality]*rpcCountData)}
	const successCount = 5
	const errorCount = 5
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		// Override the loadStore in the picker with testClusterLoadReporter.
		picker := p.(*picker)
		originalLoadStore := picker.loadStore
		picker.loadStore = testClusterLoadReporter
		defer func() { picker.loadStore = originalLoadStore }()
		for i := 0; i < successCount; i++ {
			gotSCSt, err := p.Pick(balancer.PickInfo{})
			if gotSCSt.SubConn != sc1 {
				return fmt.Errorf("picker.Pick, got %v, %v, want SubConn=%v", gotSCSt, err, sc1)
			}
			lr := &v3orcapb.OrcaLoadReport{
				NamedMetrics: map[string]float64{testNamedMetricsKey1: 3.14, testNamedMetricsKey2: 2.718},
			}
			gotSCSt.Done(balancer.DoneInfo{ServerLoad: lr})
		}
		for i := 0; i < errorCount; i++ {
			gotSCSt, err := p.Pick(balancer.PickInfo{})
			if gotSCSt.SubConn != sc1 {
				return fmt.Errorf("picker.Pick, got %v, %v, want SubConn=%v", gotSCSt, err, sc1)
			}
			gotSCSt.Done(balancer.DoneInfo{Err: fmt.Errorf("error")})
		}
		return nil
	}); err != nil {
		t.Fatal(err.Error())
	}

	// Dump load data from the store and compare with expected counts.
	sd := testClusterLoadReporter.stats()
	if sd == nil {
		t.Fatalf("loads for cluster %v not found in store", testClusterName)
	}
	if sd.cluster != testClusterName || sd.service != testServiceName {
		t.Fatalf("got unexpected load for %q, %q, want %q, %q", sd.cluster, sd.service, testClusterName, testServiceName)
	}
	localityData, ok := sd.localityStats[testLocality]
	if !ok {
		t.Fatalf("loads for %v not found in store", testLocality)
	}
	reqStats := localityData.requestStats
	if reqStats.succeeded != successCount {
		t.Errorf("got succeeded %v, want %v", reqStats.succeeded, successCount)
	}
	if reqStats.errored != errorCount {
		t.Errorf("got errord %v, want %v", reqStats.errored, errorCount)
	}
	if reqStats.inProgress != 0 {
		t.Errorf("got inProgress %v, want %v", reqStats.inProgress, 0)
	}
	wantLoadStats := map[string]serverLoadData{
		testNamedMetricsKey1: {count: 5, sum: 15.7},  // aggregation of 5 * 3.14 = 15.7
		testNamedMetricsKey2: {count: 5, sum: 13.59}, // aggregation of 5 * 2.718 = 13.59
	}
	if diff := cmp.Diff(wantLoadStats, localityData.loadStats, toleranceCmpOpt); diff != "" {
		t.Errorf("localityData.LoadStats returned unexpected diff (-want +got):\n%s", diff)
	}
	b.Close()
	if err := xdsC.WaitForCancelReportLoad(ctx); err != nil {
		t.Fatalf("unexpected error waiting form load report to be canceled: %v", err)
	}
}

// TestUpdateLRSServer covers the cases
// - the init config specifies "" as the LRS server
// - config modifies LRS server to a different string
// - config sets LRS server to nil to stop load reporting
func (s) TestUpdateLRSServer(t *testing.T) {
	var testLocality = clients.Locality{
		Region:  "test-region",
		Zone:    "test-zone",
		SubZone: "test-sub-zone",
	}

	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	endpoints := make([]resolver.Endpoint, len(testBackendEndpoints))
	for i, e := range testBackendEndpoints {
		endpoints[i] = xdsinternal.SetLocalityIDInEndpoint(e, testLocality)
	}
	testLRSServerConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: endpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:             testClusterName,
			EDSServiceName:      testServiceName,
			LoadReportingServer: testLRSServerConfig,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	got, err := xdsC.WaitForReportLoad(ctx)
	if err != nil {
		t.Fatalf("xdsClient.ReportLoad failed with error: %v", err)
	}
	if got.Server != testLRSServerConfig {
		t.Fatalf("xdsClient.ReportLoad called with {%q}: want {%q}", got.Server, testLRSServerConfig)
	}

	testLRSServerConfig2, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector-another.googleapis.com:443",
		ChannelCreds: []bootstrap.ChannelCreds{{Type: "google_default"}},
	})
	if err != nil {
		t.Fatalf("Failed to create LRS server config for testing: %v", err)
	}

	// Update LRS server to a different name.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: endpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:             testClusterName,
			EDSServiceName:      testServiceName,
			LoadReportingServer: testLRSServerConfig2,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}
	if err := xdsC.WaitForCancelReportLoad(ctx); err != nil {
		t.Fatalf("unexpected error waiting form load report to be canceled: %v", err)
	}
	got2, err2 := xdsC.WaitForReportLoad(ctx)
	if err2 != nil {
		t.Fatalf("xdsClient.ReportLoad failed with error: %v", err2)
	}
	if got2.Server != testLRSServerConfig2 {
		t.Fatalf("xdsClient.ReportLoad called with {%q}: want {%q}", got2.Server, testLRSServerConfig2)
	}

	// Update LRS server to nil, to disable LRS.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: endpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:        testClusterName,
			EDSServiceName: testServiceName,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}
	if err := xdsC.WaitForCancelReportLoad(ctx); err != nil {
		t.Fatalf("unexpected error waiting form load report to be canceled: %v", err)
	}

	shortCtx, shortCancel := context.WithTimeout(context.Background(), defaultShortTestTimeout)
	defer shortCancel()
	if s, err := xdsC.WaitForReportLoad(shortCtx); err != context.DeadlineExceeded {
		t.Fatalf("unexpected load report to server: %q", s)
	}
}

// Test verifies that child policies was updated on receipt of
// configuration update.
func (s) TestChildPolicyUpdatedOnConfigUpdate(t *testing.T) {
	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Keep track of which child policy was updated
	updatedChildPolicy := ""

	// Create stub balancers to track config updates
	const (
		childPolicyName1 = "stubBalancer1"
		childPolicyName2 = "stubBalancer2"
	)

	stub.Register(childPolicyName1, stub.BalancerFuncs{
		UpdateClientConnState: func(_ *stub.BalancerData, _ balancer.ClientConnState) error {
			updatedChildPolicy = childPolicyName1
			return nil
		},
	})

	stub.Register(childPolicyName2, stub.BalancerFuncs{
		UpdateClientConnState: func(_ *stub.BalancerData, _ balancer.ClientConnState) error {
			updatedChildPolicy = childPolicyName2
			return nil
		},
	})

	// Initial config update with childPolicyName1
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster: testClusterName,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: childPolicyName1,
			},
		},
	}); err != nil {
		t.Fatalf("Error updating the config: %v", err)
	}

	if updatedChildPolicy != childPolicyName1 {
		t.Fatal("Child policy 1 was not updated on initial configuration update.")
	}

	// Second config update with childPolicyName2
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster: testClusterName,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: childPolicyName2,
			},
		},
	}); err != nil {
		t.Fatalf("Error updating the config: %v", err)
	}

	if updatedChildPolicy != childPolicyName2 {
		t.Fatal("Child policy 2 was not updated after child policy name change.")
	}
}

// Test verifies that config update fails if child policy config
// failed to parse.
func (s) TestFailedToParseChildPolicyConfig(t *testing.T) {
	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Create a stub balancer which fails to ParseConfig.
	const parseConfigError = "failed to parse config"
	const childPolicyName = "stubBalancer-FailedToParseChildPolicyConfig"
	stub.Register(childPolicyName, stub.BalancerFuncs{
		ParseConfig: func(_ json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return nil, errors.New(parseConfigError)
		},
	})

	err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster: testClusterName,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: childPolicyName,
			},
		},
	})

	if err == nil || !strings.Contains(err.Error(), parseConfigError) {
		t.Fatalf("Got error: %v, want error: %s", err, parseConfigError)
	}
}

// Test verify that the case picker is updated synchronously on receipt of
// configuration update.
func (s) TestPickerUpdatedSynchronouslyOnConfigUpdate(t *testing.T) {
	// Override the pickerUpdateHook to be notified that picker was updated.
	pickerUpdated := make(chan struct{}, 1)
	origNewPickerUpdated := pickerUpdateHook
	pickerUpdateHook = func() {
		pickerUpdated <- struct{}{}
	}
	defer func() { pickerUpdateHook = origNewPickerUpdated }()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Override the clientConnUpdateHook to ensure client conn was updated.
	clientConnUpdateDone := make(chan struct{}, 1)
	origClientConnUpdateHook := clientConnUpdateHook
	clientConnUpdateHook = func() {
		// Verify that picker was updated before the completion of
		// client conn update.
		select {
		case <-pickerUpdated:
		case <-ctx.Done():
			t.Fatal("Client conn update completed before picker update.")
		}
		clientConnUpdateDone <- struct{}{}
	}
	defer func() { clientConnUpdateHook = origClientConnUpdateHook }()

	defer xdsclient.ClearCounterForTesting(testClusterName, testServiceName)
	xdsC := fakeclient.NewClient()

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Create a stub balancer which waits for the cluster_impl policy to be
	// closed before sending a picker update (upon receipt of a resolver
	// update).
	stub.Register(t.Name(), stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			bd.ClientConn.UpdateState(balancer.State{
				Picker: base.NewErrPicker(errors.New("dummy error picker")),
			})
			return nil
		},
	})

	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{Endpoints: testBackendEndpoints}, xdsC),
		BalancerConfig: &LBConfig{
			Cluster:        testClusterName,
			EDSServiceName: testServiceName,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: t.Name(),
			},
		},
	}); err != nil {
		t.Fatalf("Unexpected error from UpdateClientConnState: %v", err)
	}

	select {
	case <-clientConnUpdateDone:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for client conn update to be completed.")
	}
}
