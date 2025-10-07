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
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
	xdsinternal "google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/internal/xds/testutils/fakeclient"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/resolver"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultShortTestTimeout = 100 * time.Microsecond

	testClusterName = "test-cluster"
	testServiceName = "test-eds-service"
)

var (
	testBackendEndpoints = []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}}}
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func init() {
	NewRandomWRR = testutils.NewTestWRR
}

// TestPickerUpdateAfterClose covers the case where a child policy sends a
// picker update after the cluster_impl policy is closed. Because picker updates
// are handled in the run() goroutine, which exits before Close() returns, we
// expect the above picker update to be dropped.
func (s) TestPickerUpdateAfterClose(t *testing.T) {
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
	xdsC := fakeclient.NewClient()
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

	builder := balancer.Get(Name)
	cc := testutils.NewBalancerClientConn(t)
	b := builder.Build(cc, balancer.BuildOptions{})
	defer b.Close()

	xdsC := fakeclient.NewClient()
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

	xdsC := fakeclient.NewClient()
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
