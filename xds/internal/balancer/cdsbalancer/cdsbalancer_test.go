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
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	xdsinternal "google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/edsbalancer"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const (
	clusterName        = "cluster1"
	serviceName        = "service1"
	defaultTestTimeout = 2 * time.Second
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testClientConn struct {
	balancer.ClientConn

	newPickerCh *testutils.Channel // The last picker updated.
}

func newTestClientConn() *testClientConn {
	return &testClientConn{
		newPickerCh: testutils.NewChannelWithSize(1),
	}
}

func (tcc *testClientConn) UpdateState(bs balancer.State) {
	tcc.newPickerCh.Replace(bs)
}

// cdsWatchInfo wraps the update and the error sent in a CDS watch callback.
type cdsWatchInfo struct {
	update xdsclient.ClusterUpdate
	err    error
}

// invokeWatchCb invokes the CDS watch callback registered by the cdsBalancer
// and waits for appropriate state to be pushed to the provided edsBalancer.
func invokeWatchCbAndWait(xdsC *fakeclient.Client, cdsW cdsWatchInfo, wantCCS balancer.ClientConnState, edsB *testEDSBalancer) error {
	xdsC.InvokeWatchClusterCallback(cdsW.update, cdsW.err)
	if cdsW.err != nil {
		return edsB.waitForResolverError(cdsW.err)
	}
	return edsB.waitForClientConnUpdate(wantCCS)
}

// testEDSBalancer is a fake edsBalancer used to verify different actions from
// the cdsBalancer. It contains a bunch of channels to signal different events
// to the test.
type testEDSBalancer struct {
	// ccsCh is a channel used to signal the receipt of a ClientConn update.
	ccsCh chan balancer.ClientConnState
	// scStateCh is a channel used to signal the receipt of a SubConn update.
	scStateCh chan subConnWithState
	// resolverErrCh is a channel used to signal a resolver error.
	resolverErrCh chan error
	// closeCh is a channel used to signal the closing of this balancer.
	closeCh chan struct{}
}

type subConnWithState struct {
	sc    balancer.SubConn
	state balancer.SubConnState
}

func newTestEDSBalancer() *testEDSBalancer {
	return &testEDSBalancer{
		ccsCh:         make(chan balancer.ClientConnState, 1),
		scStateCh:     make(chan subConnWithState, 1),
		resolverErrCh: make(chan error, 1),
		closeCh:       make(chan struct{}, 1),
	}
}

func (tb *testEDSBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	tb.ccsCh <- ccs
	return nil
}

func (tb *testEDSBalancer) ResolverError(err error) {
	tb.resolverErrCh <- err
}

func (tb *testEDSBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	tb.scStateCh <- subConnWithState{sc: sc, state: state}
}

func (tb *testEDSBalancer) Close() {
	tb.closeCh <- struct{}{}
}

// waitForClientConnUpdate verifies if the testEDSBalancer receives the
// provided ClientConnState within a reasonable amount of time.
func (tb *testEDSBalancer) waitForClientConnUpdate(wantCCS balancer.ClientConnState) error {
	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		return errors.New("Timeout when expecting ClientConn update on EDS balancer")
	case gotCCS := <-tb.ccsCh:
		timer.Stop()
		if !cmp.Equal(gotCCS, wantCCS, cmpopts.IgnoreUnexported(attributes.Attributes{})) {
			return fmt.Errorf("received ClientConnState: %+v, want %+v", gotCCS, wantCCS)
		}
		return nil
	}
}

// waitForSubConnUpdate verifies if the testEDSBalancer receives the provided
// SubConn update within a reasonable amount of time.
func (tb *testEDSBalancer) waitForSubConnUpdate(wantSCS subConnWithState) error {
	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		return errors.New("Timeout when expecting SubConn update on EDS balancer")
	case gotSCS := <-tb.scStateCh:
		timer.Stop()
		if !cmp.Equal(gotSCS, wantSCS, cmp.AllowUnexported(subConnWithState{})) {
			return fmt.Errorf("received SubConnState: %+v, want %+v", gotSCS, wantSCS)
		}
		return nil
	}
}

