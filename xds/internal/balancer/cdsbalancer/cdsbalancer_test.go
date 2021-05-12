// +build go1.12

/*
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
 */

package cdsbalancer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/edsbalancer"
	"google.golang.org/grpc/xds/internal/client"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const (
	clusterName             = "cluster1"
	serviceName             = "service1"
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// cdsWatchInfo wraps the update and the error sent in a CDS watch callback.
type cdsWatchInfo struct {
	update xdsclient.ClusterUpdate
	err    error
}

// invokeWatchCb invokes the CDS watch callback registered by the cdsBalancer
// and waits for appropriate state to be pushed to the provided edsBalancer.
func invokeWatchCbAndWait(ctx context.Context, xdsC *fakeclient.Client, cdsW cdsWatchInfo, wantCCS balancer.ClientConnState, edsB *testEDSBalancer) error {
	xdsC.InvokeWatchClusterCallback(cdsW.update, cdsW.err)
	if cdsW.err != nil {
		return edsB.waitForResolverError(ctx, cdsW.err)
	}
	return edsB.waitForClientConnUpdate(ctx, wantCCS)
}

// testEDSBalancer is a fake edsBalancer used to verify different actions from
// the cdsBalancer. It contains a bunch of channels to signal different events
// to the test.
type testEDSBalancer struct {
	// ccsCh is a channel used to signal the receipt of a ClientConn update.
	ccsCh *testutils.Channel
	// scStateCh is a channel used to signal the receipt of a SubConn update.
	scStateCh *testutils.Channel
	// resolverErrCh is a channel used to signal a resolver error.
	resolverErrCh *testutils.Channel
	// closeCh is a channel used to signal the closing of this balancer.
	closeCh *testutils.Channel
	// parentCC is the balancer.ClientConn passed to this test balancer as part
	// of the Build() call.
	parentCC balancer.ClientConn
}

type subConnWithState struct {
	sc    balancer.SubConn
	state balancer.SubConnState
}

func newTestEDSBalancer() *testEDSBalancer {
	return &testEDSBalancer{
		ccsCh:         testutils.NewChannel(),
		scStateCh:     testutils.NewChannel(),
		resolverErrCh: testutils.NewChannel(),
		closeCh:       testutils.NewChannel(),
	}
}

func (tb *testEDSBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	tb.ccsCh.Send(ccs)
	return nil
}

func (tb *testEDSBalancer) ResolverError(err error) {
	tb.resolverErrCh.Send(err)
}

func (tb *testEDSBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	tb.scStateCh.Send(subConnWithState{sc: sc, state: state})
}

func (tb *testEDSBalancer) Close() {
	tb.closeCh.Send(struct{}{})
}

// waitForClientConnUpdate verifies if the testEDSBalancer receives the
// provided ClientConnState within a reasonable amount of time.
func (tb *testEDSBalancer) waitForClientConnUpdate(ctx context.Context, wantCCS balancer.ClientConnState) error {
	ccs, err := tb.ccsCh.Receive(ctx)
	if err != nil {
		return err
	}
	gotCCS := ccs.(balancer.ClientConnState)
	if !cmp.Equal(gotCCS, wantCCS, cmpopts.IgnoreUnexported(attributes.Attributes{})) {
		return fmt.Errorf("received ClientConnState: %+v, want %+v", gotCCS, wantCCS)
	}
	return nil
}

// waitForSubConnUpdate verifies if the testEDSBalancer receives the provided
// SubConn update before the context expires.
func (tb *testEDSBalancer) waitForSubConnUpdate(ctx context.Context, wantSCS subConnWithState) error {
	scs, err := tb.scStateCh.Receive(ctx)
	if err != nil {
		return err
	}
	gotSCS := scs.(subConnWithState)
	if !cmp.Equal(gotSCS, wantSCS, cmp.AllowUnexported(subConnWithState{})) {
		return fmt.Errorf("received SubConnState: %+v, want %+v", gotSCS, wantSCS)
	}
	return nil
}

// waitForResolverError verifies if the testEDSBalancer receives the provided
// resolver error before the context expires.
func (tb *testEDSBalancer) waitForResolverError(ctx context.Context, wantErr error) error {
	gotErr, err := tb.resolverErrCh.Receive(ctx)
	if err != nil {
		return err
	}
	if gotErr != wantErr {
		return fmt.Errorf("received resolver error: %v, want %v", gotErr, wantErr)
	}
	return nil
}

// waitForClose verifies that the edsBalancer is closed before the context
// expires.
func (tb *testEDSBalancer) waitForClose(ctx context.Context) error {
	if _, err := tb.closeCh.Receive(ctx); err != nil {
		return err
	}
	return nil
}

// cdsCCS is a helper function to construct a good update passed from the
// xdsResolver to the cdsBalancer.
func cdsCCS(cluster string) balancer.ClientConnState {
	const cdsLBConfig = `{
      "loadBalancingConfig":[
        {
          "cds_experimental":{
            "Cluster": "%s"
          }
        }
      ]
    }`
	jsonSC := fmt.Sprintf(cdsLBConfig, cluster)
	return balancer.ClientConnState{
		ResolverState: resolver.State{
			ServiceConfig: internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(jsonSC),
		},
		BalancerConfig: &lbConfig{ClusterName: clusterName},
	}
}

// edsCCS is a helper function to construct a good update passed from the
// cdsBalancer to the edsBalancer.
func edsCCS(service string, countMax *uint32, enableLRS bool) balancer.ClientConnState {
	lbCfg := &edsbalancer.EDSConfig{
		EDSServiceName:        service,
		MaxConcurrentRequests: countMax,
	}
	if enableLRS {
		lbCfg.LrsLoadReportingServerName = new(string)
	}
	return balancer.ClientConnState{
		BalancerConfig: lbCfg,
	}
}

// setup creates a cdsBalancer and an edsBalancer (and overrides the
// newEDSBalancer function to return it), and also returns a cleanup function.
func setup(t *testing.T) (*fakeclient.Client, *cdsBalancer, *testEDSBalancer, *xdstestutils.TestClientConn, func()) {
	t.Helper()

	xdsC := fakeclient.NewClient()
	oldNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) { return xdsC, nil }

	builder := balancer.Get(cdsName)
	if builder == nil {
		t.Fatalf("balancer.Get(%q) returned nil", cdsName)
	}
	tcc := xdstestutils.NewTestClientConn(t)
	cdsB := builder.Build(tcc, balancer.BuildOptions{})

	edsB := newTestEDSBalancer()
	oldEDSBalancerBuilder := newEDSBalancer
	newEDSBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		edsB.parentCC = cc
		return edsB, nil
	}

	return xdsC, cdsB.(*cdsBalancer), edsB, tcc, func() {
		newEDSBalancer = oldEDSBalancerBuilder
		newXDSClient = oldNewXDSClient
	}
}

