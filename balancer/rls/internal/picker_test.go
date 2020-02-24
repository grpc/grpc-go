// +build go1.10

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

package rls

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/rls/internal/cache"
	"google.golang.org/grpc/balancer/rls/internal/keys"
	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/metadata"
)

const (
	defaultTestTimeout = 1 * time.Second
	maxAge             = 5 * time.Second
)

func initKeyBuilderMap(t *testing.T) keys.BuilderMap {
	t.Helper()

	kb1 := &rlspb.GrpcKeyBuilder{
		Names:   []*rlspb.GrpcKeyBuilder_Name{{Service: "gFoo"}},
		Headers: []*rlspb.NameMatcher{{Key: "k1", Names: []string{"n1"}}},
	}
	kb2 := &rlspb.GrpcKeyBuilder{
		Names:   []*rlspb.GrpcKeyBuilder_Name{{Service: "gBar", Method: "method1"}},
		Headers: []*rlspb.NameMatcher{{Key: "k2", Names: []string{"n21", "n22"}}},
	}
	kb3 := &rlspb.GrpcKeyBuilder{
		Names:   []*rlspb.GrpcKeyBuilder_Name{{Service: "gFoobar"}},
		Headers: []*rlspb.NameMatcher{{Key: "k3", Names: []string{"n3"}}},
	}
	kbm, err := keys.MakeBuilderMap(&rlspb.RouteLookupConfig{
		GrpcKeybuilders: []*rlspb.GrpcKeyBuilder{kb1, kb2, kb3},
	})
	if err != nil {
		t.Fatalf("Failed to create keyBuilderMap: %v", err)
	}
	return kbm
}

type fakeChildPicker struct {
	pickCh     chan struct{}
	pickResult balancer.PickResult
	err        error
}

func (p *fakeChildPicker) Pick(_ balancer.PickInfo) (balancer.PickResult, error) {
	p.pickCh <- struct{}{}
	return p.pickResult, p.err
}

// TestPickKeyBuilder verifies the different possible scenarios for
// forming an RLS key for an incoming RPC.
func TestPickKeyBuilder(t *testing.T) {
	kbm := initKeyBuilderMap(t)

	tests := []struct {
		desc       string
		rpcPath    string
		md         metadata.MD
		wantKey    cache.Key
		wantResult balancer.PickResult
	}{
		{
			desc:       "non existent service in keyBuilder map",
			rpcPath:    "/gNonExistentService/method",
			md:         metadata.New(map[string]string{"n1": "v1", "n3": "v3"}),
			wantKey:    cache.Key{Path: "/gNonExistentService/method", KeyMap: ""},
			wantResult: balancer.PickResult{},
		},
		{
			desc:       "no metadata in incoming context",
			rpcPath:    "/gFoo/method",
			md:         metadata.MD{},
			wantKey:    cache.Key{Path: "/gFoo/method", KeyMap: ""},
			wantResult: balancer.PickResult{},
		},
		{
			desc:       "keyBuilderMatch",
			rpcPath:    "/gFoo/method",
			md:         metadata.New(map[string]string{"n1": "v1", "n3": "v3"}),
			wantKey:    cache.Key{Path: "/gFoo/method", KeyMap: "k1=v1"},
			wantResult: balancer.PickResult{},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			cp := &fakeChildPicker{
				pickCh:     make(chan struct{}, 1),
				pickResult: balancer.PickResult{},
				err:        nil,
			}
			p := picker{
				kbm:      kbm,
				strategy: rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
				readCache: func(key cache.Key) (*cache.Entry, bool) {
					if !cmp.Equal(key, test.wantKey) {
						t.Fatalf("picker using cacheKey %v, want %v", key, test.wantKey)
					}

					now := time.Now()
					return &cache.Entry{
						ExpiryTime: now.Add(maxAge),
						StaleTime:  now.Add(maxAge),
						// Cache entry is configured with a child policy whose
						// picker always returns an empty PickResult and nil
						// error.
						ChildPicker: cp,
					}, false
				},
				// The other hooks are not set here because they are not expected to be
				// invoked for these cases and if they get invoked, they will panic.
			}

			gotResult, err := p.Pick(balancer.PickInfo{
				FullMethodName: test.rpcPath,
				Ctx:            metadata.NewOutgoingContext(context.Background(), test.md),
			})
			if err != nil {
				t.Fatalf("Pick() failed with error: %v", err)
			}
			if !cmp.Equal(gotResult, test.wantResult) {
				t.Fatalf("Pick() returned %v, want %v", gotResult, test.wantResult)
			}

			timer := time.NewTimer(defaultTestTimeout)
			select {
			case <-cp.pickCh:
				timer.Stop()
			case <-timer.C:
				t.Fatal("Timeout waiting for Pick to be called on child policy")
			}
		})
	}
}

