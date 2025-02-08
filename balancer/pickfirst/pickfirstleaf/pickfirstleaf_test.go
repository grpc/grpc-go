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

package pickfirstleaf

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
)

const (
	// Default timeout for tests in this package.
	defaultTestTimeout = 10 * time.Second
	// Default short timeout, to be used when waiting for events which are not
	// expected to happen.
	defaultTestShortTimeout = 100 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestAddressList_Iteration verifies the behaviour of the addressList while
// iterating through the entries.
func (s) TestAddressList_Iteration(t *testing.T) {
	addrs := []resolver.Address{
		{
			Addr:               "192.168.1.1",
			ServerName:         "test-host-1",
			Attributes:         attributes.New("key-1", "val-1"),
			BalancerAttributes: attributes.New("bal-key-1", "bal-val-1"),
		},
		{
			Addr:               "192.168.1.2",
			ServerName:         "test-host-2",
			Attributes:         attributes.New("key-2", "val-2"),
			BalancerAttributes: attributes.New("bal-key-2", "bal-val-2"),
		},
		{
			Addr:               "192.168.1.3",
			ServerName:         "test-host-3",
			Attributes:         attributes.New("key-3", "val-3"),
			BalancerAttributes: attributes.New("bal-key-3", "bal-val-3"),
		},
	}

	addressList := addressList{}
	addressList.updateAddrs(addrs)

	for i := 0; i < len(addrs); i++ {
		if got, want := addressList.isValid(), true; got != want {
			t.Fatalf("addressList.isValid() = %t, want %t", got, want)
		}
		if got, want := addressList.currentAddress(), addrs[i]; !want.Equal(got) {
			t.Errorf("addressList.currentAddress() = %v, want %v", got, want)
		}
		if got, want := addressList.increment(), i+1 < len(addrs); got != want {
			t.Fatalf("addressList.increment() = %t, want %t", got, want)
		}
	}

	if got, want := addressList.isValid(), false; got != want {
		t.Fatalf("addressList.isValid() = %t, want %t", got, want)
	}

	// increment an invalid address list.
	if got, want := addressList.increment(), false; got != want {
		t.Errorf("addressList.increment() = %t, want %t", got, want)
	}

	if got, want := addressList.isValid(), false; got != want {
		t.Errorf("addressList.isValid() = %t, want %t", got, want)
	}

	addressList.reset()
	for i := 0; i < len(addrs); i++ {
		if got, want := addressList.isValid(), true; got != want {
			t.Fatalf("addressList.isValid() = %t, want %t", got, want)
		}
		if got, want := addressList.currentAddress(), addrs[i]; !want.Equal(got) {
			t.Errorf("addressList.currentAddress() = %v, want %v", got, want)
		}
		if got, want := addressList.increment(), i+1 < len(addrs); got != want {
			t.Fatalf("addressList.increment() = %t, want %t", got, want)
		}
	}
}

// TestAddressList_SeekTo verifies the behaviour of addressList.seekTo.
func (s) TestAddressList_SeekTo(t *testing.T) {
	addrs := []resolver.Address{
		{
			Addr:               "192.168.1.1",
			ServerName:         "test-host-1",
			Attributes:         attributes.New("key-1", "val-1"),
			BalancerAttributes: attributes.New("bal-key-1", "bal-val-1"),
		},
		{
			Addr:               "192.168.1.2",
			ServerName:         "test-host-2",
			Attributes:         attributes.New("key-2", "val-2"),
			BalancerAttributes: attributes.New("bal-key-2", "bal-val-2"),
		},
		{
			Addr:               "192.168.1.3",
			ServerName:         "test-host-3",
			Attributes:         attributes.New("key-3", "val-3"),
			BalancerAttributes: attributes.New("bal-key-3", "bal-val-3"),
		},
	}

	addressList := addressList{}
	addressList.updateAddrs(addrs)

	// Try finding an address in the list.
	key := resolver.Address{
		Addr:               "192.168.1.2",
		ServerName:         "test-host-2",
		Attributes:         attributes.New("key-2", "val-2"),
		BalancerAttributes: attributes.New("ignored", "bal-val-2"),
	}

	if got, want := addressList.seekTo(key), true; got != want {
		t.Errorf("addressList.seekTo(%v) = %t, want %t", key, got, want)
	}

	// It should be possible to increment once more now that the pointer has advanced.
	if got, want := addressList.increment(), true; got != want {
		t.Errorf("addressList.increment() = %t, want %t", got, want)
	}

	if got, want := addressList.increment(), false; got != want {
		t.Errorf("addressList.increment() = %t, want %t", got, want)
	}

	// Seek to the key again, it is behind the pointer now.
	if got, want := addressList.seekTo(key), true; got != want {
		t.Errorf("addressList.seekTo(%v) = %t, want %t", key, got, want)
	}

	// Seek to a key not in the list.
	key = resolver.Address{
		Addr:               "192.168.1.5",
		ServerName:         "test-host-5",
		Attributes:         attributes.New("key-5", "val-5"),
		BalancerAttributes: attributes.New("ignored", "bal-val-5"),
	}

	if got, want := addressList.seekTo(key), false; got != want {
		t.Errorf("addressList.seekTo(%v) = %t, want %t", key, got, want)
	}

	// It should be possible to increment once more since the pointer has not advanced.
	if got, want := addressList.increment(), true; got != want {
		t.Errorf("addressList.increment() = %t, want %t", got, want)
	}

	if got, want := addressList.increment(), false; got != want {
		t.Errorf("addressList.increment() = %t, want %t", got, want)
	}
}

// TestPickFirstLeaf_TFPickerUpdate sends TRANSIENT_FAILURE SubConn state updates
// for each SubConn managed by a pickfirst balancer. It verifies that the picker
// is updated with the expected frequency.
func (s) TestPickFirstLeaf_TFPickerUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := pickfirstBuilder{}.Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}},
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	// PF should report TRANSIENT_FAILURE only once all the sunbconns have failed
	// once.
	tfErr := fmt.Errorf("test err: connection refused")
	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure, ConnectionError: tfErr})

	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", balancer.ErrNoSubConnAvailable, err)
	}

	sc2 := <-cc.NewSubConnCh
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure, ConnectionError: tfErr})

	if err := cc.WaitForPickerWithErr(ctx, tfErr); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", tfErr, err)
	}

	// Subsequent TRANSIENT_FAILUREs should be reported only after seeing "# of SubConns"
	// TRANSIENT_FAILUREs.
	newTfErr := fmt.Errorf("test err: unreachable")
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure, ConnectionError: newTfErr})
	select {
	case <-time.After(defaultTestShortTimeout):
	case p := <-cc.NewPickerCh:
		sc, err := p.Pick(balancer.PickInfo{})
		t.Fatalf("Unexpected picker update: %v, %v", sc, err)
	}

	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure, ConnectionError: newTfErr})
	if err := cc.WaitForPickerWithErr(ctx, newTfErr); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", newTfErr, err)
	}
}
