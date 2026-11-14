/*
 *
 * Copyright 2024 gRPC authors.
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

package endpointsharding_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/endpointsharding"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/roundrobin"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var (
	defaultTestTimeout      = time.Second * 10
	defaultTestShortTimeout = time.Millisecond * 10
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var logger = grpclog.Component("endpoint-sharding-test")

// TestEndpointShardingIgnoreEmptyEndpoints verifies that empty endpoints
// do not affect round robin RPCs or prevent TransientFailure after all backends
// stop.
func (s) TestEndpointShardingIgnoreEmptyEndpoints(t *testing.T) {
	backend1 := stubserver.StartTestService(t, nil)
	t.Cleanup(backend1.Stop)
	backend2 := stubserver.StartTestService(t, nil)
	t.Cleanup(backend2.Stop)
	cc, _ := newEndpointShardingClient(t, []resolver.Endpoint{
		{Addresses: []resolver.Address{{Addr: backend1.Address}}},
		{},
		{Addresses: []resolver.Address{{Addr: backend2.Address}}},
		{Addresses: []resolver.Address{}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	if err := roundrobin.CheckRoundRobinRPCs(ctx, client, []resolver.Address{{Addr: backend1.Address}, {Addr: backend2.Address}}); err != nil {
		t.Fatalf("CheckRoundRobinRPCs() failed: %v", err)
	}
	backend1.Stop()
	backend2.Stop()
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
}

// TestEndpointShardingUpdateToAllEmptyEndpoints verifies that an update with
// only empty endpoints returns ErrBadResolverState and makes RPCs fail with
// Unavailable after the channel enters TransientFailure.
func (s) TestEndpointShardingUpdateToAllEmptyEndpoints(t *testing.T) {
	backend := stubserver.StartTestService(t, nil)
	t.Cleanup(backend.Stop)
	cc, mr := newEndpointShardingClient(t, []resolver.Endpoint{
		{Addresses: []resolver.Address{{Addr: backend.Address}}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("Initial EmptyCall() failed: %v", err)
	}
	state := resolver.State{
		Endpoints: []resolver.Endpoint{{}, {Addresses: []resolver.Address{}}},
	}
	if err := mr.CC().UpdateState(state); err != balancer.ErrBadResolverState {
		t.Fatalf("UpdateState() returned %v, want %v", err, balancer.ErrBadResolverState)
	}
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("EmptyCall() returned error %v, want code %v", err, codes.Unavailable)
	}
}

func newEndpointShardingClient(t *testing.T, endpoints []resolver.Endpoint) (*grpc.ClientConn, *manual.Resolver) {
	t.Helper()
	name := t.Name()
	balancer.Register(endpointShardingTestBalancerBuilder{name: name})
	mr := manual.NewBuilderWithScheme("e2e-test")
	t.Cleanup(mr.Close)
	mr.InitialState(resolver.State{Endpoints: endpoints})
	cc, err := grpc.NewClient(mr.Scheme()+":///",
		grpc.WithResolvers(mr),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{%q:{}}]}`, name)),
	)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	return cc, mr
}

type endpointShardingTestBalancerBuilder struct {
	name string
}

func (b endpointShardingTestBalancerBuilder) Name() string { return b.name }

func (endpointShardingTestBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return endpointsharding.NewBalancer(cc, opts, balancer.Get(pickfirst.Name).Build, endpointsharding.Options{})
}

func init() {
	balancer.Register(fakePetioleBuilder{})
}

const fakePetioleName = "fake_petiole"

type fakePetioleBuilder struct{}

func (fakePetioleBuilder) Name() string {
	return fakePetioleName
}

func (fakePetioleBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	fp := &fakePetiole{
		ClientConn: cc,
		bOpts:      opts,
	}
	fp.Balancer = endpointsharding.NewBalancer(fp, opts, balancer.Get(pickfirst.Name).Build, endpointsharding.Options{})
	return fp
}

func (fakePetioleBuilder) ParseConfig(json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return nil, nil
}

// fakePetiole is a load balancer that wraps the endpointShardingBalancer, and
// forwards ClientConnUpdates with a child config of graceful switch that wraps
// pick first. It also intercepts UpdateState to make sure it can access the
// child state maintained by EndpointSharding.
type fakePetiole struct {
	balancer.Balancer
	balancer.ClientConn
	bOpts balancer.BuildOptions
}

func (fp *fakePetiole) UpdateClientConnState(state balancer.ClientConnState) error {
	if el := state.ResolverState.Endpoints; len(el) != 2 {
		return fmt.Errorf("UpdateClientConnState wants two endpoints, got: %v", el)
	}

	return fp.Balancer.UpdateClientConnState(state)
}

func (fp *fakePetiole) UpdateState(state balancer.State) {
	childStates := endpointsharding.ChildStatesFromPicker(state.Picker)
	// Both child states should be present in the child picker. States and
	// picker change over the lifecycle of test, but there should always be two.
	if len(childStates) != 2 {
		logger.Fatal(fmt.Errorf("length of child states received: %v, want 2", len(childStates)))
	}

	fp.ClientConn.UpdateState(state)
}

// TestEndpointShardingBasic tests the basic functionality of the endpoint
// sharding balancer. It specifies a petiole policy that is essentially a
// wrapper around the endpoint sharder. Two backends are started, with each
// backend's address specified in an endpoint. The petiole does not have a
// special picker, so it should fallback to the default behavior, which is to
// round_robin amongst the endpoint children that are in the aggregated state.
// It also verifies the petiole has access to the raw child state in case it
// wants to implement a custom picker. The test sends a resolver error to the
// endpointsharding balancer and verifies an error picker from the children
// is used while making an RPC.
func (s) TestEndpointShardingBasic(t *testing.T) {
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	json := fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, fakePetioleName)
	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(json)
	mr.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend1.Address}}},
			{Addresses: []resolver.Address{{Addr: backend2.Address}}},
		},
		ServiceConfig: sc,
	})

	dOpts := []grpc.DialOption{
		grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Use a large backoff delay to avoid the error picker being updated
		// too quickly.
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  2 * defaultTestTimeout,
				Multiplier: float64(0),
				Jitter:     float64(0),
				MaxDelay:   2 * defaultTestTimeout,
			},
		}),
	}
	cc, err := grpc.NewClient(mr.Scheme()+":///", dOpts...)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	// Assert a round robin distribution between the two spun up backends. This
	// requires a poll and eventual consistency as both endpoint children do not
	// start in state READY.
	if err = roundrobin.CheckRoundRobinRPCs(ctx, client, []resolver.Address{{Addr: backend1.Address}, {Addr: backend2.Address}}); err != nil {
		t.Fatalf("error in expected round robin: %v", err)
	}

	// Stopping both the backends should make the channel enter
	// TransientFailure.
	backend1.Stop()
	backend2.Stop()
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// When the resolver reports an error, the picker should get updated to
	// return the resolver error.
	mr.CC().ReportError(errors.New("test error"))
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		if err == nil {
			t.Fatalf("EmptyCall succeeded when expected to fail with %q", "test error")
		}
		if strings.Contains(err.Error(), "test error") {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("Context timed out waiting for picker with resolver error.")
	}
}

// Tests that endpointsharding doesn't automatically re-connect IDLE children.
// The test creates an endpoint with two servers and another with a single
// server. The active service in endpoint 1 is closed to make the child
// pickfirst enter IDLE state. The test verifies that the child pickfirst
// doesn't connect to the second address in the endpoint.
func (s) TestEndpointShardingReconnectDisabled(t *testing.T) {
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()
	backend3 := stubserver.StartTestService(t, nil)
	defer backend3.Stop()

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	name := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "")
	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			epOpts := endpointsharding.Options{DisableAutoReconnect: true}
			bd.ChildBalancer = endpointsharding.NewBalancer(bd.ClientConn, bd.BuildOptions, balancer.Get(pickfirst.Name).Build, epOpts)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
	}
	stub.Register(name, bf)

	json := fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, name)
	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(json)
	mr.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend1.Address}, {Addr: backend2.Address}}},
			{Addresses: []resolver.Address{{Addr: backend3.Address}}},
		},
		ServiceConfig: sc,
	})

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	// Assert a round robin distribution between the two spun up backends. This
	// requires a poll and eventual consistency as both endpoint children do not
	// start in state READY.
	if err = roundrobin.CheckRoundRobinRPCs(ctx, client, []resolver.Address{{Addr: backend1.Address}, {Addr: backend3.Address}}); err != nil {
		t.Fatalf("error in expected round robin: %v", err)
	}

	// On closing the first server, the first child balancer should enter
	// IDLE. Since endpointsharding is configured not to auto-reconnect, it will
	// remain IDLE and will not try to connect to the second backend in the same
	// endpoint.
	backend1.Stop()
	// CheckRoundRobinRPCs waits for all the backends to become reachable, we
	// call it to ensure the picker no longer sends RPCs to closed backend.
	if err = roundrobin.CheckRoundRobinRPCs(ctx, client, []resolver.Address{{Addr: backend3.Address}}); err != nil {
		t.Fatalf("error in expected round robin: %v", err)
	}

	// Verify requests go only to backend3 for a short time.
	shortCtx, cancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer cancel()
	for ; shortCtx.Err() == nil; <-time.After(time.Millisecond) {
		var peer peer.Peer
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
			if status.Code(err) != codes.DeadlineExceeded {
				t.Fatalf("EmptyCall() returned unexpected error %v", err)
			}
			break
		}
		if got, want := peer.Addr.String(), backend3.Address; got != want {
			t.Fatalf("EmptyCall() went to unexpected backend: got %q, want %q", got, want)
		}
	}
}

// Tests that endpointsharding doesn't automatically re-connect IDLE children
// until cc.Connect() is called. The test creates an endpoint with a single
// address. The client is connected and the active server is closed to make the
// child pickfirst enter IDLE state. The test verifies that the child pickfirst
// doesn't re-connect automatically. The test calls cc.Connect() and verified
// that the balancer connects causing the channel to enter TransientFailure.
func (s) TestEndpointShardingExitIdle(t *testing.T) {
	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	name := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "")
	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			epOpts := endpointsharding.Options{DisableAutoReconnect: true}
			bd.ChildBalancer = endpointsharding.NewBalancer(bd.ClientConn, bd.BuildOptions, balancer.Get(pickfirst.Name).Build, epOpts)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
		ExitIdle: func(bd *stub.BalancerData) {
			bd.ChildBalancer.ExitIdle()
		},
	}
	stub.Register(name, bf)

	json := fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, name)
	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(json)
	mr.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend.Address}}},
		},
		ServiceConfig: sc,
	})

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Errorf("client.EmptyCall() returned unexpected error: %v", err)
	}

	// On closing the first server, the first child balancer should enter
	// IDLE. Since endpointsharding is configured not to auto-reconnect, it will
	// remain IDLE and will not try to re-connect
	backend.Stop()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)
	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	testutils.AwaitNoStateChange(shortCtx, t, cc, connectivity.Idle)

	// The balancer should try to re-connect and fail.
	cc.Connect()
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
}

// Tests that a child balancer can't create new SubConns after it has been
// closed. The test removes the child's endpoint through a resolver update and
// verifies that a later NewSubConn call from the child fails.
func (s) TestEndpointShardingNewSubConnAfterClose(t *testing.T) {
	name := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "")
	stub.Register(name, stub.BalancerFuncs{})

	childCCs := make(chan balancer.ClientConn, 2)
	childBuilder := func(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
		childCCs <- cc
		return balancer.Get(name).Build(cc, opts)
	}
	tcc := testutils.NewBalancerClientConn(t)
	es := endpointsharding.NewBalancer(tcc, balancer.BuildOptions{}, childBuilder, endpointsharding.Options{DisableAutoReconnect: true})
	defer es.Close()

	ep1 := resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-1"}}}
	ep2 := resolver.Endpoint{Addresses: []resolver.Address{{Addr: "addr-2"}}}
	if err := es.UpdateClientConnState(balancer.ClientConnState{ResolverState: resolver.State{Endpoints: []resolver.Endpoint{ep1}}}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	childCC := <-childCCs
	if _, err := childCC.NewSubConn(ep1.Addresses, balancer.NewSubConnOptions{}); err != nil {
		t.Fatalf("NewSubConn() failed before the child was closed: %v", err)
	}

	// Removing ep1 from the resolver update closes its child balancer.
	if err := es.UpdateClientConnState(balancer.ClientConnState{ResolverState: resolver.State{Endpoints: []resolver.Endpoint{ep2}}}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	if _, err := childCC.NewSubConn(ep1.Addresses, balancer.NewSubConnOptions{}); err == nil {
		t.Fatal("NewSubConn() succeeded after the child was closed, want error")
	}
}