func TestPick(t *testing.T) {
	const (
		rpcPath       = "/gFoo/method"
		wantKeyMapStr = "k1=v1"
	)
	kbm := initKeyBuilderMap(t)
	md := metadata.New(map[string]string{"n1": "v1", "n3": "v3"})
	wantKey := cache.Key{Path: rpcPath, KeyMap: wantKeyMapStr}
	rlsLastErr := errors.New("last RLS request failed")

	tests := []struct {
		desc string
		// The cache entry, as returned by the overridden readCache hook.
		cacheEntry *cache.Entry
		// Whether or not a pending entry exists, as returned by the overridden
		// readCache hook.
		pending bool
		// Whether or not the RLS request should be throttled.
		throttle bool
		// Whether or not the test is expected to make a new RLS request.
		newRLSRequest bool
		// Whether or not the test ends up delegating to the default pick.
		useDefaultPick bool
		// Whether or not the test ends up delegating to the child policy in
		// the cache entry.
		useChildPick bool
		// Request processing strategy as used by the picker.
		strategy rlspb.RouteLookupConfig_RequestProcessingStrategy
		// Expected pick result returned by the picker under test.
		wantResult balancer.PickResult
		// Expected error returned by the picker under test.
		wantErr error
	}{
		{
			desc:       "cacheMiss_pending_defaultTargetOnError",
			pending:    true,
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult: balancer.PickResult{},
			wantErr:    balancer.ErrNoSubConnAvailable,
		},
		{
			desc:       "cacheMiss_pending_clientSeesError",
			pending:    true,
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult: balancer.PickResult{},
			wantErr:    balancer.ErrNoSubConnAvailable,
		},
		{
			desc:           "cacheMiss_pending_defaultTargetOnMiss",
			pending:        true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:          "cacheMiss_noPending_notThrottled_defaultTargetOnError",
			newRLSRequest: true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:    balancer.PickResult{},
			wantErr:       balancer.ErrNoSubConnAvailable,
		},
		{
			desc:          "cacheMiss_noPending_notThrottled_clientSeesError",
			newRLSRequest: true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult:    balancer.PickResult{},
			wantErr:       balancer.ErrNoSubConnAvailable,
		},
		{
			desc:           "cacheMiss_noPending_notThrottled_defaultTargetOnMiss",
			newRLSRequest:  true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:           "cacheMiss_noPending_throttled_defaultTargetOnError",
			throttle:       true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:       "cacheMiss_noPending_throttled_clientSeesError",
			throttle:   true,
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult: balancer.PickResult{},
			wantErr:    errRLSThrottled,
		},
		{
			desc:           "cacheMiss_noPending_throttled_defaultTargetOnMiss",
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			throttle:       true,
			useDefaultPick: true,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:           "cacheHit_noPending_boExpired_dataExpired_throttled_defaultTargetOnError",
			cacheEntry:     &cache.Entry{}, // Everything is expired in this entry
			throttle:       true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:       "cacheHit_noPending_boExpired_dataExpired_throttled_clientSeesError",
			cacheEntry: &cache.Entry{}, // Everything is expired in this entry
			throttle:   true,
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult: balancer.PickResult{},
			wantErr:    errRLSThrottled,
		},
		{
			desc:           "cacheHit_noPending_boExpired_dataExpired_throttled_defaultTargetOnMiss",
			cacheEntry:     &cache.Entry{}, // Everything is expired in this entry
			throttle:       true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:         "cacheHit_noPending_stale_boExpired_dataNotExpired_throttled_defaultTargetOnMiss",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			throttle:     true, // Proactive refresh is throttled.
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc:         "cacheHit_noPending_stale_boExpired_dataNotExpired_throttled_clientSeesError",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			throttle:     true, // Proactive refresh is throttled.
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc:         "cacheHit_noPending_stale_boExpired_dataNotExpired_throttled_defaultTargetOnError",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			throttle:     true, // Proactive refresh is throttled.
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc:          "cacheHit_noPending_boExpired_dataExpired_notThrottled_defaultTargetOnError",
			cacheEntry:    &cache.Entry{}, // Everything is expired in this entry
			newRLSRequest: true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:    balancer.PickResult{},
			wantErr:       balancer.ErrNoSubConnAvailable,
		},
		{
			desc:          "cacheHit_noPending_boExpired_dataExpired_notThrottled_clientSeesError",
			cacheEntry:    &cache.Entry{}, // Everything is expired in this entry
			newRLSRequest: true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult:    balancer.PickResult{},
			wantErr:       balancer.ErrNoSubConnAvailable,
		},
		{
			desc:           "cacheHit_noPending_boExpired_dataExpired_notThrottled_defaultTargetOnMiss",
			cacheEntry:     &cache.Entry{}, // Everything is expired in this entry
			newRLSRequest:  true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:          "cacheHit_noPending_stale_boExpired_dataNotExpired_notThrottled_defaultTargetOnMiss",
			cacheEntry:    &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			newRLSRequest: true, // Proactive refresh.
			useChildPick:  true,
			strategy:      rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:    balancer.PickResult{},
			wantErr:       nil,
		},
		{
			desc:          "cacheHit_noPending_stale_boExpired_dataNotExpired_notThrottled_clientSeesError",
			cacheEntry:    &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			newRLSRequest: true, // Proactive refresh.
			useChildPick:  true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult:    balancer.PickResult{},
			wantErr:       nil,
		},
		{
			desc:          "cacheHit_noPending_stale_boExpired_dataNotExpired_notThrottled_defaultTargetOnError",
			cacheEntry:    &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			newRLSRequest: true, // Proactive refresh.
			useChildPick:  true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:    balancer.PickResult{},
			wantErr:       nil,
		},
		{
			desc:           "cacheHit_noPending_stale_boNotExpired_dataExpired_defaultTargetOnError",
			cacheEntry:     &cache.Entry{BackoffTime: time.Now().Add(maxAge)},
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:           "cacheHit_noPending_stale_boNotExpired_dataExpired_defaultTargetOnMiss",
			cacheEntry:     &cache.Entry{BackoffTime: time.Now().Add(maxAge)},
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc: "cacheHit_noPending_stale_boNotExpired_dataExpired_clientSeesError",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(maxAge),
				CallStatus:  rlsLastErr,
			},
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult: balancer.PickResult{},
			wantErr:    rlsLastErr,
		},
		{
			desc: "cacheHit_noPending_stale_boNotExpired_dataNotExpired_defaultTargetOnError",
			cacheEntry: &cache.Entry{
				ExpiryTime:  time.Now().Add(maxAge),
				BackoffTime: time.Now().Add(maxAge),
				CallStatus:  rlsLastErr,
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_stale_boNotExpired_dataNotExpired_defaultTargetOnMiss",
			cacheEntry: &cache.Entry{
				ExpiryTime:  time.Now().Add(maxAge),
				BackoffTime: time.Now().Add(maxAge),
				CallStatus:  rlsLastErr,
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_stale_boNotExpired_dataNotExpired_clientSeesError",
			cacheEntry: &cache.Entry{
				ExpiryTime:  time.Now().Add(maxAge),
				BackoffTime: time.Now().Add(maxAge),
				CallStatus:  rlsLastErr,
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_notStale_dataNotExpired_defaultTargetOnError",
			cacheEntry: &cache.Entry{
				ExpiryTime: time.Now().Add(maxAge),
				StaleTime:  time.Now().Add(maxAge),
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_notStale_dataNotExpired_defaultTargetOnMiss",
			cacheEntry: &cache.Entry{
				ExpiryTime: time.Now().Add(maxAge),
				StaleTime:  time.Now().Add(maxAge),
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_notStale_dataNotExpired_clientSeesError",
			cacheEntry: &cache.Entry{
				ExpiryTime: time.Now().Add(maxAge),
				StaleTime:  time.Now().Add(maxAge),
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc:       "cacheHit_pending_dataExpired_boExpired_defaultTargetOnError",
			cacheEntry: &cache.Entry{},
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult: balancer.PickResult{},
			wantErr:    balancer.ErrNoSubConnAvailable,
		},
		{
			desc:           "cacheHit_pending_dataExpired_boExpired_defaultTargetOnMiss",
			cacheEntry:     &cache.Entry{},
			pending:        true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc:       "cacheHit_pending_dataExpired_boExpired_clientSeesError",
			cacheEntry: &cache.Entry{},
			pending:    true,
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult: balancer.PickResult{},
			wantErr:    balancer.ErrNoSubConnAvailable,
		},
		{
			desc: "cacheHit_pending_dataExpired_boNotExpired_defaultTargetOnError",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(maxAge),
				CallStatus:  rlsLastErr,
			},
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc: "cacheHit_pending_dataExpired_boNotExpired_defaultTargetOnMiss",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(maxAge),
				CallStatus:  rlsLastErr,
			},
			pending:        true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:     balancer.PickResult{},
			wantErr:        nil,
		},
		{
			desc: "cacheHit_pending_dataExpired_boNotExpired_clientSeesError",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(maxAge),
				CallStatus:  rlsLastErr,
			},
			pending:    true,
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult: balancer.PickResult{},
			wantErr:    rlsLastErr,
		},
		{
			desc:         "cacheHit_pending_dataNotExpired_defaultTargetOnError",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			pending:      true,
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc:         "cacheHit_pending_dataNotExpired_defaultTargetOnMiss",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			pending:      true,
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
		{
			desc:         "cacheHit_pending_dataNotExpired_clientSeesError",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(maxAge)},
			pending:      true,
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantResult:   balancer.PickResult{},
			wantErr:      nil,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			rlsCh := make(chan error, 1)
			defaultPickCh := make(chan error, 1)

			// Fake the child picker in the cache entry with one which pushes
			// on a channel when it is invoked.
			var childPicker *fakeChildPicker
			if test.useChildPick {
				childPicker = &fakeChildPicker{
					pickCh:     make(chan struct{}, 1),
					pickResult: balancer.PickResult{},
					err:        nil,
				}
			}

			p := picker{
				kbm:      kbm,
				strategy: test.strategy,
				readCache: func(key cache.Key) (*cache.Entry, bool) {
					if !cmp.Equal(key, wantKey) {
						t.Fatalf("cache lookup using cacheKey %v, want %v", key, wantKey)
					}
					if test.useChildPick {
						// If the test has set `useChildPick` to true,
						// `chidlPicker` would have been initialized to a non-nil
						// `fakeChildPicker` object. It is not possible to
						// initialize this from the test table, because we want a
						// unique instance of the `fakeChildPicker` object for each
						// test and be able to make sure that the pick was
						// delegated to it by reading on its pick channel.
						test.cacheEntry.ChildPicker = childPicker
					}
					return test.cacheEntry, test.pending
				},
				shouldThrottle: func() bool { return test.throttle },
				startRLS: func(path string, km keys.KeyMap) {
					if !test.newRLSRequest {
						rlsCh <- errors.New("RLS request attempted when none was expected")
						return
					}
					if path != rpcPath {
						rlsCh <- fmt.Errorf("RLS request initiated for rpcPath %s, want %s", path, rpcPath)
						return
					}
					if km.Str != wantKeyMapStr {
						rlsCh <- fmt.Errorf("RLS request initiated with keys %v, want %v", km.Str, wantKeyMapStr)
						return
					}
					rlsCh <- nil
				},
				defaultPick: func(_ balancer.PickInfo) (balancer.PickResult, error) {
					if !test.useDefaultPick {
						defaultPickCh <- errors.New("Using default pick when the test doesn't want to use default pick")
					}
					defaultPickCh <- nil
					return balancer.PickResult{}, nil
				},
			}

			// Spawn goroutines to verify that the appropriate picker was
			// invoked *the default picker or the child picker) and whether an
			// RLS request was made.
			var g errgroup.Group
			g.Go(func() error {
				if test.useChildPick {
					timer := time.NewTimer(defaultTestTimeout)
					select {
					case <-childPicker.pickCh:
						timer.Stop()
						return nil
					case <-timer.C:
						return errors.New("Timeout waiting for Pick to be called on child policy")
					}
				}
				return nil
			})
			g.Go(func() error {
				if test.useDefaultPick {
					timer := time.NewTimer(defaultTestTimeout)
					select {
					case err := <-defaultPickCh:
						timer.Stop()
						return err
					case <-timer.C:
						return errors.New("Timeout waiting for Pick to be called on default policy")
					}
				}
				return nil
			})
			g.Go(func() error {
				if test.newRLSRequest {
					timer := time.NewTimer(defaultTestTimeout)
					select {
					case err := <-rlsCh:
						timer.Stop()
						return err
					case <-timer.C:
						return errors.New("Timeout waiting for RLS request to be sent out")
					}
				}
				return nil
			})

			if gotResult, err := p.Pick(balancer.PickInfo{
				FullMethodName: rpcPath,
				Ctx:            metadata.NewOutgoingContext(context.Background(), md),
			}); err != test.wantErr || !cmp.Equal(gotResult, test.wantResult) {
				t.Fatalf("Pick() = {%v, %v}, want: {%v, %v}", gotResult, err, test.wantResult, test.wantErr)
			}

			// If any of the spawned goroutines returned an error (which
			// indicates that some expectation of the test was not met), we
			// fail.
			if err := g.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
