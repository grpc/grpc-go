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

package lrs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	xdsinternal "google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const defaultTestTimeout = 1 * time.Second

var (
	testBackendAddrs = []resolver.Address{
		{Addr: "1.1.1.1:1"},
	}
	testLocality = &xdsinternal.LocalityID{
		Region:  "test-region",
		Zone:    "test-zone",
		SubZone: "test-sub-zone",
	}
)

// TestLoadReporting verifies that the lrs balancer starts the loadReport
// stream when the lbConfig passed to it contains a valid value for the LRS
// server (empty string).
func TestLoadReporting(t *testing.T) {
	xdsC := fakeclient.NewClient()
	oldNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) { return xdsC, nil }
	defer func() { newXDSClient = oldNewXDSClient }()

	builder := balancer.Get(lrsBalancerName)
	cc := testutils.NewTestClientConn(t)
	lrsB := builder.Build(cc, balancer.BuildOptions{})
	defer lrsB.Close()

	if err := lrsB.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: testBackendAddrs,
		},
		BalancerConfig: &lbConfig{
			ClusterName:                testClusterName,
			EdsServiceName:             testServiceName,
			LrsLoadReportingServerName: testLRSServerName,
			Locality:                   testLocality,
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
	lrsB.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	lrsB.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with one backend.
	p1 := <-cc.NewPickerCh
	const successCount = 5
	for i := 0; i < successCount; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
		gotSCSt.Done(balancer.DoneInfo{})
	}
	const errorCount = 5
	for i := 0; i < errorCount; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
		gotSCSt.Done(balancer.DoneInfo{Err: fmt.Errorf("error")})
	}

	// Dump load data from the store and compare with expected counts.
	loadStore := xdsC.LoadStore()
	if loadStore == nil {
		t.Fatal("loadStore is nil in xdsClient")
	}
	sds := loadStore.Stats([]string{testClusterName})
	if len(sds) == 0 {
		t.Fatalf("loads for cluster %v not found in store", testClusterName)
	}
	sd := sds[0]
	if sd.Cluster != testClusterName || sd.Service != testServiceName {
		t.Fatalf("got unexpected load for %q, %q, want %q, %q", sd.Cluster, sd.Service, testClusterName, testServiceName)
	}
	testLocalityJSON, _ := testLocality.ToString()
	localityData, ok := sd.LocalityStats[testLocalityJSON]
	if !ok {
		t.Fatalf("loads for %v not found in store", testLocality)
	}
	reqStats := localityData.RequestStats
	if reqStats.Succeeded != successCount {
		t.Errorf("got succeeded %v, want %v", reqStats.Succeeded, successCount)
	}
	if reqStats.Errored != errorCount {
		t.Errorf("got errord %v, want %v", reqStats.Errored, errorCount)
	}
	if reqStats.InProgress != 0 {
		t.Errorf("got inProgress %v, want %v", reqStats.InProgress, 0)
	}
}