// setupWithWatch does everything that setup does, and also pushes a ClientConn
// update to the cdsBalancer and waits for a CDS watch call to be registered.
func setupWithWatch(t *testing.T) (*fakeclient.Client, *cdsBalancer, *testEDSBalancer, *xdstestutils.TestClientConn, func()) {
	t.Helper()

	xdsC, cdsB, edsB, tcc, cancel := setup(t)
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName)); err != nil {
		t.Fatalf("cdsBalancer.UpdateClientConnState failed with error: %v", err)
	}

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := xdsC.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != clusterName {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, clusterName)
	}
	return xdsC, cdsB, edsB, tcc, cancel
}

// TestUpdateClientConnState invokes the UpdateClientConnState method on the
// cdsBalancer with different inputs and verifies that the CDS watch API on the
// provided xdsClient is invoked appropriately.
func (s) TestUpdateClientConnState(t *testing.T) {
	tests := []struct {
		name        string
		ccs         balancer.ClientConnState
		wantErr     error
		wantCluster string
	}{
		{
			name:    "bad-lbCfg-type",
			ccs:     balancer.ClientConnState{BalancerConfig: nil},
			wantErr: balancer.ErrBadResolverState,
		},
		{
			name:    "empty-cluster-in-lbCfg",
			ccs:     balancer.ClientConnState{BalancerConfig: &lbConfig{ClusterName: ""}},
			wantErr: balancer.ErrBadResolverState,
		},
		{
			name:        "happy-good-case",
			ccs:         cdsCCS(clusterName),
			wantCluster: clusterName,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			xdsC, cdsB, _, _, cancel := setup(t)
			defer func() {
				cancel()
				cdsB.Close()
			}()

			if err := cdsB.UpdateClientConnState(test.ccs); err != test.wantErr {
				t.Fatalf("cdsBalancer.UpdateClientConnState failed with error: %v", err)
			}
			if test.wantErr != nil {
				// When we wanted an error and got it, we should return early.
				return
			}
			ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer ctxCancel()
			gotCluster, err := xdsC.WaitForWatchCluster(ctx)
			if err != nil {
				t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
			}
			if gotCluster != test.wantCluster {
				t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, test.wantCluster)
			}
		})
	}
}

