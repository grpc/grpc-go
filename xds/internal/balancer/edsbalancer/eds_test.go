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

package edsbalancer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/jsonpb"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpctest"
	scpb "google.golang.org/grpc/internal/proto/grpc_service_config"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/load"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"

	_ "google.golang.org/grpc/xds/internal/client/v2" // V2 client registration.
)

const (
	defaultTestTimeout      = 1 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
	testServiceName         = "test/foo"
	testEDSClusterName      = "test/service/eds"
)

var (
	// A non-empty endpoints update which is expected to be accepted by the EDS
	// LB policy.
	defaultEndpointsUpdate = xdsclient.EndpointsUpdate{
		Localities: []xdsclient.Locality{
			{
				Endpoints: []xdsclient.Endpoint{{Address: "endpoint1"}},
				ID:        internal.LocalityID{Zone: "zone"},
				Priority:  1,
				Weight:    100,
			},
		},
	}
)

func init() {
	balancer.Register(&edsBalancerBuilder{})
}

func subConnFromPicker(p balancer.Picker) func() balancer.SubConn {
	return func() balancer.SubConn {
		scst, _ := p.Pick(balancer.PickInfo{})
		return scst.SubConn
	}
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const testBalancerNameFooBar = "foo.bar"

func newNoopTestClientConn() *noopTestClientConn {
	return &noopTestClientConn{}
}

// noopTestClientConn is used in EDS balancer config update tests that only
// cover the config update handling, but not SubConn/load-balancing.
type noopTestClientConn struct {
	balancer.ClientConn
}

func (t *noopTestClientConn) NewSubConn([]resolver.Address, balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return nil, nil
}

func (noopTestClientConn) Target() string { return testServiceName }

type scStateChange struct {
	sc    balancer.SubConn
	state connectivity.State
}

type fakeEDSBalancer struct {
	cc                 balancer.ClientConn
	childPolicy        *testutils.Channel
	subconnStateChange *testutils.Channel
	edsUpdate          *testutils.Channel
	serviceName        *testutils.Channel
}

func (f *fakeEDSBalancer) handleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	f.subconnStateChange.Send(&scStateChange{sc: sc, state: state})
}

func (f *fakeEDSBalancer) handleChildPolicy(name string, config json.RawMessage) {
	f.childPolicy.Send(&loadBalancingConfig{Name: name, Config: config})
}

func (f *fakeEDSBalancer) handleEDSResponse(edsResp xdsclient.EndpointsUpdate) {
	f.edsUpdate.Send(edsResp)
}

func (f *fakeEDSBalancer) updateState(priority priorityType, s balancer.State) {}

func (f *fakeEDSBalancer) updateServiceRequestsCounter(serviceName string) {
	f.serviceName.Send(serviceName)
}

func (f *fakeEDSBalancer) close() {}

func (f *fakeEDSBalancer) waitForChildPolicy(ctx context.Context, wantPolicy *loadBalancingConfig) error {
	val, err := f.childPolicy.Receive(ctx)
	if err != nil {
		return err
	}
	gotPolicy := val.(*loadBalancingConfig)
	if !cmp.Equal(gotPolicy, wantPolicy) {
		return fmt.Errorf("got childPolicy %v, want %v", gotPolicy, wantPolicy)
	}
	return nil
}

func (f *fakeEDSBalancer) waitForSubConnStateChange(ctx context.Context, wantState *scStateChange) error {
	val, err := f.subconnStateChange.Receive(ctx)
	if err != nil {
		return err
	}
	gotState := val.(*scStateChange)
	if !cmp.Equal(gotState, wantState, cmp.AllowUnexported(scStateChange{})) {
		return fmt.Errorf("got subconnStateChange %v, want %v", gotState, wantState)
	}
	return nil
}

