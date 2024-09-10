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

	endpoints := []resolver.Endpoint{
		{
			Addresses: []resolver.Address{addrs[0], addrs[1]},
		},
		{
			Addresses: []resolver.Address{addrs[2]},
		},
	}

	addressList := addressList{}
	addressList.updateEndpointList(endpoints)

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

	endpoints := []resolver.Endpoint{
		{
			Addresses: []resolver.Address{addrs[0], addrs[1]},
		},
		{
			Addresses: []resolver.Address{addrs[2]},
		},
	}

	addressList := addressList{}
	addressList.updateEndpointList(endpoints)

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

// TestPickFirstLeaf_TFPickerUpdate sends TRANSIENT_FAILURE subconn state updates
// for each subconn managed by a pickfirst balancer. It verifies that the picker
// is updated with the expected frequency.
func (s) TestPickFirstLeaf_TFPickerUpdate(t *testing.T) {
	cc := testutils.NewBalancerClientConn(t)
	bal := pickfirstBuilder{}.Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	bal.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}},
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
			},
		},
	})

	// PF should report TRANSIENT_FAILURE only once all the sunbconns have failed
	// once.
	tfErr := fmt.Errorf("test err: connection refused")
	sc0 := <-cc.NewSubConnCh
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure, ConnectionError: tfErr})

	p := <-cc.NewPickerCh
	_, err := p.Pick(balancer.PickInfo{})
	if want, got := balancer.ErrNoSubConnAvailable, err; got != want {
		t.Fatalf("picker.Pick() = %v, want %v", got, want)
	}

	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure, ConnectionError: tfErr})

	p = <-cc.NewPickerCh
	_, err = p.Pick(balancer.PickInfo{})
	if want, got := tfErr, err; got != want {
		t.Fatalf("picker.Pick() = %v, want %v", got, want)
	}

	// Subsequent TRANSIENT_FAILUREs should be reported only after seeing "# of subconns"
	// TRANSIENT_FAILUREs.
	newTfErr := fmt.Errorf("test err: unreachable")
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure, ConnectionError: newTfErr})
	select {
	case <-time.After(defaultTestShortTimeout):
	case p = <-cc.NewPickerCh:
		sc, err := p.Pick(balancer.PickInfo{})
		t.Fatalf("Unexpected picker update: %v, %v", sc, err)
	}

	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure, ConnectionError: newTfErr})
	p = <-cc.NewPickerCh
	_, err = p.Pick(balancer.PickInfo{})
	if want, got := newTfErr, err; got != want {
		t.Fatalf("picker.Pick() = %v, want %v", got, want)
	}
}