// TestUpdateClientConnStateWithSameState verifies that a ClientConnState
// update with the same cluster and xdsClient does not cause the cdsBalancer to
// create a new watch.
func (s) TestUpdateClientConnStateWithSameState(t *testing.T) {
	xdsC, cdsB, _, _, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// This is the same clientConn update sent in setupWithWatch().
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName)); err != nil {
		t.Fatalf("cdsBalancer.UpdateClientConnState failed with error: %v", err)
	}
	// The above update should not result in a new watch being registered.
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ctxCancel()
	if _, err := xdsC.WaitForWatchCluster(ctx); err != context.DeadlineExceeded {
		t.Fatalf("waiting for WatchCluster() should have timed out, but returned error: %v", err)
	}
}

// TestHandleClusterUpdate invokes the registered CDS watch callback with
// different updates and verifies that the expect ClientConnState is propagated
// to the edsBalancer.
func (s) TestHandleClusterUpdate(t *testing.T) {
	xdsC, cdsB, edsB, _, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	tests := []struct {
		name      string
		cdsUpdate xdsclient.ClusterUpdate
		updateErr error
		wantCCS   balancer.ClientConnState
	}{
		{
			name:      "happy-case-with-lrs",
			cdsUpdate: xdsclient.ClusterUpdate{ServiceName: serviceName, EnableLRS: true},
			wantCCS:   edsCCS(serviceName, nil, true),
		},
		{
			name:      "happy-case-without-lrs",
			cdsUpdate: xdsclient.ClusterUpdate{ServiceName: serviceName},
			wantCCS:   edsCCS(serviceName, nil, false),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer ctxCancel()
			if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{test.cdsUpdate, test.updateErr}, test.wantCCS, edsB); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestHandleClusterUpdateError covers the cases that an error is returned from
// the watcher.
func (s) TestHandleClusterUpdateError(t *testing.T) {
	// This creates a CDS balancer, pushes a ClientConnState update with a fake
	// xdsClient, and makes sure that the CDS balancer registers a watch on the
	// provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// A watch was registered above, but the watch callback has not been invoked
	// yet. This means that the watch handler on the CDS balancer has not been
	// invoked yet, and therefore no EDS balancer has been built so far. A
	// resolver error at this point should result in the CDS balancer returning
	// an error picker.
	watcherErr := errors.New("cdsBalancer watcher error")
	xdsC.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{}, watcherErr)

	// Since the error being pushed here is not a resource-not-found-error, the
	// registered watch should not be cancelled.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cluster watch cancelled for a non-resource-not-found-error")
	}
	// The CDS balancer has not yet created an EDS balancer. So, this resolver
	// error should not be forwarded to our fake EDS balancer.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsB.waitForResolverError(sCtx, watcherErr); err != context.DeadlineExceeded {
		t.Fatal("eds balancer shouldn't get error (shouldn't be built yet)")
	}

	// Make sure the CDS balancer reports an error picker.
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout when waiting for an error picker")
	case picker := <-tcc.NewPickerCh:
		if _, perr := picker.Pick(balancer.PickInfo{}); perr == nil {
			t.Fatalf("CDS balancer returned a picker which is not an error picker")
		}
	}

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newEDSBalancer function as part of test setup.
	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false)
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Again push a non-resource-not-found-error through the watcher callback.
	xdsC.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{}, watcherErr)
	// Make sure the registered watch is not cancelled.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cluster watch cancelled for a non-resource-not-found-error")
	}
	// Make sure the error is forwarded to the EDS balancer.
	if err := edsB.waitForResolverError(ctx, watcherErr); err != nil {
		t.Fatalf("Watch callback error is not forwarded to EDS balancer")
	}

	// Push a resource-not-found-error this time around.
	resourceErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "cdsBalancer resource not found error")
	xdsC.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{}, resourceErr)
	// Make sure that the watch is not cancelled. This error indicates that the
	// request cluster resource is not found. We should continue to watch it.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cluster watch cancelled for a resource-not-found-error")
	}
	// Make sure the error is forwarded to the EDS balancer.
	if err := edsB.waitForResolverError(ctx, resourceErr); err != nil {
		t.Fatalf("Watch callback error is not forwarded to EDS balancer")
	}
}