func (f *fakeEDSBalancer) waitForEDSResponse(ctx context.Context, wantUpdate xdsclient.EndpointsUpdate) error {
	val, err := f.edsUpdate.Receive(ctx)
	if err != nil {
		return err
	}
	gotUpdate := val.(xdsclient.EndpointsUpdate)
	if !reflect.DeepEqual(gotUpdate, wantUpdate) {
		return fmt.Errorf("got edsUpdate %+v, want %+v", gotUpdate, wantUpdate)
	}
	return nil
}

func (f *fakeEDSBalancer) waitForCounterUpdate(ctx context.Context, wantServiceName string) error {
	val, err := f.serviceName.Receive(ctx)
	if err != nil {
		return err
	}
	gotServiceName := val.(string)
	if gotServiceName != wantServiceName {
		return fmt.Errorf("got serviceName %v, want %v", gotServiceName, wantServiceName)
	}
	return nil
}

func newFakeEDSBalancer(cc balancer.ClientConn) edsBalancerImplInterface {
	return &fakeEDSBalancer{
		cc:                 cc,
		childPolicy:        testutils.NewChannelWithSize(10),
		subconnStateChange: testutils.NewChannelWithSize(10),
		edsUpdate:          testutils.NewChannelWithSize(10),
		serviceName:        testutils.NewChannelWithSize(10),
	}
}

type fakeSubConn struct{}

func (*fakeSubConn) UpdateAddresses([]resolver.Address) { panic("implement me") }
func (*fakeSubConn) Connect()                           { panic("implement me") }

// waitForNewEDSLB makes sure that a new edsLB is created by the top-level
// edsBalancer.
func waitForNewEDSLB(ctx context.Context, ch *testutils.Channel) (*fakeEDSBalancer, error) {
	val, err := ch.Receive(ctx)
	if err != nil {
		return nil, fmt.Errorf("error when waiting for a new edsLB: %v", err)
	}
	return val.(*fakeEDSBalancer), nil
}

// setup overrides the functions which are used to create the xdsClient and the
// edsLB, creates fake version of them and makes them available on the provided
// channels. The returned cancel function should be called by the test for
// cleanup.
func setup(edsLBCh *testutils.Channel) (*fakeclient.Client, func()) {
	xdsC := fakeclient.NewClientWithName(testBalancerNameFooBar)
	oldNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) { return xdsC, nil }

	origNewEDSBalancer := newEDSBalancer
	newEDSBalancer = func(cc balancer.ClientConn, enqueue func(priorityType, balancer.State), _ load.PerClusterReporter, logger *grpclog.PrefixLogger) edsBalancerImplInterface {
		edsLB := newFakeEDSBalancer(cc)
		defer func() { edsLBCh.Send(edsLB) }()
		return edsLB
	}
	return xdsC, func() {
		newEDSBalancer = origNewEDSBalancer
		newXDSClient = oldNewXDSClient
	}
}

const (
	fakeBalancerA = "fake_balancer_A"
	fakeBalancerB = "fake_balancer_B"
)

// Install two fake balancers for service config update tests.
//
// ParseConfig only accepts the json if the balancer specified is registered.
func init() {
	balancer.Register(&fakeBalancerBuilder{name: fakeBalancerA})
	balancer.Register(&fakeBalancerBuilder{name: fakeBalancerB})
}

type fakeBalancerBuilder struct {
	name string
}

func (b *fakeBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &fakeBalancer{cc: cc}
}

func (b *fakeBalancerBuilder) Name() string {
	return b.name
}

type fakeBalancer struct {
	cc balancer.ClientConn
}

func (b *fakeBalancer) ResolverError(error) {
	panic("implement me")
}

func (b *fakeBalancer) UpdateClientConnState(balancer.ClientConnState) error {
	panic("implement me")
}

func (b *fakeBalancer) UpdateSubConnState(balancer.SubConn, balancer.SubConnState) {
	panic("implement me")
}

func (b *fakeBalancer) Close() {}

