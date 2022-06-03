/*
 *
 * Copyright 2021 gRPC authors.
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

package rls

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	rlspb "google.golang.org/grpc/internal/proto/grpc_lookup_v1"
	rlstest "google.golang.org/grpc/internal/testutils/rls"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Test verifies the scenario where there is no matching entry in the data cache
// and no pending request either, and the ensuing RLS request is throttled.
func (s) TestPick_DataCacheMiss_NoPendingEntry_ThrottledWithDefaultTarget(t *testing.T) {
	// Start an RLS server and set the throttler to always throttle requests.
	rlsServer, rlsReqCh := rlstest.SetupFakeRLSServer(t, nil)
	overrideAdaptiveThrottler(t, alwaysThrottlingThrottler())

	// Build RLS service config with a default target.
	rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
	defBackendCh, defBackendAddress := startBackend(t)
	rlsConfig.RouteLookupConfig.DefaultTarget = defBackendAddress

	// Register a manual resolver and push the RLS service config through it.
	r := startManualResolverWithConfig(t, rlsConfig)

	cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Make an RPC and ensure it gets routed to the default target.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	makeTestRPCAndExpectItToReachBackend(ctx, t, cc, defBackendCh)

	// Make sure no RLS request is sent out.
	verifyRLSRequest(t, rlsReqCh, false)
}

// Test verifies the scenario where there is no matching entry in the data cache
// and no pending request either, and the ensuing RLS request is throttled.
// There is no default target configured in the service config, so the RPC is
// expected to fail with an RLS throttled error.
func (s) TestPick_DataCacheMiss_NoPendingEntry_ThrottledWithoutDefaultTarget(t *testing.T) {
	// Start an RLS server and set the throttler to always throttle requests.
	rlsServer, rlsReqCh := rlstest.SetupFakeRLSServer(t, nil)
	overrideAdaptiveThrottler(t, alwaysThrottlingThrottler())

	// Build an RLS config without a default target.
	rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)

	// Register a manual resolver and push the RLS service config through it.
	r := startManualResolverWithConfig(t, rlsConfig)

	// Dial the backend.
	cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Make an RPC and expect it to fail with RLS throttled error.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	makeTestRPCAndVerifyError(ctx, t, cc, codes.Unavailable, errRLSThrottled)

	// Make sure no RLS request is sent out.
	verifyRLSRequest(t, rlsReqCh, false)
}

// Test verifies the scenario where there is no matching entry in the data cache
// and no pending request either, and the ensuing RLS request is not throttled.
// The RLS response does not contain any backends, so the RPC fails with a
// deadline exceeded error.
func (s) TestPick_DataCacheMiss_NoPendingEntry_NotThrottled(t *testing.T) {
	// Start an RLS server and set the throttler to never throttle requests.
	rlsServer, rlsReqCh := rlstest.SetupFakeRLSServer(t, nil)
	overrideAdaptiveThrottler(t, neverThrottlingThrottler())

	// Build an RLS config without a default target.
	rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)

	// Register a manual resolver and push the RLS service config through it.
	r := startManualResolverWithConfig(t, rlsConfig)

	// Dial the backend.
	cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Make an RPC and expect it to fail with deadline exceeded error. We use a
	// smaller timeout to ensure that the test doesn't run very long.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	makeTestRPCAndVerifyError(ctx, t, cc, codes.DeadlineExceeded, context.DeadlineExceeded)

	// Make sure an RLS request is sent out.
	verifyRLSRequest(t, rlsReqCh, true)
}

// Test verifies the scenario where there is no matching entry in the data
// cache, but there is a pending request. So, we expect no RLS request to be
// sent out. The pick should be queued and not delegated to the default target.
func (s) TestPick_DataCacheMiss_PendingEntryExists(t *testing.T) {
	tests := []struct {
		name              string
		withDefaultTarget bool
	}{
		{
			name:              "withDefaultTarget",
			withDefaultTarget: true,
		},
		{
			name:              "withoutDefaultTarget",
			withDefaultTarget: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// A unary interceptor which blocks the RouteLookup RPC on the fake
			// RLS server until the test is done. The first RPC by the client
			// will cause the LB policy to send out an RLS request. This will
			// also lead to creation of a pending entry, and further RPCs by the
			// client should not result in RLS requests being sent out.
			rlsReqCh := make(chan struct{}, 1)
			interceptor := func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
				rlsReqCh <- struct{}{}
				<-ctx.Done()
				return nil, ctx.Err()
			}

			// Start an RLS server and set the throttler to never throttle.
			rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil, grpc.UnaryInterceptor(interceptor))
			overrideAdaptiveThrottler(t, neverThrottlingThrottler())

			// Build RLS service config with an optional default target.
			rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
			if test.withDefaultTarget {
				_, defBackendAddress := startBackend(t)
				rlsConfig.RouteLookupConfig.DefaultTarget = defBackendAddress
			}

			// Register a manual resolver and push the RLS service config
			// through it.
			r := startManualResolverWithConfig(t, rlsConfig)

			// Dial the backend.
			cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial() failed: %v", err)
			}
			defer cc.Close()

			// Make an RPC and expect it to fail with deadline exceeded error.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
			defer cancel()
			makeTestRPCAndVerifyError(ctx, t, cc, codes.DeadlineExceeded, context.DeadlineExceeded)

			// Make sure an RLS request is sent out.
			verifyRLSRequest(t, rlsReqCh, true)

			// Make another RPC and expect it to fail the same way.
			ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
			defer cancel()
			makeTestRPCAndVerifyError(ctx, t, cc, codes.DeadlineExceeded, context.DeadlineExceeded)

			// Make sure no RLS request is sent out this time around.
			verifyRLSRequest(t, rlsReqCh, false)
		})
	}
}

// Test verifies the scenario where there is a matching entry in the data cache
// which is valid and there is no pending request. The pick is expected to be
// delegated to the child policy.
func (s) TestPick_DataCacheHit_NoPendingEntry_ValidEntry(t *testing.T) {
	// Start an RLS server and set the throttler to never throttle requests.
	rlsServer, rlsReqCh := rlstest.SetupFakeRLSServer(t, nil)
	overrideAdaptiveThrottler(t, neverThrottlingThrottler())

	// Build the RLS config without a default target.
	rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)

	// Start a test backend, and setup the fake RLS server to return this as a
	// target in the RLS response.
	testBackendCh, testBackendAddress := startBackend(t)
	rlsServer.SetResponseCallback(func(_ context.Context, req *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
		return &rlstest.RouteLookupResponse{Resp: &rlspb.RouteLookupResponse{Targets: []string{testBackendAddress}}}
	})

	// Register a manual resolver and push the RLS service config through it.
	r := startManualResolverWithConfig(t, rlsConfig)

	// Dial the backend.
	cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Make an RPC and ensure it gets routed to the test backend.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

	// Make sure an RLS request is sent out.
	verifyRLSRequest(t, rlsReqCh, true)

	// Make another RPC and expect it to find the target in the data cache.
	makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

	// Make sure no RLS request is sent out this time around.
	verifyRLSRequest(t, rlsReqCh, false)
}

// Test verifies the scenario where there is a matching entry in the data cache
// which is stale and there is no pending request. The pick is expected to be
// delegated to the child policy with a proactive cache refresh.
func (s) TestPick_DataCacheHit_NoPendingEntry_StaleEntry(t *testing.T) {
	// We expect the same pick behavior (i.e delegated to the child policy) for
	// a proactive refresh whether the control channel is throttled or not.
	tests := []struct {
		name      string
		throttled bool
	}{
		{
			name:      "throttled",
			throttled: true,
		},
		{
			name:      "notThrottled",
			throttled: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Start an RLS server and setup the throttler appropriately.
			rlsServer, rlsReqCh := rlstest.SetupFakeRLSServer(t, nil)
			var throttler *fakeThrottler
			if test.throttled {
				throttler = oneTimeAllowingThrottler()
				overrideAdaptiveThrottler(t, throttler)
			} else {
				throttler = neverThrottlingThrottler()
				overrideAdaptiveThrottler(t, throttler)
			}

			// Build the RLS config without a default target. Set the stale age
			// to a very low value to force entries to become stale quickly.
			rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
			rlsConfig.RouteLookupConfig.MaxAge = durationpb.New(time.Minute)
			rlsConfig.RouteLookupConfig.StaleAge = durationpb.New(defaultTestShortTimeout)

			// Start a test backend, and setup the fake RLS server to return
			// this as a target in the RLS response.
			testBackendCh, testBackendAddress := startBackend(t)
			rlsServer.SetResponseCallback(func(_ context.Context, req *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
				return &rlstest.RouteLookupResponse{Resp: &rlspb.RouteLookupResponse{Targets: []string{testBackendAddress}}}
			})

			// Register a manual resolver and push the RLS service config
			// through it.
			r := startManualResolverWithConfig(t, rlsConfig)

			// Dial the backend.
			cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial() failed: %v", err)
			}
			defer cc.Close()

			// Make an RPC and ensure it gets routed to the test backend.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

			// Make sure an RLS request is sent out.
			verifyRLSRequest(t, rlsReqCh, true)

			// The cache entry has a large maxAge, but a small stateAge. We keep
			// retrying until the cache entry becomes stale, in which case we expect a
			// proactive cache refresh.
			//
			// If the control channel is not throttled, then we expect an RLS request
			// to be sent out. If the control channel is throttled, we expect the fake
			// throttler's channel to be signalled.
			for {
				// Make another RPC and expect it to find the target in the data cache.
				makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

				if !test.throttled {
					select {
					case <-time.After(defaultTestShortTimeout):
						// Go back and retry the RPC.
					case <-rlsReqCh:
						return
					}
				} else {
					select {
					case <-time.After(defaultTestShortTimeout):
						// Go back and retry the RPC.
					case <-throttler.throttleCh:
						return
					}
				}
			}
		})
	}
}

// Test verifies scenarios where there is a matching entry in the data cache
// which has expired and there is no pending request.
func (s) TestPick_DataCacheHit_NoPendingEntry_ExpiredEntry(t *testing.T) {
	tests := []struct {
		name              string
		throttled         bool
		withDefaultTarget bool
	}{
		{
			name:              "throttledWithDefaultTarget",
			throttled:         true,
			withDefaultTarget: true,
		},
		{
			name:              "throttledWithoutDefaultTarget",
			throttled:         true,
			withDefaultTarget: false,
		},
		{
			name:      "notThrottled",
			throttled: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Start an RLS server and setup the throttler appropriately.
			rlsServer, rlsReqCh := rlstest.SetupFakeRLSServer(t, nil)
			var throttler *fakeThrottler
			if test.throttled {
				throttler = oneTimeAllowingThrottler()
				overrideAdaptiveThrottler(t, throttler)
			} else {
				throttler = neverThrottlingThrottler()
				overrideAdaptiveThrottler(t, throttler)
			}

			// Build the RLS config with a very low value for maxAge. This will
			// ensure that cache entries become invalid very soon.
			rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
			rlsConfig.RouteLookupConfig.MaxAge = durationpb.New(defaultTestShortTimeout)

			// Start a default backend if needed.
			var defBackendCh chan struct{}
			if test.withDefaultTarget {
				var defBackendAddress string
				defBackendCh, defBackendAddress = startBackend(t)
				rlsConfig.RouteLookupConfig.DefaultTarget = defBackendAddress
			}

			// Start a test backend, and setup the fake RLS server to return
			// this as a target in the RLS response.
			testBackendCh, testBackendAddress := startBackend(t)
			rlsServer.SetResponseCallback(func(_ context.Context, req *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
				return &rlstest.RouteLookupResponse{Resp: &rlspb.RouteLookupResponse{Targets: []string{testBackendAddress}}}
			})

			// Register a manual resolver and push the RLS service config
			// through it.
			r := startManualResolverWithConfig(t, rlsConfig)

			// Dial the backend.
			cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial() failed: %v", err)
			}
			defer cc.Close()

			// Make an RPC and ensure it gets routed to the test backend.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

			// Make sure an RLS request is sent out.
			verifyRLSRequest(t, rlsReqCh, true)

			// Keep retrying the RPC until the cache entry expires. Expected behavior
			// is dependent on the scenario being tested.
			switch {
			case test.throttled && test.withDefaultTarget:
				makeTestRPCAndExpectItToReachBackend(ctx, t, cc, defBackendCh)
				<-throttler.throttleCh
			case test.throttled && !test.withDefaultTarget:
				makeTestRPCAndVerifyError(ctx, t, cc, codes.Unavailable, errRLSThrottled)
				<-throttler.throttleCh
			case !test.throttled:
				for {
					// The backend to which the RPC is routed does not change after the
					// cache entry expires because the control channel is not throttled.
					// So, we need to keep retrying until the cache entry expires, at
					// which point we expect an RLS request to be sent out and the RPC to
					// get routed to the same testBackend.
					makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)
					select {
					case <-time.After(defaultTestShortTimeout):
						// Go back and retry the RPC.
					case <-rlsReqCh:
						return
					}
				}
			}
		})
	}
}

// Test verifies scenarios where there is a matching entry in the data cache
// which has expired and is in backoff and there is no pending request.
func (s) TestPick_DataCacheHit_NoPendingEntry_ExpiredEntryInBackoff(t *testing.T) {
	tests := []struct {
		name              string
		withDefaultTarget bool
	}{
		{
			name:              "withDefaultTarget",
			withDefaultTarget: true,
		},
		{
			name:              "withoutDefaultTarget",
			withDefaultTarget: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Start an RLS server and set the throttler to never throttle requests.
			rlsServer, rlsReqCh := rlstest.SetupFakeRLSServer(t, nil)
			overrideAdaptiveThrottler(t, neverThrottlingThrottler())

			// Override the backoff strategy to return a large backoff which
			// will make sure the date cache entry remains in backoff for the
			// duration of the test.
			origBackoffStrategy := defaultBackoffStrategy
			defaultBackoffStrategy = &fakeBackoffStrategy{backoff: defaultTestTimeout}
			defer func() { defaultBackoffStrategy = origBackoffStrategy }()

			// Build the RLS config with a very low value for maxAge. This will
			// ensure that cache entries become invalid very soon.
			rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
			rlsConfig.RouteLookupConfig.MaxAge = durationpb.New(defaultTestShortTimeout)

			// Start a default backend if needed.
			var defBackendCh chan struct{}
			if test.withDefaultTarget {
				var defBackendAddress string
				defBackendCh, defBackendAddress = startBackend(t)
				rlsConfig.RouteLookupConfig.DefaultTarget = defBackendAddress
			}

			// Start a test backend, and set up the fake RLS server to return this as
			// a target in the RLS response.
			testBackendCh, testBackendAddress := startBackend(t)
			rlsServer.SetResponseCallback(func(_ context.Context, req *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
				return &rlstest.RouteLookupResponse{Resp: &rlspb.RouteLookupResponse{Targets: []string{testBackendAddress}}}
			})

			// Register a manual resolver and push the RLS service config through it.
			r := startManualResolverWithConfig(t, rlsConfig)

			// Dial the backend.
			cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial() failed: %v", err)
			}
			defer cc.Close()

			// Make an RPC and ensure it gets routed to the test backend.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

			// Make sure an RLS request is sent out.
			verifyRLSRequest(t, rlsReqCh, true)

			// Set up the fake RLS server to return errors. This will push the cache
			// entry into backoff.
			var rlsLastErr = status.Error(codes.DeadlineExceeded, "last RLS request failed")
			rlsServer.SetResponseCallback(func(_ context.Context, req *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
				return &rlstest.RouteLookupResponse{Err: rlsLastErr}
			})

			// Since the RLS server is now configured to return errors, this will push
			// the cache entry into backoff. The pick will be delegated to the default
			// backend if one exits, and will fail with the error returned by the RLS
			// server otherwise.
			if test.withDefaultTarget {
				makeTestRPCAndExpectItToReachBackend(ctx, t, cc, defBackendCh)
			} else {
				makeTestRPCAndVerifyError(ctx, t, cc, codes.Unavailable, rlsLastErr)
			}
		})
	}
}

// Test verifies scenarios where there is a matching entry in the data cache
// which is stale and there is a pending request.
func (s) TestPick_DataCacheHit_PendingEntryExists_StaleEntry(t *testing.T) {
	tests := []struct {
		name              string
		withDefaultTarget bool
	}{
		{
			name:              "withDefaultTarget",
			withDefaultTarget: true,
		},
		{
			name:              "withoutDefaultTarget",
			withDefaultTarget: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// A unary interceptor which does nothing on the first RPC, but
			// blocks on subsequent RPCs on the fake RLS server until the test
			// is done. Since we configure the LB policy with a really low value
			// for stale age, this allows us to simulate the condition where the
			// LB policy has a stale entry and a pending entry in the cache.
			rlsReqCh := make(chan struct{}, 1)
			i := 0
			interceptor := func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
				rlsReqCh <- struct{}{}
				if i == 0 {
					i++
					return handler(ctx, req)
				}
				<-ctx.Done()
				return nil, ctx.Err()
			}

			// Start an RLS server and set the throttler to never throttle.
			rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil, grpc.UnaryInterceptor(interceptor))
			overrideAdaptiveThrottler(t, neverThrottlingThrottler())

			// Build RLS service config with an optional default target.
			rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
			if test.withDefaultTarget {
				_, defBackendAddress := startBackend(t)
				rlsConfig.RouteLookupConfig.DefaultTarget = defBackendAddress
			}

			// Low value for stale age to force entries to become stale quickly.
			rlsConfig.RouteLookupConfig.MaxAge = durationpb.New(time.Minute)
			rlsConfig.RouteLookupConfig.StaleAge = durationpb.New(defaultTestShortTimeout)

			// Start a test backend, and setup the fake RLS server to return
			// this as a target in the RLS response.
			testBackendCh, testBackendAddress := startBackend(t)
			rlsServer.SetResponseCallback(func(_ context.Context, req *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
				return &rlstest.RouteLookupResponse{Resp: &rlspb.RouteLookupResponse{Targets: []string{testBackendAddress}}}
			})

			// Register a manual resolver and push the RLS service config
			// through it.
			r := startManualResolverWithConfig(t, rlsConfig)

			// Dial the backend.
			cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial() failed: %v", err)
			}
			defer cc.Close()

			// Make an RPC and ensure it gets routed to the test backend.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

			// Make sure an RLS request is sent out.
			verifyRLSRequest(t, rlsReqCh, true)

			// The cache entry has a large maxAge, but a small stateAge. We keep
			// retrying until the cache entry becomes stale, in which case we expect a
			// proactive cache refresh.
			for {
				makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

				select {
				case <-time.After(defaultTestShortTimeout):
					// Go back and retry the RPC.
				case <-rlsReqCh:
					return
				}
			}
		})
	}
}

// Test verifies scenarios where there is a matching entry in the data cache
// which is expired and there is a pending request.
func (s) TestPick_DataCacheHit_PendingEntryExists_ExpiredEntry(t *testing.T) {
	tests := []struct {
		name              string
		withDefaultTarget bool
	}{
		{
			name:              "withDefaultTarget",
			withDefaultTarget: true,
		},
		{
			name:              "withoutDefaultTarget",
			withDefaultTarget: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// A unary interceptor which does nothing on the first RPC, but
			// blocks on subsequent RPCs on the fake RLS server until the test
			// is done. And since we configure the LB policy with a really low
			// value for max age, this allows us to simulate the condition where
			// the LB policy has an expired entry and a pending entry in the
			// cache.
			rlsReqCh := make(chan struct{}, 1)
			i := 0
			interceptor := func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
				rlsReqCh <- struct{}{}
				if i == 0 {
					i++
					return handler(ctx, req)
				}
				<-ctx.Done()
				return nil, ctx.Err()
			}

			// Start an RLS server and set the throttler to never throttle.
			rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil, grpc.UnaryInterceptor(interceptor))
			overrideAdaptiveThrottler(t, neverThrottlingThrottler())

			// Build RLS service config with an optional default target.
			rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
			if test.withDefaultTarget {
				_, defBackendAddress := startBackend(t)
				rlsConfig.RouteLookupConfig.DefaultTarget = defBackendAddress
			}
			// Set a low value for maxAge to ensure cache entries expire soon.
			rlsConfig.RouteLookupConfig.MaxAge = durationpb.New(defaultTestShortTimeout)

			// Start a test backend, and setup the fake RLS server to return
			// this as a target in the RLS response.
			testBackendCh, testBackendAddress := startBackend(t)
			rlsServer.SetResponseCallback(func(_ context.Context, req *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
				return &rlstest.RouteLookupResponse{Resp: &rlspb.RouteLookupResponse{Targets: []string{testBackendAddress}}}
			})

			// Register a manual resolver and push the RLS service config
			// through it.
			r := startManualResolverWithConfig(t, rlsConfig)

			// Dial the backend.
			cc, err := grpc.Dial(r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial() failed: %v", err)
			}
			defer cc.Close()

			// Make an RPC and ensure it gets routed to the test backend.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			makeTestRPCAndExpectItToReachBackend(ctx, t, cc, testBackendCh)

			// Make sure an RLS request is sent out.
			verifyRLSRequest(t, rlsReqCh, true)

			// At this point, we have a cache entry with a small maxAge, and the RLS
			// server is configured to block on further RLS requests. As we retry the
			// RPC, at some point the cache entry would expire and force us to send an
			// RLS request. But this request would exceed the deadline since the
			// server blocks.
			makeTestRPCAndVerifyError(ctx, t, cc, codes.DeadlineExceeded, context.DeadlineExceeded)
			verifyRLSRequest(t, rlsReqCh, true)

			// Another RPC at this point should find the pending entry and be queued.
			// But since we pass a small deadline, this RPC should fail with a
			// deadline exceeded error since the pending request does not return until
			// the test is done. And since we have a pending entry, we expect no RLS
			// request to be sent out.
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			makeTestRPCAndVerifyError(sCtx, t, cc, codes.DeadlineExceeded, context.DeadlineExceeded)
			verifyRLSRequest(t, rlsReqCh, false)
		})
	}
}

func TestIsFullMethodNameValid(t *testing.T) {
	tests := []struct {
		desc       string
		methodName string
		want       bool
	}{
		{
			desc:       "does not start with a slash",
			methodName: "service/method",
			want:       false,
		},
		{
			desc:       "does not contain a method",
			methodName: "/service",
			want:       false,
		},
		{
			desc:       "path has more elements",
			methodName: "/service/path/to/method",
			want:       false,
		},
		{
			desc:       "valid",
			methodName: "/service/method",
			want:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			if got := isFullMethodNameValid(test.methodName); got != test.want {
				t.Fatalf("isFullMethodNameValid(%q) = %v, want %v", test.methodName, got, test.want)
			}
		})
	}
}