// TestResolverError verifies the ResolverError() method in the CDS balancer.
func (s) TestResolverError(t *testing.T) {
	// This creates a CDS balancer, pushes a ClientConnState update with a fake
	// xdsClient, and makes sure that the CDS balancer registers a watch on the
	// provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// A watch was registered above, but the watch callback has not been invoked
	// yet. This means that the watch handler on the CDS balancer has not been
	// invoked yet, and therefore no EDS balancer has been built so far. A
	// resolver error at this point should result in the CDS balancer returning
	// an error picker.
	resolverErr := errors.New("cdsBalancer resolver error")
	cdsB.ResolverError(resolverErr)

	// Since the error being pushed here is not a resource-not-found-error, the
	// registered watch should not be cancelled.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cluster watch cancelled for a non-resource-not-found-error")
	}
	// The CDS balancer has not yet created an EDS balancer. So, this resolver
	// error should not be forwarded to our fake EDS balancer.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsB.waitForResolverError(sCtx, resolverErr); err != context.DeadlineExceeded {
		t.Fatal("eds balancer shouldn't get error (shouldn't be built yet)")
	}
	// Make sure the CDS balancer reports an error picker.
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout when waiting for an error picker")
	case picker := <-tcc.NewPickerCh:
		if _, perr := picker.Pick(balancer.PickInfo{}); perr == nil {
			t.Fatalf("CDS balancer returned a picker which is not an error picker")
		}
	}

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newEDSBalancer function as part of test setup.
	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false)
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Again push a non-resource-not-found-error.
	cdsB.ResolverError(resolverErr)
	// Make sure the registered watch is not cancelled.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cluster watch cancelled for a non-resource-not-found-error")
	}
	// Make sure the error is forwarded to the EDS balancer.
	if err := edsB.waitForResolverError(ctx, resolverErr); err != nil {
		t.Fatalf("ResolverError() not forwarded to EDS balancer")
	}

	// Push a resource-not-found-error this time around.
	resourceErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "cdsBalancer resource not found error")
	cdsB.ResolverError(resourceErr)
	// Make sure the registered watch is cancelled.
	if err := xdsC.WaitForCancelClusterWatch(ctx); err != nil {
		t.Fatalf("want watch to be canceled, watchForCancel failed: %v", err)
	}
	// Make sure the error is forwarded to the EDS balancer.
	if err := edsB.waitForResolverError(ctx, resourceErr); err != nil {
		t.Fatalf("eds balancer should get resource-not-found error, waitForError failed: %v", err)
	}
}

// TestUpdateSubConnState verifies the UpdateSubConnState() method in the CDS
// balancer.
func (s) TestUpdateSubConnState(t *testing.T) {
	// This creates a CDS balancer, pushes a ClientConnState update with a fake
	// xdsClient, and makes sure that the CDS balancer registers a watch on the
	// provided xdsClient.
	xdsC, cdsB, edsB, _, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newEDSBalancer function as part of test setup.
	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Push a subConn state change to the CDS balancer.
	var sc balancer.SubConn
	state := balancer.SubConnState{ConnectivityState: connectivity.Ready}
	cdsB.UpdateSubConnState(sc, state)

	// Make sure that the update is forwarded to the EDS balancer.
	if err := edsB.waitForSubConnUpdate(ctx, subConnWithState{sc: sc, state: state}); err != nil {
		t.Fatal(err)
	}
}

