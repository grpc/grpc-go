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
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/clusterresolver"
	"google.golang.org/grpc/xds/internal/balancer/outlierdetection"
	"google.golang.org/grpc/xds/internal/balancer/ringhash"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

const (
	clusterName             = "cluster1"
	serviceName             = "service1"
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

var (
	defaultTestAuthorityServerConfig = &bootstrap.ServerConfig{
		ServerURI: "self_server",
		CredsType: "self_creds",
	}
	noopODLBCfg = outlierdetection.LBConfig{
		Interval: 1<<63 - 1,
	}
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// cdsWatchInfo wraps the update and the error sent in a CDS watch callback.
type cdsWatchInfo struct {
	update xdsresource.ClusterUpdate
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
	closeCh    *testutils.Channel
	exitIdleCh *testutils.Channel
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
		exitIdleCh:    testutils.NewChannel(),
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

func (tb *testEDSBalancer) ExitIdle() {
	tb.exitIdleCh.Send(struct{}{})
}

// waitForClientConnUpdate verifies if the testEDSBalancer receives the
// provided ClientConnState within a reasonable amount of time.
func (tb *testEDSBalancer) waitForClientConnUpdate(ctx context.Context, wantCCS balancer.ClientConnState) error {
	ccs, err := tb.ccsCh.Receive(ctx)
	if err != nil {
		return err
	}
	gotCCS := ccs.(balancer.ClientConnState)
	if xdsclient.FromResolverState(gotCCS.ResolverState) == nil {
		return fmt.Errorf("want resolver state with XDSClient attached, got one without")
	}
	if diff := cmp.Diff(gotCCS, wantCCS, cmpopts.IgnoreFields(resolver.State{}, "Attributes")); diff != "" {
		return fmt.Errorf("received unexpected ClientConnState, diff (-got +want): %v", diff)
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
func cdsCCS(cluster string, xdsC xdsclient.XDSClient) balancer.ClientConnState {
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
		ResolverState: xdsclient.SetClient(resolver.State{
			ServiceConfig: internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC),
		}, xdsC),
		BalancerConfig: &lbConfig{ClusterName: clusterName},
	}
}

// edsCCS is a helper function to construct a good update passed from the
// cdsBalancer to the edsBalancer.
func edsCCS(service string, countMax *uint32, enableLRS bool, xdslbpolicy *internalserviceconfig.BalancerConfig, odConfig outlierdetection.LBConfig) balancer.ClientConnState {
	discoveryMechanism := clusterresolver.DiscoveryMechanism{
		Type:                  clusterresolver.DiscoveryMechanismTypeEDS,
		Cluster:               service,
		MaxConcurrentRequests: countMax,
		OutlierDetection:      odConfig,
	}
	if enableLRS {
		discoveryMechanism.LoadReportingServer = defaultTestAuthorityServerConfig
	}
	lbCfg := &clusterresolver.LBConfig{
		DiscoveryMechanisms: []clusterresolver.DiscoveryMechanism{discoveryMechanism},
		XDSLBPolicy:         xdslbpolicy,
	}

	return balancer.ClientConnState{
		BalancerConfig: lbCfg,
	}
}

// setup creates a cdsBalancer and an edsBalancer (and overrides the
// newChildBalancer function to return it), and also returns a cleanup function.
func setup(t *testing.T) (*fakeclient.Client, *cdsBalancer, *testEDSBalancer, *testutils.TestClientConn, func()) {
	t.Helper()
	xdsC := fakeclient.NewClient()
	builder := balancer.Get(cdsName)
	if builder == nil {
		t.Fatalf("balancer.Get(%q) returned nil", cdsName)
	}
	tcc := testutils.NewTestClientConn(t)
	cdsB := builder.Build(tcc, balancer.BuildOptions{})

	edsB := newTestEDSBalancer()
	oldEDSBalancerBuilder := newChildBalancer
	newChildBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		edsB.parentCC = cc
		return edsB, nil
	}

	return xdsC, cdsB.(*cdsBalancer), edsB, tcc, func() {
		newChildBalancer = oldEDSBalancerBuilder
		xdsC.Close()
	}
}

// setupWithWatch does everything that setup does, and also pushes a ClientConn
// update to the cdsBalancer and waits for a CDS watch call to be registered.
func setupWithWatch(t *testing.T) (*fakeclient.Client, *cdsBalancer, *testEDSBalancer, *testutils.TestClientConn, func()) {
	t.Helper()

	xdsC, cdsB, edsB, tcc, cancel := setup(t)
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName, xdsC)); err != nil {
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
	xdsC := fakeclient.NewClient()
	defer xdsC.Close()

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
			ccs:         cdsCCS(clusterName, xdsC),
			wantCluster: clusterName,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, cdsB, _, _, cancel := setup(t)
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
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName, xdsC)); err != nil {
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
	xdsC.SetBootstrapConfig(&bootstrap.Config{
		XDSServer: defaultTestAuthorityServerConfig,
	})
	defer func() {
		cancel()
		cdsB.Close()
	}()

	tests := []struct {
		name      string
		cdsUpdate xdsresource.ClusterUpdate
		updateErr error
		wantCCS   balancer.ClientConnState
	}{
		{
			name:      "happy-case-with-lrs",
			cdsUpdate: xdsresource.ClusterUpdate{ClusterName: serviceName, LRSServerConfig: xdsresource.ClusterLRSServerSelf},
			wantCCS:   edsCCS(serviceName, nil, true, nil, noopODLBCfg),
		},
		{
			name:      "happy-case-without-lrs",
			cdsUpdate: xdsresource.ClusterUpdate{ClusterName: serviceName},
			wantCCS:   edsCCS(serviceName, nil, false, nil, noopODLBCfg),
		},
		{
			name: "happy-case-with-ring-hash-lb-policy",
			cdsUpdate: xdsresource.ClusterUpdate{
				ClusterName: serviceName,
				LBPolicy:    &xdsresource.ClusterLBPolicyRingHash{MinimumRingSize: 10, MaximumRingSize: 100},
			},
			wantCCS: edsCCS(serviceName, nil, false, &internalserviceconfig.BalancerConfig{
				Name:   ringhash.Name,
				Config: &ringhash.LBConfig{MinRingSize: 10, MaxRingSize: 100},
			}, noopODLBCfg),
		},
		{
			name: "happy-case-outlier-detection",
			cdsUpdate: xdsresource.ClusterUpdate{ClusterName: serviceName, OutlierDetection: &xdsresource.OutlierDetection{
				Interval:                       10 * time.Second,
				BaseEjectionTime:               30 * time.Second,
				MaxEjectionTime:                300 * time.Second,
				MaxEjectionPercent:             10,
				SuccessRateStdevFactor:         1900,
				EnforcingSuccessRate:           100,
				SuccessRateMinimumHosts:        5,
				SuccessRateRequestVolume:       100,
				FailurePercentageThreshold:     85,
				EnforcingFailurePercentage:     5,
				FailurePercentageMinimumHosts:  5,
				FailurePercentageRequestVolume: 50,
			}},
			wantCCS: edsCCS(serviceName, nil, false, nil, outlierdetection.LBConfig{
				Interval:           10 * time.Second,
				BaseEjectionTime:   30 * time.Second,
				MaxEjectionTime:    300 * time.Second,
				MaxEjectionPercent: 10,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           1900,
					EnforcementPercentage: 100,
					MinimumHosts:          5,
					RequestVolume:         100,
				},
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{
					Threshold:             85,
					EnforcementPercentage: 5,
					MinimumHosts:          5,
					RequestVolume:         50,
				},
			}),
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
	xdsC.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{}, watcherErr)

	// Since the error being pushed here is not a resource-not-found-error, the
	// registered watch should not be cancelled.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
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
	// newChildBalancer function as part of test setup.
	cdsUpdate := xdsresource.ClusterUpdate{ClusterName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false, nil, noopODLBCfg)
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Again push a non-resource-not-found-error through the watcher callback.
	xdsC.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{}, watcherErr)
	// Make sure the registered watch is not cancelled.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cluster watch cancelled for a non-resource-not-found-error")
	}
	// Make sure the error is forwarded to the EDS balancer.
	if err := edsB.waitForResolverError(ctx, watcherErr); err != nil {
		t.Fatalf("Watch callback error is not forwarded to EDS balancer")
	}

	// Push a resource-not-found-error this time around.
	resourceErr := xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "cdsBalancer resource not found error")
	xdsC.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{}, resourceErr)
	// Make sure that the watch is not cancelled. This error indicates that the
	// request cluster resource is not found. We should continue to watch it.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
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
	if _, err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
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
	// newChildBalancer function as part of test setup.
	cdsUpdate := xdsresource.ClusterUpdate{ClusterName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false, nil, noopODLBCfg)
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Again push a non-resource-not-found-error.
	cdsB.ResolverError(resolverErr)
	// Make sure the registered watch is not cancelled.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cluster watch cancelled for a non-resource-not-found-error")
	}
	// Make sure the error is forwarded to the EDS balancer.
	if err := edsB.waitForResolverError(ctx, resolverErr); err != nil {
		t.Fatalf("ResolverError() not forwarded to EDS balancer")
	}

	// Push a resource-not-found-error this time around.
	resourceErr := xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "cdsBalancer resource not found error")
	cdsB.ResolverError(resourceErr)
	// Make sure the registered watch is cancelled.
	if _, err := xdsC.WaitForCancelClusterWatch(ctx); err != nil {
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
	// newChildBalancer function as part of test setup.
	cdsUpdate := xdsresource.ClusterUpdate{ClusterName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false, nil, noopODLBCfg)
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
	cdsUpdate := xdsresource.ClusterUpdate{ClusterName: clusterName, MaxRequests: &maxRequests}
	wantCCS := edsCCS(clusterName, &maxRequests, false, nil, noopODLBCfg)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Since the counter's max requests was set to 1, the first request should
	// succeed and the second should fail.
	counter := xdsclient.GetClusterRequestsCounter(clusterName, "")
	if err := counter.StartRequest(maxRequests); err != nil {
		t.Fatal(err)
	}
	if err := counter.StartRequest(maxRequests); err == nil {
		t.Fatal("unexpected success on start request over max")
	}
	counter.EndRequest()
}

