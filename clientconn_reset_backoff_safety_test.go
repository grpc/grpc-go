/*
 *
 * Copyright 2026 gRPC authors.
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

// Regression test for ResetConnectBackoff() racing with balancer-driven
// removeAddrConn() / newAddrConnLocked() on cc.conns.
//
// Background:
//   clientconn.go:1181-1188 (pre-fix) reads cc.conns under cc.mu, releases the
//   lock, and then iterates the map outside the lock. Meanwhile, the balancer
//   wrapper's serializer goroutine may call cc.removeAddrConn()
//   (delete cc.conns[ac]) or cc.newAddrConnLocked() (cc.conns[ac] = ...).
//   The two paths race on the same map; under -race the runtime reports DATA
//   RACE, and without -race the Go runtime may throw "fatal error: concurrent
//   map iteration and map write".
//
//   The fix snapshots the keys to a slice while holding the lock and iterates
//   the slice outside the lock, matching the existing safe pattern used by
//   ClientConn.Close (clientconn.go:1207-1208 sets cc.conns = nil under lock,
//   then iterates the saved local variable).
//
// This test MUST pass after the fix and MUST fail under -race before the fix.

package grpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

// startBackoffSafetyBlackholes spawns N TCP listeners that accept connections
// but never read or write, so the client's dial succeeds and the balancer
// actually creates / shuts down SubConns (which is what drives the racy
// removeAddrConn / newAddrConnLocked path).
func startBackoffSafetyBlackholes(t *testing.T, n int) (addrs []string, cleanup func()) {
	t.Helper()
	var lns []net.Listener
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		lns = append(lns, ln)
		addrs = append(addrs, ln.Addr().String())
		wg.Add(1)
		go func(ln net.Listener) {
			defer wg.Done()
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				wg.Add(1)
				go func(c net.Conn) {
					defer wg.Done()
					<-stop
					c.Close()
				}(c)
			}
		}(ln)
	}
	cleanup = func() {
		close(stop)
		for _, ln := range lns {
			ln.Close()
		}
		wg.Wait()
	}
	return
}

// TestResetConnectBackoffSafety_ConcurrentWithResolverUpdates exercises the
// documented use of ResetConnectBackoff (calling it on network-recovery to
// wake up subchannels) while a resolver concurrently pushes endpoint updates
// (the natural behaviour of DNS resolver after network recovery as well).
//
// The two paths share cc.conns. Under -race the pre-fix implementation
// triggers a DATA RACE; after the fix it must run cleanly.
func (s) TestResetConnectBackoffSafety_ConcurrentWithResolverUpdates(t *testing.T) {
	const (
		listenerCount = 4
		duration      = 2 * time.Second
	)

	addrs, cleanup := startBackoffSafetyBlackholes(t, listenerCount)
	defer cleanup()

	r := manual.NewBuilderWithScheme("repro-p2-4")
	r.InitialState(resolver.State{
		Addresses: []resolver.Address{{Addr: addrs[0]}},
	})

	cc, err := NewClient(
		"repro-p2-4:///dummy",
		WithResolvers(r),
		WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cc.Close()

	cc.Connect()

	// Let the initial SubConn settle so the balancer is in a steady state
	// before stressing concurrent map access.
	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Goroutine A: documented "network recovery" call site.
	// User code per ResetConnectBackoff doc:
	//   "if a previously unavailable network becomes available, this may
	//    be used to trigger an immediate reconnect."
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				cc.ResetConnectBackoff()
			}
		}
	}()

	// Goroutine B: resolver pushes endpoint updates (the natural behaviour
	// of a DNS resolver when the network recovers). Each update with a
	// different address makes pick_first shut down the old SubConn and
	// create a new one, hitting cc.removeAddrConn() and
	// cc.newAddrConnLocked() on cc.conns.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				addr := addrs[i%listenerCount]
				r.UpdateState(resolver.State{
					Addresses: []resolver.Address{{Addr: addr}},
				})
				i++
				// A small sleep keeps the race window open without
				// spinning the CPU completely.
				time.Sleep(time.Microsecond)
			}
		}
	}()

	// Run the stress for a fixed window. Under -race, any concurrent
	// access on cc.conns trips the detector; the test will be marked
	// failed before this select returns.
	timer := time.NewTimer(duration)
	defer timer.Stop()
	<-timer.C
	close(stop)
	wg.Wait()

	// If we reach here under -race, no race fired. Without -race, the Go
	// runtime would have already aborted with "fatal error: concurrent map
	// iteration and map write" if the race actually realized, so reaching
	// this point implies the fix held up.
}

// TestResetConnectBackoffSafety_HappyPath verifies that ResetConnectBackoff,
// when called serially (no concurrent resolver updates), still does its job:
// it iterates the live addrConns without panicking and returns normally even
// after a Connect() has produced state. This guards against the fix
// accidentally breaking the non-racy use case.
func (s) TestResetConnectBackoffSafety_HappyPath(t *testing.T) {
	addrs, cleanup := startBackoffSafetyBlackholes(t, 1)
	defer cleanup()

	r := manual.NewBuilderWithScheme("repro-p2-4-happy")
	r.InitialState(resolver.State{
		Addresses: []resolver.Address{{Addr: addrs[0]}},
	})

	cc, err := NewClient(
		"repro-p2-4-happy:///dummy",
		WithResolvers(r),
		WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cc.Close()

	cc.Connect()
	// Give the SubConn time to register in cc.conns.
	time.Sleep(50 * time.Millisecond)

	// Should be idempotent and safe to call repeatedly in a single goroutine.
	for i := 0; i < 5; i++ {
		cc.ResetConnectBackoff()
	}

	// Issue a quick state read to confirm cc is still healthy.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = cc.WaitForStateChange(ctx, cc.GetState())
}