// TestCircuitBreaking verifies that the CDS balancer correctly updates a
// service's counter on watch updates.
func (s) TestCircuitBreaking(t *testing.T) {
	// This creates a CDS balancer, pushes a ClientConnState update with a fake
	// xdsClient, and makes sure that the CDS balancer registers a watch on the
	// provided xdsClient.
	xdsC, cdsB, edsB, _, cancel := setupWithXDSCreds(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will update
	// the service's counter with the new max requests.
	var maxRequests uint32 = 1
	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName, MaxRequests: &maxRequests}
	wantCCS := edsCCS(serviceName, &maxRequests, false)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Since the counter's max requests was set to 1, the first request should
	// succeed and the second should fail.
	counter := client.GetServiceRequestsCounter(serviceName)
	if err := counter.StartRequest(maxRequests); err != nil {
		t.Fatal(err)
	}
	if err := counter.StartRequest(maxRequests); err == nil {
		t.Fatal("unexpected success on start request over max")
	}
	counter.EndRequest()
}

// TestClose verifies the Close() method in the the CDS balancer.
func (s) TestClose(t *testing.T) {
	// This creates a CDS balancer, pushes a ClientConnState update with a fake
	// xdsClient, and makes sure that the CDS balancer registers a watch on the
	// provided xdsClient.
	xdsC, cdsB, edsB, _, cancel := setupWithWatch(t)
	defer cancel()

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newEDSBalancer function as part of test setup.
	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Close the CDS balancer.
	cdsB.Close()

	// Make sure that the cluster watch registered by the CDS balancer is
	// cancelled.
	if err := xdsC.WaitForCancelClusterWatch(ctx); err != nil {
		t.Fatal(err)
	}

	// Make sure that a cluster update is not acted upon.
	xdsC.InvokeWatchClusterCallback(cdsUpdate, nil)
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsB.waitForClientConnUpdate(sCtx, balancer.ClientConnState{}); err != context.DeadlineExceeded {
		t.Fatalf("ClusterUpdate after close forwaded to EDS balancer")
	}

	// Make sure that the underlying EDS balancer is closed.
	if err := edsB.waitForClose(ctx); err != nil {
		t.Fatal(err)
	}

	// Make sure that the UpdateClientConnState() method on the CDS balancer
	// returns error.
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName)); err != errBalancerClosed {
		t.Fatalf("UpdateClientConnState() after close returned %v, want %v", err, errBalancerClosed)
	}

	// Make sure that the UpdateSubConnState() method on the CDS balancer does
	// not forward the update to the EDS balancer.
	cdsB.UpdateSubConnState(&xdstestutils.TestSubConn{}, balancer.SubConnState{})
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsB.waitForSubConnUpdate(sCtx, subConnWithState{}); err != context.DeadlineExceeded {
		t.Fatal("UpdateSubConnState() forwarded to EDS balancer after Close()")
	}

	// Make sure that the ResolverErr() method on the CDS balancer does not
	// forward the update to the EDS balancer.
	rErr := errors.New("cdsBalancer resolver error")
	cdsB.ResolverError(rErr)
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsB.waitForResolverError(sCtx, rErr); err != context.DeadlineExceeded {
		t.Fatal("ResolverError() forwarded to EDS balancer after Close()")
	}
}

// TestParseConfig verifies the ParseConfig() method in the CDS balancer.
func (s) TestParseConfig(t *testing.T) {
	bb := balancer.Get(cdsName)
	if bb == nil {
		t.Fatalf("balancer.Get(%q) returned nil", cdsName)
	}
	parser, ok := bb.(balancer.ConfigParser)
	if !ok {
		t.Fatalf("balancer %q does not implement the ConfigParser interface", cdsName)
	}

	tests := []struct {
		name    string
		input   json.RawMessage
		wantCfg serviceconfig.LoadBalancingConfig
		wantErr bool
	}{
		{
			name:    "good-lb-config",
			input:   json.RawMessage(`{"Cluster": "cluster1"}`),
			wantCfg: &lbConfig{ClusterName: "cluster1"},
		},
		{
			name:    "unknown-fields-in-lb-config",
			input:   json.RawMessage(`{"Unknown": "foobar"}`),
			wantCfg: &lbConfig{ClusterName: ""},
		},
		{
			name:    "empty-lb-config",
			input:   json.RawMessage(""),
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCfg, gotErr := parser.ParseConfig(test.input)
			if (gotErr != nil) != test.wantErr {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", string(test.input), gotErr, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if !cmp.Equal(gotCfg, test.wantCfg) {
				t.Fatalf("ParseConfig(%v) = %v, want %v", string(test.input), gotCfg, test.wantCfg)
			}
		})
	}
}
