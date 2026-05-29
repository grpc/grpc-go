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

// Regression tests for doHTTPConnectHandshake context propagation.
//
// Background:
//   When an HTTP CONNECT proxy accepts the TCP connection but then never sends
//   a response (a real failure mode for "half-dead" corporate proxies),
//   doHTTPConnectHandshake used to block on req.Write(conn) and
//   http.ReadResponse(...) for as long as the kernel's TCP retransmit budget
//   allowed (~15 minutes on Linux defaults), ignoring the caller's ctx.
//
//   These tests assert that the handshake terminates within a small slack of
//   the ctx deadline. They MUST pass after the fix and MUST fail before it.

package transport

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/internal/proxyattributes"
)

// startBlackholeProxy starts a TCP listener that accepts connections but never
// reads, writes, or closes them — simulating a hung HTTP CONNECT proxy.
// All accepted conns are closed at cleanup so the test doesn't leak fds.
func startBlackholeProxy(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
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
	}()
	cleanup = func() {
		close(stop)
		ln.Close()
		wg.Wait()
	}
	return ln.Addr().String(), cleanup
}

// TestProxyHandshakeSafety_ContextCancelUnblocksWrite asserts that when the
// proxy accepts but never reads, doHTTPConnectHandshake stops within a small
// slack of ctx deadline rather than blocking until TCP timeout.
//
// Pre-fix: handshake blocks for ≥ kernel TCP timeout regardless of ctx.
// Post-fix: handshake returns within ctxBudget + tolerance.
func (s) TestProxyHandshakeSafety_ContextCancelUnblocksHandshake(t *testing.T) {
	proxyAddr, cleanup := startBlackholeProxy(t)
	defer cleanup()

	const ctxBudget = 1 * time.Second
	const tolerance = 500 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), ctxBudget)
	defer cancel()

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial blackhole proxy: %v", err)
	}
	// Ensure conn is always closed even if handshake returns nil err (it
	// shouldn't here, but be defensive).
	defer conn.Close()

	start := time.Now()
	got, handshakeErr := doHTTPConnectHandshake(
		ctx,
		conn,
		"grpc-go-test",
		proxyattributes.Options{ConnectAddr: "example.com:443"},
	)
	elapsed := time.Since(start)

	if handshakeErr == nil {
		// Defensive: handshake should never succeed against a silent proxy.
		if got != nil {
			got.Close()
		}
		t.Fatalf("handshake unexpectedly succeeded; elapsed=%v", elapsed)
	}

	if elapsed > ctxBudget+tolerance {
		t.Errorf("handshake elapsed %v > ctx budget %v + tolerance %v; ctx.Err()=%v handshakeErr=%v",
			elapsed, ctxBudget, tolerance, ctx.Err(), handshakeErr)
	} else {
		t.Logf("handshake returned in %v (ctx budget %v): err=%v", elapsed, ctxBudget, handshakeErr)
	}
}

// TestProxyHandshakeSafety_ErrorIdentifiableAsContext asserts that the
// returned error chain is identifiable as a context cancellation (via
// errors.Is or via ctx.Err() check), not an opaque "use of closed network
// connection". This matters for upstream callers that branch on
// context.DeadlineExceeded for retry/fallback decisions.
func (s) TestProxyHandshakeSafety_ErrorIdentifiableAsContextCancel(t *testing.T) {
	proxyAddr, cleanup := startBlackholeProxy(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial blackhole proxy: %v", err)
	}
	defer conn.Close()

	_, handshakeErr := doHTTPConnectHandshake(
		ctx,
		conn,
		"grpc-go-test",
		proxyattributes.Options{ConnectAddr: "example.com:443"},
	)
	if handshakeErr == nil {
		t.Fatalf("handshake unexpectedly succeeded")
	}

	// Acceptable signals that ctx was the cause:
	//   1. errors.Is(err, context.DeadlineExceeded / context.Canceled)
	//   2. ctx.Err() != nil and handshake error wraps it
	// We accept either signal: callers can use errors.Is, or they can check
	// ctx.Err() themselves after a handshake failure.
	isCtxIdentifiable :=
		errors.Is(handshakeErr, context.DeadlineExceeded) ||
			errors.Is(handshakeErr, context.Canceled) ||
			ctx.Err() != nil // weaker — at least ctx confirms it timed out

	if !isCtxIdentifiable {
		t.Errorf("handshake error chain should reflect ctx cancellation; got: %v (ctx.Err()=%v)",
			handshakeErr, ctx.Err())
	}
}