// waitForResolverError verifies if the testEDSBalancer receives the
// provided resolver error within a reasonable amount of time.
func (tb *testEDSBalancer) waitForResolverError(wantErr error) error {
	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		return errors.New("Timeout when expecting a resolver error")
	case gotErr := <-tb.resolverErrCh:
		timer.Stop()
		if gotErr != wantErr {
			return fmt.Errorf("received resolver error: %v, want %v", gotErr, wantErr)
		}
		return nil
	}
}

// waitForClose verifies that the edsBalancer is closed with a reasonable
// amount of time.
func (tb *testEDSBalancer) waitForClose() error {
	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		return errors.New("Timeout when expecting a close")
	case <-tb.closeCh:
		timer.Stop()
		return nil
	}
}

// cdsCCS is a helper function to construct a good update passed from the
// xdsResolver to the cdsBalancer.
func cdsCCS(cluster string, xdsClient interface{}) balancer.ClientConnState {
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
			Attributes:    attributes.New(xdsinternal.XDSClientID, xdsClient),
		},
		BalancerConfig: &lbConfig{ClusterName: clusterName},
	}
}

// edsCCS is a helper function to construct a good update passed from the
// cdsBalancer to the edsBalancer.
func edsCCS(service string, enableLRS bool, xdsClient interface{}) balancer.ClientConnState {
	lbCfg := &edsbalancer.EDSConfig{EDSServiceName: service}
	if enableLRS {
		lbCfg.LrsLoadReportingServerName = new(string)
	}
	return balancer.ClientConnState{
		ResolverState:  resolver.State{Attributes: attributes.New(xdsinternal.XDSClientID, xdsClient)},
		BalancerConfig: lbCfg,
	}
}

// setup creates a cdsBalancer and an edsBalancer (and overrides the
// newEDSBalancer function to return it), and also returns a cleanup function.
func setup() (*cdsBalancer, *testEDSBalancer, *testClientConn, func()) {
	builder := cdsBB{}
	tcc := newTestClientConn()
	// cdsB := builder.Build(tcc, balancer.BuildOptions{}).(balancer.V2Balancer)
	cdsB := builder.Build(tcc, balancer.BuildOptions{})

	edsB := newTestEDSBalancer()
	oldEDSBalancerBuilder := newEDSBalancer
	newEDSBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		return edsB, nil
	}

	return cdsB.(*cdsBalancer), edsB, tcc, func() {
		newEDSBalancer = oldEDSBalancerBuilder
	}
}

// setupWithWatch does everything that setup does, and also pushes a ClientConn
// update to the cdsBalancer and waits for a CDS watch call to be registered.
func setupWithWatch(t *testing.T) (*fakeclient.Client, *cdsBalancer, *testEDSBalancer, *testClientConn, func()) {
	t.Helper()

	xdsC := fakeclient.NewClient()
	cdsB, edsB, tcc, cancel := setup()
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName, xdsC)); err != nil {
		t.Fatalf("cdsBalancer.UpdateClientConnState failed with error: %v", err)
	}
	gotCluster, err := xdsC.WaitForWatchCluster()
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
			name: "no-xdsClient-in-attributes",
			ccs: balancer.ClientConnState{
				ResolverState: resolver.State{
					Attributes: attributes.New("key", "value"),
				},
				BalancerConfig: &lbConfig{ClusterName: clusterName},
			},
			wantErr: balancer.ErrBadResolverState,
		},
		{
			name: "bad-xdsClient-in-attributes",
			ccs: balancer.ClientConnState{
				ResolverState: resolver.State{
					Attributes: attributes.New(xdsinternal.XDSClientID, "value"),
				},
				BalancerConfig: &lbConfig{ClusterName: clusterName},
			},
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
			cdsB, _, _, cancel := setup()
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
			gotCluster, err := xdsC.WaitForWatchCluster()
			if err != nil {
				t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
			}
			if gotCluster != test.wantCluster {
				t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, test.wantCluster)
			}
		})
	}
}

