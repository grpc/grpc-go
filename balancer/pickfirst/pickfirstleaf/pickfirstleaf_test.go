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
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirst/internal"
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

// TestPickFirstLeaf_HappyEyeballs_TriggerConnectionDelay verifies that
// pickfirst attempts to connect to the second backend once the happy eyeballs
// timer expires.
func (s) TestPickFirstLeaf_HappyEyeballs_TriggerConnectionDelay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	timerCh := make(chan struct{})
	originalTimer := internal.TimeAfterFunc
	internal.TimeAfterFunc = func(_ time.Duration, f func()) *time.Timer {
		// Set a really long expiration to prevent it from triggering
		// automatically.
		ret := time.AfterFunc(time.Hour, f)
		go func() {
			select {
			case <-ctx.Done():
			case <-timerCh:
			}
			ret.Reset(0)
		}()
		return ret
	}

	defer func() {
		internal.TimeAfterFunc = originalTimer
	}()

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

	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", balancer.ErrNoSubConnAvailable, err)
	}

	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("cc.WaitForConnectivityState(%v) returned error: %v", connectivity.Connecting, err)
	}

	sc0 := <-cc.NewSubConnCh

	// Until the timer fires, no new subchannel should be created.
	select {
	case <-time.After(defaultTestShortTimeout):
	case sc1 := <-cc.NewSubConnCh:
		t.Fatalf("Received unexpected subchannel: %v", sc1)
	}

	timerCh <- struct{}{}

	sc1 := <-cc.NewSubConnCh

	// Second connection attempt is successful.
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("cc.WaitForConnectivityState(%v) returned error: %v", connectivity.Ready, err)
	}

	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		pr, err := p.Pick(balancer.PickInfo{})
		if err != nil {
			return err
		}
		if pr.SubConn != sc1 {
			t.Fatalf("Unexpected SubConn produced by picker, got = %v, want = %v", pr.SubConn, sc1)
		}
		return nil
	}); err != nil {
		t.Fatalf("cc.WaitForPicker() returned error: %v", err)
	}

	closedSC := <-cc.ShutdownSubConnCh
	if closedSC != sc0 {
		t.Fatalf("Unexpected closed SubConn, got = %v, want = %v", closedSC, sc0)
	}
}

// TestPickFirstLeaf_HappyEyeballs_TFAfterEndOfList verifies that pickfirst
// correctly detects the end of the first happy eyeballs pass when the timer
// causes pickfirst to reach the end of the address list and failures are
// reported out of order.
func (s) TestPickFirstLeaf_HappyEyeballs_TFAfterEndOfList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	timerCh := make(chan struct{})
	originalTimer := internal.TimeAfterFunc
	internal.TimeAfterFunc = func(_ time.Duration, f func()) *time.Timer {
		// Set a really long expiration to prevent it from triggering
		// automatically.
		ret := time.AfterFunc(time.Hour, f)
		go func() {
			select {
			case <-ctx.Done():
			case <-timerCh:
			}
			ret.Reset(0)
		}()
		return ret
	}

	defer func() {
		internal.TimeAfterFunc = originalTimer
	}()

	defer func() {
		internal.TimeAfterFunc = originalTimer
	}()

	cc := testutils.NewBalancerClientConn(t)
	bal := pickfirstBuilder{}.Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}},
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
				{Addresses: []resolver.Address{{Addr: "3.3.3.3:3"}}},
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", balancer.ErrNoSubConnAvailable, err)
	}

	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("cc.WaitForConnectivityState(%v) returned error: %v", connectivity.Connecting, err)
	}

	sc0 := <-cc.NewSubConnCh
	// Make the happy eyeballs timer fire twice so that pickfirst reaches the
	// last address in the list.
	timerCh <- struct{}{}
	sc1 := <-cc.NewSubConnCh
	timerCh <- struct{}{}
	sc2 := <-cc.NewSubConnCh

	// First SubConn Fails.
	tfErr := fmt.Errorf("test error")
	sc0.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   tfErr,
	})

	// No picker should be produced until the first pass is complete.
	select {
	case <-time.After(defaultTestShortTimeout):
	case p := <-cc.NewPickerCh:
		sc, err := p.Pick(balancer.PickInfo{})
		t.Fatalf("Unexpected picker update: %v, %v", sc, err)
	}

	// Move off the end of the list, pickfirst should still be waiting for TFs
	// to be reported.
	timerCh <- struct{}{}

	// second SubConn fails.
	sc1.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   tfErr,
	})

	// second SubConn fails again, we've still not seen the 3rd SubConn fail.
	// Second connection attempt is successful.
	sc1.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   tfErr,
	})

	select {
	case <-time.After(defaultTestShortTimeout):
	case p := <-cc.NewPickerCh:
		sc, err := p.Pick(balancer.PickInfo{})
		t.Fatalf("Unexpected picker update: %v, %v", sc, err)
	}

	// Last SubConn fails, this should result in a picker update.
	sc2.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   tfErr,
	})

	if err := cc.WaitForPickerWithErr(ctx, tfErr); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", tfErr, err)
	}

	// Fail the first SubConn 3 times, causing the next picker update.
	tfErr = fmt.Errorf("New test error")
	sc0.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   tfErr,
	})
	sc0.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   tfErr,
	})

	select {
	case <-time.After(defaultTestShortTimeout):
	case p := <-cc.NewPickerCh:
		sc, err := p.Pick(balancer.PickInfo{})
		t.Fatalf("Unexpected picker update: %v, %v", sc, err)
	}

	sc0.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   tfErr,
	})

	if err := cc.WaitForPickerWithErr(ctx, tfErr); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", tfErr, err)
	}
}

