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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/client/load"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const (
	defaultTestTimeout = 1 * time.Second
	testClusterName    = "test-cluster"
	testServiceName    = "test-eds-service"
	testLRSServerName  = "test-lrs-name"
)

var (
	testBackendAddrs = []resolver.Address{
		{Addr: "1.1.1.1:1"},
	}
)

func init() {
	newRandomWRR = testutils.NewTestWRR
}

// TestDrop verifies that the balancer correctly drops the picks, and that
// the drops are reported.
func TestDrop(t *testing.T) {
	xdsC := fakeclient.NewClient()
	oldNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) { return xdsC, nil }
	defer func() { newXDSClient = oldNewXDSClient }()

	builder := balancer.Get(clusterImplName)
	cc := testutils.NewTestClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	const (
		dropReason      = "test-dropping-category"
		dropNumerator   = 1
		dropDenominator = 2
	)
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: testBackendAddrs,
		},
		BalancerConfig: &lbConfig{
			Cluster:                    testClusterName,
			EDSServiceName:             testServiceName,
			LRSLoadReportingServerName: newString(testLRSServerName),
			DropCategories: []dropCategory{{
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

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	got, err := xdsC.WaitForReportLoad(ctx)
	if err != nil {
		t.Fatalf("xdsClient.ReportLoad failed with error: %v", err)
	}
	if got.Server != testLRSServerName {
		t.Fatalf("xdsClient.ReportLoad called with {%q}: want {%q}", got.Server, testLRSServerName)
	}

	sc1 := <-cc.NewSubConnCh
	b.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	// This should get the connecting picker.
	p0 := <-cc.NewPickerCh
	for i := 0; i < 10; i++ {
		_, err := p0.Pick(balancer.PickInfo{})
		if err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("picker.Pick, got _,%v, want Err=%v", err, balancer.ErrNoSubConnAvailable)
		}
	}

	b.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	// Test pick with one backend.
	p1 := <-cc.NewPickerCh
	const rpcCount = 20
	for i := 0; i < rpcCount; i++ {
		gotSCSt, err := p1.Pick(balancer.PickInfo{})
		// Even RPCs are dropped.
		if i%2 == 0 {
			if err == nil || !strings.Contains(err.Error(), "dropped") {
				t.Fatalf("pick.Pick, got %v, %v, want error RPC dropped", gotSCSt, err)
			}
			continue
		}
		if err != nil || !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, %v, want SubConn=%v", gotSCSt, err, sc1)
		}
		if gotSCSt.Done != nil {
			gotSCSt.Done(balancer.DoneInfo{})
		}
	}

	// Dump load data from the store and compare with expected counts.
	loadStore := xdsC.LoadStore()
	if loadStore == nil {
		t.Fatal("loadStore is nil in xdsClient")
	}
	const dropCount = rpcCount * dropNumerator / dropDenominator
	wantStatsData0 := []*load.Data{{
		Cluster:    testClusterName,
		Service:    testServiceName,
		TotalDrops: dropCount,
		Drops:      map[string]uint64{dropReason: dropCount},
	}}

	var cmpOpts = cmp.Options{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(load.Data{}, "ReportInterval"),
	}

	gotStatsData0 := loadStore.Stats([]string{testClusterName})
	if diff := cmp.Diff(gotStatsData0, wantStatsData0, cmpOpts); diff != "" {
		t.Fatalf("got unexpected reports, diff (-got, +want): %v", diff)
	}

	// Send an update with new drop configs.
	const (
		dropReason2      = "test-dropping-category-2"
		dropNumerator2   = 1
		dropDenominator2 = 4
	)
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: testBackendAddrs,
		},
		BalancerConfig: &lbConfig{
			Cluster:                    testClusterName,
			EDSServiceName:             testServiceName,
			LRSLoadReportingServerName: newString(testLRSServerName),
			DropCategories: []dropCategory{{
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

	p2 := <-cc.NewPickerCh
	for i := 0; i < rpcCount; i++ {
		gotSCSt, err := p2.Pick(balancer.PickInfo{})
		// Even RPCs are dropped.
		if i%4 == 0 {
			if err == nil || !strings.Contains(err.Error(), "dropped") {
				t.Fatalf("pick.Pick, got %v, %v, want error RPC dropped", gotSCSt, err)
			}
			continue
		}
		if err != nil || !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, %v, want SubConn=%v", gotSCSt, err, sc1)
		}
		if gotSCSt.Done != nil {
			gotSCSt.Done(balancer.DoneInfo{})
		}
	}

	const dropCount2 = rpcCount * dropNumerator2 / dropDenominator2
	wantStatsData1 := []*load.Data{{
		Cluster:    testClusterName,
		Service:    testServiceName,
		TotalDrops: dropCount2,
		Drops:      map[string]uint64{dropReason2: dropCount2},
	}}

	gotStatsData1 := loadStore.Stats([]string{testClusterName})
	if diff := cmp.Diff(gotStatsData1, wantStatsData1, cmpOpts); diff != "" {
		t.Fatalf("got unexpected reports, diff (-got, +want): %v", diff)
	}
}