// TestUpdateClientConnStateAfterClose invokes the UpdateClientConnState method
// on the cdsBalancer after close and verifies that it returns an error.
func (s) TestUpdateClientConnStateAfterClose(t *testing.T) {
	cdsB, _, _, cancel := setup()
	defer cancel()
	cdsB.Close()

	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName, fakeclient.NewClient())); err != errBalancerClosed {
		t.Fatalf("UpdateClientConnState() after close returned %v, want %v", err, errBalancerClosed)
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

	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName, xdsC)); err != nil {
		t.Fatalf("cdsBalancer.UpdateClientConnState failed with error: %v", err)
	}
	if _, err := xdsC.WaitForWatchCluster(); err != testutils.ErrRecvTimeout {
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
			wantCCS:   edsCCS(serviceName, true, xdsC),
		},
		{
			name:      "happy-case-without-lrs",
			cdsUpdate: xdsclient.ClusterUpdate{ServiceName: serviceName},
			wantCCS:   edsCCS(serviceName, false, xdsC),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := invokeWatchCbAndWait(xdsC, cdsWatchInfo{test.cdsUpdate, test.updateErr}, test.wantCCS, edsB); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestHandleClusterUpdateError covers the cases that an error is returned from
// the watcher.
//
// Includes error with and without a child eds balancer, and whether error is a
// resource-not-found error.
func (s) TestHandleClusterUpdateError(t *testing.T) {
	xdsC, cdsB, edsB, tcc, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// An error before eds balancer is built. Should result in an error picker.
	// And this is not a resource not found error, watch shouldn't be canceled.
	err1 := errors.New("cdsBalancer resolver error 1")
	xdsC.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{}, err1)
	if err := xdsC.WaitForCancelClusterWatch(); err == nil {
		t.Fatal("watch was canceled, want not canceled (timeout error)")
	}
	if err := edsB.waitForResolverError(err1); err == nil {
		t.Fatal("eds balancer shouldn't get error (shouldn't be built yet)")
	}
	state, err := tcc.newPickerCh.Receive()
	if err != nil {
		t.Fatalf("failed to get picker, expect an error picker")
	}
	picker := state.(balancer.State).Picker
	if _, perr := picker.Pick(balancer.PickInfo{}); perr == nil {
		t.Fatalf("want picker to always fail, got nil")
	}

	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName}
	wantCCS := edsCCS(serviceName, false, xdsC)
	if err := invokeWatchCbAndWait(xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// An error after eds balancer is build, eds should receive the error. This
	// is not a resource not found error, watch shouldn't be canceled
	err2 := errors.New("cdsBalancer resolver error 2")
	xdsC.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{}, err2)
	if err := xdsC.WaitForCancelClusterWatch(); err == nil {
		t.Fatal("watch was canceled, want not canceled (timeout error)")
	}
	if err := edsB.waitForResolverError(err2); err != nil {
		t.Fatalf("eds balancer should get error, waitForError failed: %v", err)
	}

	// A resource not found error. Watch should not be canceled because this
	// means CDS resource is removed, and eds should receive the error.
	resourceErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "cdsBalancer resource not found error")
	xdsC.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{}, resourceErr)
	if err := xdsC.WaitForCancelClusterWatch(); err == nil {
		t.Fatalf("want watch to be not canceled, watchForCancel should timeout")
	}
	if err := edsB.waitForResolverError(resourceErr); err != nil {
		t.Fatalf("eds balancer should get resource-not-found error, waitForError failed: %v", err)
	}
}

