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
	"testing"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/resolver"
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

	// It should be possible to increment once more since the pointer has not advanced.
	if got, want := addressList.increment(), true; got != want {
		t.Errorf("addressList.increment() = %t, want %t", got, want)
	}

	if got, want := addressList.increment(), false; got != want {
		t.Errorf("addressList.increment() = %t, want %t", got, want)
	}
}