// TestClose verifies the Close() method in the CDS balancer.
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
	// newChildBalancer function as part of test setup.
	cdsUpdate := xdsresource.ClusterUpdate{ClusterName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false, nil, noopODLBCfg)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Close the CDS balancer.
	cdsB.Close()

	// Make sure that the cluster watch registered by the CDS balancer is
	// cancelled.
	if _, err := xdsC.WaitForCancelClusterWatch(ctx); err != nil {
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
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName, xdsC)); err != errBalancerClosed {
		t.Fatalf("UpdateClientConnState() after close returned %v, want %v", err, errBalancerClosed)
	}

	// Make sure that the UpdateSubConnState() method on the CDS balancer does
	// not forward the update to the EDS balancer.
	cdsB.UpdateSubConnState(&testutils.TestSubConn{}, balancer.SubConnState{})
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

func (s) TestExitIdle(t *testing.T) {
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
	// newChildBalancer function as part of test setup.
	cdsUpdate := xdsresource.ClusterUpdate{ClusterName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false, nil, noopODLBCfg)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Call ExitIdle on the CDS balancer.
	cdsB.ExitIdle()

	edsB.exitIdleCh.Receive(ctx)
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

func (s) TestOutlierDetectionToConfig(t *testing.T) {
	tests := []struct {
		name        string
		od          *xdsresource.OutlierDetection
		odLBCfgWant outlierdetection.LBConfig
	}{
		// "if the outlier_detection field is not set in the Cluster resource,
		// a "no-op" outlier_detection config will be generated in the
		// corresponding DiscoveryMechanism config, with interval set to the
		// maximum possible value and all other fields unset." - A50
		{
			name:        "no-op-outlier-detection-config",
			od:          nil,
			odLBCfgWant: noopODLBCfg,
		},
		// "if the enforcing_success_rate field is set to 0, the config
		// success_rate_ejection field will be null and all success_rate_*
		// fields will be ignored." - A50
		{
			name: "enforcing-success-rate-zero",
			od: &xdsresource.OutlierDetection{
				Interval:                       10 * time.Second,
				BaseEjectionTime:               30 * time.Second,
				MaxEjectionTime:                300 * time.Second,
				MaxEjectionPercent:             10,
				SuccessRateStdevFactor:         1900,
				EnforcingSuccessRate:           0,
				SuccessRateMinimumHosts:        5,
				SuccessRateRequestVolume:       100,
				FailurePercentageThreshold:     85,
				EnforcingFailurePercentage:     5,
				FailurePercentageMinimumHosts:  5,
				FailurePercentageRequestVolume: 50,
			},
			odLBCfgWant: outlierdetection.LBConfig{
				Interval:            10 * time.Second,
				BaseEjectionTime:    30 * time.Second,
				MaxEjectionTime:     300 * time.Second,
				MaxEjectionPercent:  10,
				SuccessRateEjection: nil,
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{
					Threshold:             85,
					EnforcementPercentage: 5,
					MinimumHosts:          5,
					RequestVolume:         50,
				},
			},
		},
		// "If the enforcing_failure_percent field is set to 0 or null, the
		// config failure_percent_ejection field will be null and all
		// failure_percent_* fields will be ignored." - A50
		{
			name: "enforcing-failure-percentage-zero",
			od: &xdsresource.OutlierDetection{
				Interval:                       10 * time.Second,
				BaseEjectionTime:               30 * time.Second,
				MaxEjectionTime:                300 * time.Second,
				MaxEjectionPercent:             10,
				SuccessRateStdevFactor:         1900,
				EnforcingSuccessRate:           100,
				SuccessRateMinimumHosts:        5,
				SuccessRateRequestVolume:       100,
				FailurePercentageThreshold:     85,
				EnforcingFailurePercentage:     0,
				FailurePercentageMinimumHosts:  5,
				FailurePercentageRequestVolume: 50,
			},
			odLBCfgWant: outlierdetection.LBConfig{
				Interval:           10 * time.Second,
				BaseEjectionTime:   30 * time.Second,
				MaxEjectionTime:    300 * time.Second,
				MaxEjectionPercent: 10,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           1900,
					EnforcementPercentage: 100,
					MinimumHosts:          5,
					RequestVolume:         100,
				},
				FailurePercentageEjection: nil,
			},
		},
		{
			name: "normal-conversion",
			od: &xdsresource.OutlierDetection{
				Interval:                       10 * time.Second,
				BaseEjectionTime:               30 * time.Second,
				MaxEjectionTime:                300 * time.Second,
				MaxEjectionPercent:             10,
				SuccessRateStdevFactor:         1900,
				EnforcingSuccessRate:           100,
				SuccessRateMinimumHosts:        5,
				SuccessRateRequestVolume:       100,
				FailurePercentageThreshold:     85,
				EnforcingFailurePercentage:     5,
				FailurePercentageMinimumHosts:  5,
				FailurePercentageRequestVolume: 50,
			},
			odLBCfgWant: outlierdetection.LBConfig{
				Interval:           10 * time.Second,
				BaseEjectionTime:   30 * time.Second,
				MaxEjectionTime:    300 * time.Second,
				MaxEjectionPercent: 10,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           1900,
					EnforcementPercentage: 100,
					MinimumHosts:          5,
					RequestVolume:         100,
				},
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{
					Threshold:             85,
					EnforcementPercentage: 5,
					MinimumHosts:          5,
					RequestVolume:         50,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			odLBCfgGot := outlierDetectionToConfig(test.od)
			if diff := cmp.Diff(odLBCfgGot, test.odLBCfgWant); diff != "" {
				t.Fatalf("outlierDetectionToConfig(%v) (-want, +got):\n%s", test.od, diff)
			}
		})
	}
}