// TestPickFirstLeaf_HappyEyeballs_TFThenTimerFires tests the pickfirst balancer
// by causing a SubConn to fail and then jumping to the 3rd SubConn after the
// happy eyeballs timer expires.
func (s) TestPickFirstLeaf_HappyEyeballs_TFThenTimerFires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	timerMu := sync.Mutex{}
	timerCh := make(chan struct{})
	originalTimer := internal.TimeAfterFunc
	internal.TimeAfterFunc = func(_ time.Duration, f func()) *time.Timer {
		// Set a really long expiration to prevent it from triggering
		// automatically.
		ret := time.AfterFunc(time.Hour, f)
		go func() {
			timerMu.Lock()
			ch := timerCh
			timerMu.Unlock()
			select {
			case <-ctx.Done():
			case <-ch:
			}
			ret.Reset(0)
		}()
		return ret
	}

	defer func() {
		internal.TimeAfterFunc = originalTimer
	}()

	cc := testutils.NewBalancerClientConn(t)
	bal := pickfirstBuilder{}.Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}},
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
				{Addresses: []resolver.Address{{Addr: "3.3.3.3:3"}}},
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatalf("cc.WaitForPickerWithErr(%v) returned error: %v", balancer.ErrNoSubConnAvailable, err)
	}

	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("cc.WaitForConnectivityState(%v) returned error: %v", connectivity.Connecting, err)
	}

	sc0 := <-cc.NewSubConnCh
	// Until the timer fires or a TF is reported, no new subchannel should be
	// created.
	select {
	case <-time.After(defaultTestShortTimeout):
	case sc1 := <-cc.NewSubConnCh:
		t.Fatalf("Received unexpected subchannel: %v", sc1)
	}

	// First SubConn Fails.
	tfErr := fmt.Errorf("test error")
	// Replace the timer channel so that the old timers don't attempt to read
	// messages pushed next.
	timerMu.Lock()
	timerCh = make(chan struct{})
	timerMu.Unlock()
	sc0.UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   tfErr,
	})

	sc1 := <-cc.NewSubConnCh
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})

	// The happy eyeballs timer expires, skipping sc1 and requesting the creation
	// of sc2.
	timerCh <- struct{}{}
	<-cc.NewSubConnCh

	// First SubConn comes out of backoff.
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Idle})
	// Second SubConn connects.
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	// Verify that the picker is updated.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		pr, err := p.Pick(balancer.PickInfo{})
		if err != nil {
			return err
		}
		if pr.SubConn != sc1 {
			t.Fatalf("Unexpected SubConn produced by picker, got = %v, want = %v", pr.SubConn, sc1)
		}
		return nil
	}); err != nil {
		t.Fatalf("cc.WaitForPicker() returned error: %v", err)
	}
}
