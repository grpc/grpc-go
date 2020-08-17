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
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	xdsinternal "google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/testutils"
)

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

// This is a subset of testutils.fakeclient. Cannot use testutils.fakeclient
// because testutils imports package lrs.
//
// TODO: after refactoring xdsclient to support load reporting, the testutils
// package won't need to depend on lrs package for the store. And we can use the
// testutils for this.
type fakeXDSClient struct {
	loadReportCh chan *reportLoadArgs
}

func newFakeXDSClient() *fakeXDSClient {
	return &fakeXDSClient{
		loadReportCh: make(chan *reportLoadArgs, 10),
	}
}

// reportLoadArgs wraps the arguments passed to ReportLoad.
type reportLoadArgs struct {
	// server is the name of the server to which the load is reported.
	server string
	// cluster is the name of the cluster for which load is reported.
	cluster string
	// loadStore is the store where loads are stored.
	loadStore interface{}
}

// ReportLoad starts reporting load about clusterName to server.
func (xdsC *fakeXDSClient) ReportLoad(server string, clusterName string, loadStore Store) (cancel func()) {
	xdsC.loadReportCh <- &reportLoadArgs{server: server, cluster: clusterName, loadStore: loadStore}
	return func() {}
}

// waitForReportLoad waits for ReportLoad to be invoked on this client within a
// reasonable timeout, and returns the arguments passed to it.
func (xdsC *fakeXDSClient) waitForReportLoad() (*reportLoadArgs, error) {
	select {
	case <-time.After(time.Second):
		return nil, fmt.Errorf("timeout")
	case a := <-xdsC.loadReportCh:
		return a, nil
	}
}

// Close closes the xds client.
func (xdsC *fakeXDSClient) Close() {
}

// TestLoadReporting verifies that the lrs balancer starts the loadReport
// stream when the lbConfig passed to it contains a valid value for the LRS
// server (empty string).
func TestLoadReporting(t *testing.T) {
	builder := balancer.Get(lrsBalancerName)
	cc := testutils.NewTestClientConn(t)
	lrsB := builder.Build(cc, balancer.BuildOptions{})
	defer lrsB.Close()

	xdsC := newFakeXDSClient()
	if err := lrsB.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses:  testBackendAddrs,
			Attributes: attributes.New(xdsinternal.XDSClientID, xdsC),
		},
		BalancerConfig: &lbConfig{
			EdsServiceName:             testClusterName,
			LrsLoadReportingServerName: testLRSServerName,
			Locality:                   testLocality,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name: roundrobin.Name,
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from UpdateClientConnState: %v", err)
	}

	got, err := xdsC.waitForReportLoad()
	if err != nil {
		t.Fatalf("xdsClient.ReportLoad failed with error: %v", err)
	}
	if got.server != testLRSServerName || got.cluster != testClusterName {
		t.Fatalf("xdsClient.ReportLoad called with {%q, %q}: want {%q, %q}", got.server, got.cluster, testLRSServerName, testClusterName)
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

	loads := make(map[xdsinternal.LocalityID]*rpcCountData)

	got.loadStore.(*lrsStore).localityRPCCount.Range(
		func(key, value interface{}) bool {
			loads[key.(xdsinternal.LocalityID)] = value.(*rpcCountData)
			return true
		},
	)

	countData, ok := loads[*testLocality]
	if !ok {
		t.Fatalf("loads for %v not found in store", testLocality)
	}
	if *countData.succeeded != successCount {
		t.Errorf("got succeeded %v, want %v", *countData.succeeded, successCount)
	}
	if *countData.errored != errorCount {
		t.Errorf("got errord %v, want %v", *countData.errored, errorCount)
	}
	if *countData.inProgress != 0 {
		t.Errorf("got inProgress %v, want %v", *countData.inProgress, 0)
	}
}