// TestConfigChildPolicyUpdate verifies scenarios where the childPolicy
// section of the lbConfig is updated.
//
// The test does the following:
// * Builds a new EDS balancer.
// * Pushes a new ClientConnState with a childPolicy set to fakeBalancerA.
//   Verifies that an EDS watch is registered. It then pushes a new edsUpdate
//   through the fakexds client. Verifies that a new edsLB is created and it
//   receives the expected childPolicy.
// * Pushes a new ClientConnState with a childPolicy set to fakeBalancerB.
//   Verifies that the existing edsLB receives the new child policy.
func (s) TestConfigChildPolicyUpdate(t *testing.T) {
	edsLBCh := testutils.NewChannel()
	xdsC, cleanup := setup(edsLBCh)
	defer cleanup()

	builder := balancer.Get(edsName)
	edsB := builder.Build(newNoopTestClientConn(), balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}})
	if edsB == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", edsName)
	}
	defer edsB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	edsLB, err := waitForNewEDSLB(ctx, edsLBCh)
	if err != nil {
		t.Fatal(err)
	}

	lbCfgA := &loadBalancingConfig{
		Name:   fakeBalancerA,
		Config: json.RawMessage("{}"),
	}
	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{
			ChildPolicy:    lbCfgA,
			EDSServiceName: testServiceName,
		},
	}); err != nil {
		t.Fatalf("edsB.UpdateClientConnState() failed: %v", err)
	}

	if _, err := xdsC.WaitForWatchEDS(ctx); err != nil {
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	xdsC.InvokeWatchEDSCallback(defaultEndpointsUpdate, nil)
	if err := edsLB.waitForChildPolicy(ctx, lbCfgA); err != nil {
		t.Fatal(err)
	}
	if err := edsLB.waitForCounterUpdate(ctx, testServiceName); err != nil {
		t.Fatal(err)
	}

	lbCfgB := &loadBalancingConfig{
		Name:   fakeBalancerB,
		Config: json.RawMessage("{}"),
	}
	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{
			ChildPolicy:    lbCfgB,
			EDSServiceName: testServiceName,
		},
	}); err != nil {
		t.Fatalf("edsB.UpdateClientConnState() failed: %v", err)
	}
	if err := edsLB.waitForChildPolicy(ctx, lbCfgB); err != nil {
		t.Fatal(err)
	}
	if err := edsLB.waitForCounterUpdate(ctx, testServiceName); err != nil {
		// Counter is updated even though the service name didn't change. The
		// eds_impl will compare the service names, and skip if it didn't change.
		t.Fatal(err)
	}
}

// TestSubConnStateChange verifies if the top-level edsBalancer passes on
// the subConnStateChange to appropriate child balancer.
func (s) TestSubConnStateChange(t *testing.T) {
	edsLBCh := testutils.NewChannel()
	xdsC, cleanup := setup(edsLBCh)
	defer cleanup()

	builder := balancer.Get(edsName)
	edsB := builder.Build(newNoopTestClientConn(), balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}})
	if edsB == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", edsName)
	}
	defer edsB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	edsLB, err := waitForNewEDSLB(ctx, edsLBCh)
	if err != nil {
		t.Fatal(err)
	}

	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{EDSServiceName: testServiceName},
	}); err != nil {
		t.Fatalf("edsB.UpdateClientConnState() failed: %v", err)
	}

	if _, err := xdsC.WaitForWatchEDS(ctx); err != nil {
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	xdsC.InvokeWatchEDSCallback(defaultEndpointsUpdate, nil)

	fsc := &fakeSubConn{}
	state := connectivity.Ready
	edsB.UpdateSubConnState(fsc, balancer.SubConnState{ConnectivityState: state})
	if err := edsLB.waitForSubConnStateChange(ctx, &scStateChange{sc: fsc, state: state}); err != nil {
		t.Fatal(err)
	}
}

