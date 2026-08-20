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

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/resolver"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// writeHeapProfile dumps an inuse-space heap profile to path. Callers are
// expected to have just run runtime.GC() so the profile reflects live
// objects rather than transient allocations.
func writeHeapProfile(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create heap profile %s: %v", path, err)
	}
	defer f.Close()
	if err := pprof.WriteHeapProfile(f); err != nil {
		t.Fatalf("write heap profile %s: %v", path, err)
	}
}

// TestCloseRedialDoesNotRetainXDSState is a regression test for the xDS
// resolver / dependency-manager retention reported in
// googleapis/google-cloud-go#14582. Bigtable's connection recycler
// periodically calls Close() on one ClientConn dialed with an "xds:///"
// target while other channels sharing the same target stay open, then
// immediately redials a fresh channel in the closed slot. Because the
// xdsClient is refcounted per target, the other open channels pin the
// refcount above zero across every cycle — so the xdsClient is never
// destroyed, and any state it retains from subscribe/unsubscribe cycles
// (cluster watchers, callback-serializer callbacks, service-config parse
// results) accumulates cycle after cycle. Goroutine count stays flat;
// retained heap climbs monotonically. Full evidence — pprof heap
// snapshots, per-minute stats, symbol diff — lives at
// https://github.com/sushanb/bigtable-recycle-repro (60-minute sample:
// HeapSys 83 MiB → 447 MiB, +437%).
//
// This test reproduces that shape in-process: one persistent ClientConn
// pins the xdsClient (mirroring the "N-1 other channels stay open"
// invariant), while a second ClientConn is dialed, driven with one RPC,
// and closed on every iteration. Runtime HeapInuse is snapshotted before
// and after the stress loop; the test fails if the per-iteration growth
// exceeds a threshold well above normal GC slack.
//
// A cleanly-cleaning close+redial should be within a few KB / iter of
// noise. The observed retention in the standalone repro corresponds to
// roughly 60 KB per channel-recycle; the threshold below sits between
// those two.
//
// The test skips under -short since it needs many iterations to
// distinguish real retention from GC slack.
func (s) TestCloseRedialDoesNotRetainXDSState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heap-growth stress test under -short")
	}

	// Fake xDS management server and a real backend to serve RPCs on both
	// the persistent and the cycling channels. Both live for the entire
	// test; only the second (cycling) ClientConn is torn down and rebuilt
	// per iteration.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: serviceName,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// One shared xdsClient pool for the whole test, so every dial (both
	// the persistent channel and every cycled channel) subscribes/
	// unsubscribes against the same refcounted client. This mirrors the
	// production wiring, where every xds:/// resolver goes through
	// xdsclient.DefaultPool. NewXDSResolverWithConfigForTesting cannot be
	// used here — it builds a fresh pool per call, so each iteration
	// would destroy and recreate an independent xdsClient and the leak
	// surface (state accumulating inside a long-lived client) would never
	// engage.
	bootstrapConfig, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("bootstrap.NewConfigFromContents failed: %v", err)
	}
	sharedPool := xdsclient.NewPool(bootstrapConfig)
	newResolver := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))

	// buildAndConnect builds a fresh xds resolver against the shared
	// pool, dials xds:///, and blocks on a single RPC to force the
	// LDS→RDS→CDS→EDS graph to be fully materialized before returning.
	buildAndConnect := func(label string) *grpc.ClientConn {
		t.Helper()
		r, err := newResolver(sharedPool)
		if err != nil {
			t.Fatalf("%s: NewXDSResolverWithPoolForTesting failed: %v", label, err)
		}
		cc, err := grpc.NewClient("xds:///"+serviceName,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithResolvers(r),
		)
		if err != nil {
			t.Fatalf("%s: grpc.NewClient failed: %v", label, err)
		}
		client := testgrpc.NewTestServiceClient(cc)
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
			cc.Close()
			t.Fatalf("%s: EmptyCall failed: %v", label, err)
		}
		return cc
	}

	// Persistent channel: keeps the xdsClient pinned across all iterations
	// so the leaking codepath is the "cycle one of many" pattern from
	// bigtable, not a "destroy the last client" edge case that would exit
	// through a different cleanup path.
	persistent := buildAndConnect("persistent")
	defer persistent.Close()

	// Warm-up: first few cycling dials populate one-time singletons (TLS
	// machinery, resolver registry, etc.) that would otherwise show up as
	// growth inside the measured window.
	const warmup = 10
	for i := 0; i < warmup; i++ {
		cc := buildAndConnect("warmup")
		cc.Close()
	}

	// Give the runtime a chance to release anything reclaimable, then
	// snapshot the baseline.
	// Heap profiles are written to a persistent temp dir (NOT t.TempDir,
	// which is deleted before the test process exits) so reviewers can:
	//   go tool pprof -top -flat -inuse_space \
	//     -diff_base <baseline> <after>
	// to see exactly which grpc-go allocation sites accumulate.
	profileDir, err := os.MkdirTemp("", "xds-close-redial-leak-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	baselinePath := filepath.Join(profileDir, "heap-baseline.pb.gz")
	afterPath := filepath.Join(profileDir, "heap-after.pb.gz")

	runtime.GC()
	runtime.GC()
	var mBase runtime.MemStats
	runtime.ReadMemStats(&mBase)
	goroutinesBase := runtime.NumGoroutine()
	writeHeapProfile(t, baselinePath)

	// Stress loop. Higher iteration count helps push per-iter retention
	// above pprof's default 1%-of-total drop threshold so the diff shows
	// concrete grpc-go allocation sites.
	const iterations = 1000
	for i := 0; i < iterations; i++ {
		cc := buildAndConnect("cycle")
		cc.Close()
	}

	runtime.GC()
	runtime.GC()
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)
	goroutinesAfter := runtime.NumGoroutine()
	writeHeapProfile(t, afterPath)

	deltaInuse := int64(mAfter.HeapInuse) - int64(mBase.HeapInuse)
	deltaSys := int64(mAfter.HeapSys) - int64(mBase.HeapSys)
	perIterInuse := deltaInuse / int64(iterations)
	perIterSys := deltaSys / int64(iterations)

	t.Logf("heap after %d warm-up + %d stress iterations (persistent channel held throughout):",
		warmup, iterations)
	t.Logf("  HeapInuse: baseline=%d after=%d delta=%d (%d bytes/iter)",
		mBase.HeapInuse, mAfter.HeapInuse, deltaInuse, perIterInuse)
	t.Logf("  HeapSys  : baseline=%d after=%d delta=%d (%d bytes/iter)",
		mBase.HeapSys, mAfter.HeapSys, deltaSys, perIterSys)
	t.Logf("  goroutines: baseline=%d after=%d delta=%d",
		goroutinesBase, goroutinesAfter, goroutinesAfter-goroutinesBase)
	t.Logf("  heap profiles: baseline=%s after=%s", baselinePath, afterPath)
	t.Logf("  diff:   go tool pprof -top -flat -inuse_space -diff_base %s %s",
		baselinePath, afterPath)

	// A leak-free close+redial should be within a few KB / iter of noise.
	// Threshold set at 20 KiB / iter — comfortably above noise and
	// comfortably below the ~60 KB / iter observed in the standalone repro.
	const maxBytesPerIter = 20 * 1024
	if perIterInuse > maxBytesPerIter {
		t.Errorf("close+redial retained %d bytes/iter of HeapInuse over %d iterations (limit %d)",
			perIterInuse, iterations, maxBytesPerIter)
	}
}
