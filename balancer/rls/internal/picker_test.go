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
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/rls/internal/cache"
	"google.golang.org/grpc/balancer/rls/internal/keys"
	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
)

const defaultTestMaxAge = 5 * time.Second

func initKeyBuilderMap() (keys.BuilderMap, error) {
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
	return keys.MakeBuilderMap(&rlspb.RouteLookupConfig{
		GrpcKeybuilders: []*rlspb.GrpcKeyBuilder{kb1, kb2, kb3},
	})
}

// fakeSubConn embeds the balancer.SubConn interface and contains an id which
// helps verify that the expected subConn was returned by the rlsPicker.
type fakeSubConn struct {
	balancer.SubConn
	id int
}

// fakeChildPicker sends a PickResult with a fakeSubConn with the configured id.
type fakeChildPicker struct {
	id int
}

func (p *fakeChildPicker) Pick(_ balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{SubConn: &fakeSubConn{id: p.id}}, nil
}

// TestPickKeyBuilder verifies the different possible scenarios for forming an
// RLS key for an incoming RPC.
func TestPickKeyBuilder(t *testing.T) {
	kbm, err := initKeyBuilderMap()
	if err != nil {
		t.Fatalf("Failed to create keyBuilderMap: %v", err)
	}

	tests := []struct {
		desc    string
		rpcPath string
		md      metadata.MD
		wantKey cache.Key
	}{
		{
			desc:    "non existent service in keyBuilder map",
			rpcPath: "/gNonExistentService/method",
			md:      metadata.New(map[string]string{"n1": "v1", "n3": "v3"}),
			wantKey: cache.Key{Path: "/gNonExistentService/method", KeyMap: ""},
		},
		{
			desc:    "no metadata in incoming context",
			rpcPath: "/gFoo/method",
			md:      metadata.MD{},
			wantKey: cache.Key{Path: "/gFoo/method", KeyMap: ""},
		},
		{
			desc:    "keyBuilderMatch",
			rpcPath: "/gFoo/method",
			md:      metadata.New(map[string]string{"n1": "v1", "n3": "v3"}),
			wantKey: cache.Key{Path: "/gFoo/method", KeyMap: "k1=v1"},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			randID := grpcrand.Intn(math.MaxInt32)
			p := rlsPicker{
				kbm:      kbm,
				strategy: rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
				readCache: func(key cache.Key) (*cache.Entry, bool) {
					if !cmp.Equal(key, test.wantKey) {
						t.Fatalf("rlsPicker using cacheKey %v, want %v", key, test.wantKey)
					}

					now := time.Now()
					return &cache.Entry{
						ExpiryTime: now.Add(defaultTestMaxAge),
						StaleTime:  now.Add(defaultTestMaxAge),
						// Cache entry is configured with a child policy whose
						// rlsPicker always returns an empty PickResult and nil
						// error.
						ChildPicker: &fakeChildPicker{id: randID},
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
			sc, ok := gotResult.SubConn.(*fakeSubConn)
			if !ok {
				t.Fatalf("Pick() returned a SubConn of type %T, want %T", gotResult.SubConn, &fakeSubConn{})
			}
			if sc.id != randID {
				t.Fatalf("Pick() returned SubConn %d, want %d", sc.id, randID)
			}
		})
	}
}

func TestPick(t *testing.T) {
	const (
		rpcPath       = "/gFoo/method"
		wantKeyMapStr = "k1=v1"
	)
	kbm, err := initKeyBuilderMap()
	if err != nil {
		t.Fatalf("Failed to create keyBuilderMap: %v", err)
	}
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
		// Request processing strategy as used by the rlsPicker.
		strategy rlspb.RouteLookupConfig_RequestProcessingStrategy
		// Expected error returned by the rlsPicker under test.
		wantErr error
	}{
		{
			desc:     "cacheMiss_pending_defaultTargetOnError",
			pending:  true,
			strategy: rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:  balancer.ErrNoSubConnAvailable,
		},
		{
			desc:     "cacheMiss_pending_clientSeesError",
			pending:  true,
			strategy: rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:  balancer.ErrNoSubConnAvailable,
		},
		{
			desc:           "cacheMiss_pending_defaultTargetOnMiss",
			pending:        true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:        nil,
		},
		{
			desc:          "cacheMiss_noPending_notThrottled_defaultTargetOnError",
			newRLSRequest: true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:       balancer.ErrNoSubConnAvailable,
		},
		{
			desc:          "cacheMiss_noPending_notThrottled_clientSeesError",
			newRLSRequest: true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:       balancer.ErrNoSubConnAvailable,
		},
		{
			desc:           "cacheMiss_noPending_notThrottled_defaultTargetOnMiss",
			newRLSRequest:  true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:        nil,
		},
		{
			desc:           "cacheMiss_noPending_throttled_defaultTargetOnError",
			throttle:       true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:        nil,
		},
		{
			desc:     "cacheMiss_noPending_throttled_clientSeesError",
			throttle: true,
			strategy: rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:  errRLSThrottled,
		},
		{
			desc:           "cacheMiss_noPending_throttled_defaultTargetOnMiss",
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			throttle:       true,
			useDefaultPick: true,
			wantErr:        nil,
		},
		{
			desc:           "cacheHit_noPending_boExpired_dataExpired_throttled_defaultTargetOnError",
			cacheEntry:     &cache.Entry{}, // Everything is expired in this entry
			throttle:       true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:        nil,
		},
		{
			desc:       "cacheHit_noPending_boExpired_dataExpired_throttled_clientSeesError",
			cacheEntry: &cache.Entry{}, // Everything is expired in this entry
			throttle:   true,
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:    errRLSThrottled,
		},
		{
			desc:           "cacheHit_noPending_boExpired_dataExpired_throttled_defaultTargetOnMiss",
			cacheEntry:     &cache.Entry{}, // Everything is expired in this entry
			throttle:       true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:        nil,
		},
		{
			desc:         "cacheHit_noPending_stale_boExpired_dataNotExpired_throttled_defaultTargetOnMiss",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			throttle:     true, // Proactive refresh is throttled.
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:      nil,
		},
		{
			desc:         "cacheHit_noPending_stale_boExpired_dataNotExpired_throttled_clientSeesError",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			throttle:     true, // Proactive refresh is throttled.
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:      nil,
		},
		{
			desc:         "cacheHit_noPending_stale_boExpired_dataNotExpired_throttled_defaultTargetOnError",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			throttle:     true, // Proactive refresh is throttled.
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:      nil,
		},
		{
			desc:          "cacheHit_noPending_boExpired_dataExpired_notThrottled_defaultTargetOnError",
			cacheEntry:    &cache.Entry{}, // Everything is expired in this entry
			newRLSRequest: true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:       balancer.ErrNoSubConnAvailable,
		},
		{
			desc:          "cacheHit_noPending_boExpired_dataExpired_notThrottled_clientSeesError",
			cacheEntry:    &cache.Entry{}, // Everything is expired in this entry
			newRLSRequest: true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:       balancer.ErrNoSubConnAvailable,
		},
		{
			desc:           "cacheHit_noPending_boExpired_dataExpired_notThrottled_defaultTargetOnMiss",
			cacheEntry:     &cache.Entry{}, // Everything is expired in this entry
			newRLSRequest:  true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:        nil,
		},
		{
			desc:          "cacheHit_noPending_stale_boExpired_dataNotExpired_notThrottled_defaultTargetOnMiss",
			cacheEntry:    &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			newRLSRequest: true, // Proactive refresh.
			useChildPick:  true,
			strategy:      rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:       nil,
		},
		{
			desc:          "cacheHit_noPending_stale_boExpired_dataNotExpired_notThrottled_clientSeesError",
			cacheEntry:    &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			newRLSRequest: true, // Proactive refresh.
			useChildPick:  true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:       nil,
		},
		{
			desc:          "cacheHit_noPending_stale_boExpired_dataNotExpired_notThrottled_defaultTargetOnError",
			cacheEntry:    &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			newRLSRequest: true, // Proactive refresh.
			useChildPick:  true,
			strategy:      rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:       nil,
		},
		{
			desc:           "cacheHit_noPending_stale_boNotExpired_dataExpired_defaultTargetOnError",
			cacheEntry:     &cache.Entry{BackoffTime: time.Now().Add(defaultTestMaxAge)},
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:        nil,
		},
		{
			desc:           "cacheHit_noPending_stale_boNotExpired_dataExpired_defaultTargetOnMiss",
			cacheEntry:     &cache.Entry{BackoffTime: time.Now().Add(defaultTestMaxAge)},
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:        nil,
		},
		{
			desc: "cacheHit_noPending_stale_boNotExpired_dataExpired_clientSeesError",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			strategy: rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:  rlsLastErr,
		},
		{
			desc: "cacheHit_noPending_stale_boNotExpired_dataNotExpired_defaultTargetOnError",
			cacheEntry: &cache.Entry{
				ExpiryTime:  time.Now().Add(defaultTestMaxAge),
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_stale_boNotExpired_dataNotExpired_defaultTargetOnMiss",
			cacheEntry: &cache.Entry{
				ExpiryTime:  time.Now().Add(defaultTestMaxAge),
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_stale_boNotExpired_dataNotExpired_clientSeesError",
			cacheEntry: &cache.Entry{
				ExpiryTime:  time.Now().Add(defaultTestMaxAge),
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_notStale_dataNotExpired_defaultTargetOnError",
			cacheEntry: &cache.Entry{
				ExpiryTime: time.Now().Add(defaultTestMaxAge),
				StaleTime:  time.Now().Add(defaultTestMaxAge),
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_notStale_dataNotExpired_defaultTargetOnMiss",
			cacheEntry: &cache.Entry{
				ExpiryTime: time.Now().Add(defaultTestMaxAge),
				StaleTime:  time.Now().Add(defaultTestMaxAge),
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:      nil,
		},
		{
			desc: "cacheHit_noPending_notStale_dataNotExpired_clientSeesError",
			cacheEntry: &cache.Entry{
				ExpiryTime: time.Now().Add(defaultTestMaxAge),
				StaleTime:  time.Now().Add(defaultTestMaxAge),
			},
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:      nil,
		},
		{
			desc:       "cacheHit_pending_dataExpired_boExpired_defaultTargetOnError",
			cacheEntry: &cache.Entry{},
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:    balancer.ErrNoSubConnAvailable,
		},
		{
			desc:           "cacheHit_pending_dataExpired_boExpired_defaultTargetOnMiss",
			cacheEntry:     &cache.Entry{},
			pending:        true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:        nil,
		},
		{
			desc:       "cacheHit_pending_dataExpired_boExpired_clientSeesError",
			cacheEntry: &cache.Entry{},
			pending:    true,
			strategy:   rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:    balancer.ErrNoSubConnAvailable,
		},
		{
			desc: "cacheHit_pending_dataExpired_boNotExpired_defaultTargetOnError",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:        nil,
		},
		{
			desc: "cacheHit_pending_dataExpired_boNotExpired_defaultTargetOnMiss",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			pending:        true,
			useDefaultPick: true,
			strategy:       rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:        nil,
		},
		{
			desc: "cacheHit_pending_dataExpired_boNotExpired_clientSeesError",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			pending:  true,
			strategy: rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:  rlsLastErr,
		},
		{
			desc:         "cacheHit_pending_dataNotExpired_defaultTargetOnError",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			pending:      true,
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR,
			wantErr:      nil,
		},
		{
			desc:         "cacheHit_pending_dataNotExpired_defaultTargetOnMiss",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			pending:      true,
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS,
			wantErr:      nil,
		},
		{
			desc:         "cacheHit_pending_dataNotExpired_clientSeesError",
			cacheEntry:   &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			pending:      true,
			useChildPick: true,
			strategy:     rlspb.RouteLookupConfig_SYNC_LOOKUP_CLIENT_SEES_ERROR,
			wantErr:      nil,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			rlsCh := testutils.NewChannel()
			randID := grpcrand.Intn(math.MaxInt32)
			// We instantiate a fakeChildPicker which will return a fakeSubConn
			// with configured id. Either the childPicker or the defaultPicker
			// is configured to use this fakePicker based on whether
			// useChidlPick or useDefaultPick is set in the test.
			childPicker := &fakeChildPicker{id: randID}

			p := rlsPicker{
				kbm:      kbm,
				strategy: test.strategy,
				readCache: func(key cache.Key) (*cache.Entry, bool) {
					if !cmp.Equal(key, wantKey) {
						t.Fatalf("cache lookup using cacheKey %v, want %v", key, wantKey)
					}
					if test.useChildPick {
						test.cacheEntry.ChildPicker = childPicker
					}
					return test.cacheEntry, test.pending
				},
				shouldThrottle: func() bool { return test.throttle },
				startRLS: func(path string, km keys.KeyMap) {
					if !test.newRLSRequest {
						rlsCh.Send(errors.New("RLS request attempted when none was expected"))
						return
					}
					if path != rpcPath {
						rlsCh.Send(fmt.Errorf("RLS request initiated for rpcPath %s, want %s", path, rpcPath))
						return
					}
					if km.Str != wantKeyMapStr {
						rlsCh.Send(fmt.Errorf("RLS request initiated with keys %v, want %v", km.Str, wantKeyMapStr))
						return
					}
					rlsCh.Send(nil)
				},
				defaultPick: func(info balancer.PickInfo) (balancer.PickResult, error) {
					if !test.useDefaultPick {
						return balancer.PickResult{}, errors.New("Using default pick when the test doesn't want to use default pick")
					}
					return childPicker.Pick(info)
				},
			}

			gotResult, err := p.Pick(balancer.PickInfo{
				FullMethodName: rpcPath,
				Ctx:            metadata.NewOutgoingContext(context.Background(), md),
			})
			if err != test.wantErr {
				t.Fatalf("Pick() returned error {%v}, want {%v}", err, test.wantErr)
			}
			if test.useChildPick || test.useDefaultPick {
				// For cases where the pick is not queued, but is delegated to
				// either the child rlsPicker or the default rlsPicker, we
				// verify that the expected fakeSubConn is returned.
				sc, ok := gotResult.SubConn.(*fakeSubConn)
				if !ok {
					t.Fatalf("Pick() returned a SubConn of type %T, want %T", gotResult.SubConn, &fakeSubConn{})
				}
				if sc.id != randID {
					t.Fatalf("Pick() returned SubConn %d, want %d", sc.id, randID)
				}
			}

			// If the test specified that a new RLS request should be made,
			// verify it.
			if test.newRLSRequest {
				if rlsErr, err := rlsCh.Receive(); err != nil || rlsErr != nil {
					t.Fatalf("startRLS() = %v, error receiving from channel: %v", rlsErr, err)
				}
			}
		})
	}
}