// TestErrorFromXDSClientUpdate verifies that an error from xdsClient update is
// handled correctly.
//
// If it's resource-not-found, watch will NOT be canceled, the EDS impl will
// receive an empty EDS update, and new RPCs will fail.
//
// If it's connection error, nothing will happen. This will need to change to
// handle fallback.
func (s) TestErrorFromXDSClientUpdate(t *testing.T) {
	edsLBCh := testutils.NewChannel()
	xdsC, cleanup := setup(edsLBCh)
	defer cleanup()

	builder := balancer.Get(edsName)
	edsB := builder.Build(newNoopTestClientConn(), balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}})
	if edsB == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", edsName)
	}
	defer edsB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	edsLB, err := waitForNewEDSLB(ctx, edsLBCh)
	if err != nil {
		t.Fatal(err)
	}

	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{EDSServiceName: testServiceName},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := xdsC.WaitForWatchEDS(ctx); err != nil {
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	xdsC.InvokeWatchEDSCallback(xdsclient.EndpointsUpdate{}, nil)
	if err := edsLB.waitForEDSResponse(ctx, xdsclient.EndpointsUpdate{}); err != nil {
		t.Fatalf("EDS impl got unexpected EDS response: %v", err)
	}

	connectionErr := xdsclient.NewErrorf(xdsclient.ErrorTypeConnection, "connection error")
	xdsC.InvokeWatchEDSCallback(xdsclient.EndpointsUpdate{}, connectionErr)

	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := xdsC.WaitForCancelEDSWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("watch was canceled, want not canceled (timeout error)")
	}

	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsLB.waitForEDSResponse(sCtx, xdsclient.EndpointsUpdate{}); err != context.DeadlineExceeded {
		t.Fatal(err)
	}

	resourceErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "edsBalancer resource not found error")
	xdsC.InvokeWatchEDSCallback(xdsclient.EndpointsUpdate{}, resourceErr)
	// Even if error is resource not found, watch shouldn't be canceled, because
	// this is an EDS resource removed (and xds client actually never sends this
	// error, but we still handles it).
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := xdsC.WaitForCancelEDSWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("watch was canceled, want not canceled (timeout error)")
	}
	if err := edsLB.waitForEDSResponse(ctx, xdsclient.EndpointsUpdate{}); err != nil {
		t.Fatalf("eds impl expecting empty update, got %v", err)
	}
}

// TestErrorFromResolver verifies that resolver errors are handled correctly.
//
// If it's resource-not-found, watch will be canceled, the EDS impl will receive
// an empty EDS update, and new RPCs will fail.
//
// If it's connection error, nothing will happen. This will need to change to
// handle fallback.
func (s) TestErrorFromResolver(t *testing.T) {
	edsLBCh := testutils.NewChannel()
	xdsC, cleanup := setup(edsLBCh)
	defer cleanup()

	builder := balancer.Get(edsName)
	edsB := builder.Build(newNoopTestClientConn(), balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}})
	if edsB == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", edsName)
	}
	defer edsB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	edsLB, err := waitForNewEDSLB(ctx, edsLBCh)
	if err != nil {
		t.Fatal(err)
	}

	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{EDSServiceName: testServiceName},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := xdsC.WaitForWatchEDS(ctx); err != nil {
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	xdsC.InvokeWatchEDSCallback(xdsclient.EndpointsUpdate{}, nil)
	if err := edsLB.waitForEDSResponse(ctx, xdsclient.EndpointsUpdate{}); err != nil {
		t.Fatalf("EDS impl got unexpected EDS response: %v", err)
	}

	connectionErr := xdsclient.NewErrorf(xdsclient.ErrorTypeConnection, "connection error")
	edsB.ResolverError(connectionErr)

	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := xdsC.WaitForCancelEDSWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("watch was canceled, want not canceled (timeout error)")
	}

	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsLB.waitForEDSResponse(sCtx, xdsclient.EndpointsUpdate{}); err != context.DeadlineExceeded {
		t.Fatal("eds impl got EDS resp, want timeout error")
	}

	resourceErr := xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "edsBalancer resource not found error")
	edsB.ResolverError(resourceErr)
	if err := xdsC.WaitForCancelEDSWatch(ctx); err != nil {
		t.Fatalf("want watch to be canceled, waitForCancel failed: %v", err)
	}
	if err := edsLB.waitForEDSResponse(ctx, xdsclient.EndpointsUpdate{}); err != nil {
		t.Fatalf("EDS impl got unexpected EDS response: %v", err)
	}
}