// TestResolverError verifies that resolvers errors (with type
// resource-not-found or others) are handled correctly.
func (s) TestResolverError(t *testing.T) {
	xdsC, cdsB, edsB, tcc, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// An error before eds balancer is built. Should result in an error picker.
	// Not a resource not found error, watch shouldn't be canceled.
	err1 := errors.New("cdsBalancer resolver error 1")
	cdsB.ResolverError(err1)
	if err := xdsC.WaitForCancelClusterWatch(); err == nil {
		t.Fatal("watch was canceled, want not canceled (timeout error)")
	}
	if err := edsB.waitForResolverError(err1); err == nil {
		t.Fatal("eds balancer shouldn't get error (shouldn't be built yet)")
	}
	state, err := tcc.newPickerCh.Receive()
	if err != nil {
		t.Fatalf("failed to get picker, expect an error picker")
	}
	picker := state.(balancer.State).Picker
	if _, perr := picker.Pick(balancer.PickInfo{}); perr == nil {
		t.Fatalf("want picker to always fail, got nil")
	}

	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName}
	wantCCS := edsCCS(serviceName, false, xdsC)
	if err := invokeWatchCbAndWait(xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Not a resource not found error, watch shouldn't be canceled, and eds
	// should receive the error.
	err2 := errors.New("cdsBalancer resolver error 2")
	cdsB.ResolverError(err2)
	if err := xdsC.WaitForCancelClusterWatch(); err == nil {
		t.Fatal("watch was canceled, want not canceled (timeout error)")
	}
	if err := edsB.waitForResolverError(err2); err != nil {
		t.Fatalf("eds balancer should get error, waitForError failed: %v", err)
	}

	// A resource not found error. Watch should be canceled, and eds should
	// receive the error.
	resourceErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "cdsBalancer resource not found error")
	cdsB.ResolverError(resourceErr)
	if err := xdsC.WaitForCancelClusterWatch(); err != nil {
		t.Fatalf("want watch to be canceled, watchForCancel failed: %v", err)
	}
	if err := edsB.waitForResolverError(resourceErr); err != nil {
		t.Fatalf("eds balancer should get resource-not-found error, waitForError failed: %v", err)
	}
}

// TestUpdateSubConnState pushes a SubConn update to the cdsBalancer and
// verifies that the update is propagated to the edsBalancer.
func (s) TestUpdateSubConnState(t *testing.T) {
	xdsC, cdsB, edsB, _, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName}
	wantCCS := edsCCS(serviceName, false, xdsC)
	if err := invokeWatchCbAndWait(xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	var sc balancer.SubConn
	state := balancer.SubConnState{ConnectivityState: connectivity.Ready}
	cdsB.UpdateSubConnState(sc, state)
	if err := edsB.waitForSubConnUpdate(subConnWithState{sc: sc, state: state}); err != nil {
		t.Fatal(err)
	}
}

// TestClose calls Close() on the cdsBalancer, and verifies that the underlying
// edsBalancer is also closed.
func (s) TestClose(t *testing.T) {
	xdsC, cdsB, edsB, _, cancel := setupWithWatch(t)
	defer cancel()

	cdsUpdate := xdsclient.ClusterUpdate{ServiceName: serviceName}
	wantCCS := edsCCS(serviceName, false, xdsC)
	if err := invokeWatchCbAndWait(xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	cdsB.Close()
	if err := xdsC.WaitForCancelClusterWatch(); err != nil {
		t.Fatal(err)
	}
	if err := edsB.waitForClose(); err != nil {
		t.Fatal(err)
	}
}

// TestParseConfig exercises the config parsing functionality in the cds
// balancer builder.
func (s) TestParseConfig(t *testing.T) {
	bb := cdsBB{}
	if gotName := bb.Name(); gotName != cdsName {
		t.Fatalf("cdsBB.Name() = %v, want %v", gotName, cdsName)
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
			wantCfg: &lbConfig{ClusterName: clusterName},
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
			gotCfg, gotErr := bb.ParseConfig(test.input)
			if (gotErr != nil) != test.wantErr {
				t.Fatalf("bb.ParseConfig(%v) = %v, wantErr %v", string(test.input), gotErr, test.wantErr)
			}
			if !test.wantErr {
				if !cmp.Equal(gotCfg, test.wantCfg) {
					t.Fatalf("bb.ParseConfig(%v) = %v, want %v", string(test.input), gotCfg, test.wantCfg)
				}
			}
		})
	}
}