// Given a list of resource names, verifies that EDS requests for the same are
// sent by the EDS balancer, through the fake xDS client.
func verifyExpectedRequests(ctx context.Context, fc *fakeclient.Client, resourceNames ...string) error {
	for _, name := range resourceNames {
		if name == "" {
			// ResourceName empty string indicates a cancel.
			if err := fc.WaitForCancelEDSWatch(ctx); err != nil {
				return fmt.Errorf("timed out when expecting resource %q", name)
			}
			return nil
		}

		resName, err := fc.WaitForWatchEDS(ctx)
		if err != nil {
			return fmt.Errorf("timed out when expecting resource %q, %p", name, fc)
		}
		if resName != name {
			return fmt.Errorf("got EDS request for resource %q, expected: %q", resName, name)
		}
	}
	return nil
}

// TestClientWatchEDS verifies that the xdsClient inside the top-level EDS LB
// policy registers an EDS watch for expected resource upon receiving an update
// from gRPC.
func (s) TestClientWatchEDS(t *testing.T) {
	edsLBCh := testutils.NewChannel()
	xdsC, cleanup := setup(edsLBCh)
	defer cleanup()

	builder := balancer.Get(edsName)
	edsB := builder.Build(newNoopTestClientConn(), balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}})
	if edsB == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", edsName)
	}
	defer edsB.Close()

	// Update with an non-empty edsServiceName should trigger an EDS watch for
	// the same.
	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{EDSServiceName: "foobar-1"},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := verifyExpectedRequests(ctx, xdsC, "foobar-1"); err != nil {
		t.Fatal(err)
	}

	// Also test the case where the edsServerName changes from one non-empty
	// name to another, and make sure a new watch is registered. The previously
	// registered watch will be cancelled, which will result in an EDS request
	// with no resource names being sent to the server.
	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{EDSServiceName: "foobar-2"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := verifyExpectedRequests(ctx, xdsC, "", "foobar-2"); err != nil {
		t.Fatal(err)
	}
}

// TestCounterUpdate verifies that the counter update is triggered with the
// service name from an update's config.
func (s) TestCounterUpdate(t *testing.T) {
	edsLBCh := testutils.NewChannel()
	_, cleanup := setup(edsLBCh)
	defer cleanup()

	builder := balancer.Get(edsName)
	edsB := builder.Build(newNoopTestClientConn(), balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}})
	if edsB == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", edsName)
	}
	defer edsB.Close()

	// Update should trigger counter update with provided service name.
	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{EDSServiceName: "foobar-1"},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := edsB.(*edsBalancer).edsImpl.(*fakeEDSBalancer).waitForCounterUpdate(ctx, "foobar-1"); err != nil {
		t.Fatal(err)
	}
}

func (s) TestBalancerConfigParsing(t *testing.T) {
	const testEDSName = "eds.service"
	var testLRSName = "lrs.server"
	b := bytes.NewBuffer(nil)
	if err := (&jsonpb.Marshaler{}).Marshal(b, &scpb.XdsConfig{
		ChildPolicy: []*scpb.LoadBalancingConfig{
			{Policy: &scpb.LoadBalancingConfig_Xds{}},
			{Policy: &scpb.LoadBalancingConfig_RoundRobin{
				RoundRobin: &scpb.RoundRobinConfig{},
			}},
		},
		FallbackPolicy: []*scpb.LoadBalancingConfig{
			{Policy: &scpb.LoadBalancingConfig_Xds{}},
			{Policy: &scpb.LoadBalancingConfig_PickFirst{
				PickFirst: &scpb.PickFirstConfig{},
			}},
		},
		EdsServiceName:             testEDSName,
		LrsLoadReportingServerName: &wrapperspb.StringValue{Value: testLRSName},
	}); err != nil {
		t.Fatalf("%v", err)
	}

	tests := []struct {
		name    string
		js      json.RawMessage
		want    serviceconfig.LoadBalancingConfig
		wantErr bool
	}{
		{
			name:    "bad json",
			js:      json.RawMessage(`i am not JSON`),
			wantErr: true,
		},
		{
			name: "empty",
			js:   json.RawMessage(`{}`),
			want: &EDSConfig{},
		},
		{
			name: "jsonpb-generated",
			js:   b.Bytes(),
			want: &EDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   "round_robin",
					Config: json.RawMessage("{}"),
				},
				FallBackPolicy: &loadBalancingConfig{
					Name:   "pick_first",
					Config: json.RawMessage("{}"),
				},
				EDSServiceName:             testEDSName,
				LrsLoadReportingServerName: &testLRSName,
			},
		},
		{
			// json with random balancers, and the first is not registered.
			name: "manually-generated",
			js: json.RawMessage(`
{
  "childPolicy": [
    {"fake_balancer_C": {}},
    {"fake_balancer_A": {}},
    {"fake_balancer_B": {}}
  ],
  "fallbackPolicy": [
    {"fake_balancer_C": {}},
    {"fake_balancer_B": {}},
    {"fake_balancer_A": {}}
  ],
  "edsServiceName": "eds.service",
  "lrsLoadReportingServerName": "lrs.server"
}`),
			want: &EDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   "fake_balancer_A",
					Config: json.RawMessage("{}"),
				},
				FallBackPolicy: &loadBalancingConfig{
					Name:   "fake_balancer_B",
					Config: json.RawMessage("{}"),
				},
				EDSServiceName:             testEDSName,
				LrsLoadReportingServerName: &testLRSName,
			},
		},
		{
			// json with no lrs server name, LrsLoadReportingServerName should
			// be nil (not an empty string).
			name: "no-lrs-server-name",
			js: json.RawMessage(`
{
  "edsServiceName": "eds.service"
}`),
			want: &EDSConfig{
				EDSServiceName:             testEDSName,
				LrsLoadReportingServerName: nil,
			},
		},
		{
			name: "good child policy",
			js:   json.RawMessage(`{"childPolicy":[{"pick_first":{}}]}`),
			want: &EDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   "pick_first",
					Config: json.RawMessage(`{}`),
				},
			},
		},
		{
			name: "multiple good child policies",
			js:   json.RawMessage(`{"childPolicy":[{"round_robin":{}},{"pick_first":{}}]}`),
			want: &EDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   "round_robin",
					Config: json.RawMessage(`{}`),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &edsBalancerBuilder{}
			got, err := b.ParseConfig(tt.js)
			if (err != nil) != tt.wantErr {
				t.Fatalf("edsBalancerBuilder.ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !cmp.Equal(got, tt.want) {
				t.Errorf(cmp.Diff(got, tt.want))
			}
		})
	}
}

func (s) TestEqualStringPointers(t *testing.T) {
	var (
		ta1 = "test-a"
		ta2 = "test-a"
		tb  = "test-b"
	)
	tests := []struct {
		name string
		a    *string
		b    *string
		want bool
	}{
		{"both-nil", nil, nil, true},
		{"a-non-nil", &ta1, nil, false},
		{"b-non-nil", nil, &tb, false},
		{"equal", &ta1, &ta2, true},
		{"different", &ta1, &tb, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalStringPointers(tt.a, tt.b); got != tt.want {
				t.Errorf("equalStringPointers() = %v, want %v", got, tt.want)
			}
		})
	}
}
