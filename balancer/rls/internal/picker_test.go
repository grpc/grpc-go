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

// initKeyBuilderMap initializes a keyBuilderMap of the form:
// {
// 		"gFoo": "k1=n1",
//		"gBar/method1": "k2=n21,n22"
// 		"gFoobar": "k3=n3",
// }
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

// fakePicker sends a PickResult with a fakeSubConn with the configured id.
type fakePicker struct {
	id int
}

func (p *fakePicker) Pick(_ balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{SubConn: &fakeSubConn{id: p.id}}, nil
}

// newFakePicker returns a fakePicker configured with a random ID. The subConns
// returned by this picker are of type fakefakeSubConn, and contain the same
// random ID, which tests can use to verify.
func newFakePicker() *fakePicker {
	return &fakePicker{id: grpcrand.Intn(math.MaxInt32)}
}

func verifySubConn(sc balancer.SubConn, wantID int) error {
	fsc, ok := sc.(*fakeSubConn)
	if !ok {
		return fmt.Errorf("Pick() returned a SubConn of type %T, want %T", sc, &fakeSubConn{})
	}
	if fsc.id != wantID {
		return fmt.Errorf("Pick() returned SubConn %d, want %d", fsc.id, wantID)
	}
	return nil
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
				kbm: kbm,
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
						ChildPicker: &fakePicker{id: randID},
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

// TestPick_DataCacheMiss_PendingCacheMiss verifies different Pick scenarios
// where the entry is neither found in the data cache nor in the pending cache.
func TestPick_DataCacheMiss_PendingCacheMiss(t *testing.T) {
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

	tests := []struct {
		desc string
		// Whether or not a default target is configured.
		defaultPickExists bool
		// Whether or not the RLS request should be throttled.
		throttle bool
		// Whether or not the test is expected to make a new RLS request.
		wantRLSRequest bool
		// Expected error returned by the rlsPicker under test.
		wantErr error
	}{
		{
			desc:              "rls request throttled with default pick",
			defaultPickExists: true,
			throttle:          true,
		},
		{
			desc:     "rls request throttled without default pick",
			throttle: true,
			wantErr:  errRLSThrottled,
		},
		{
			desc:           "rls request not throttled",
			wantRLSRequest: true,
			wantErr:        balancer.ErrNoSubConnAvailable,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			rlsCh := testutils.NewChannel()
			defaultPicker := newFakePicker()

			p := rlsPicker{
				kbm: kbm,
				// Cache lookup fails, no pending entry.
				readCache: func(key cache.Key) (*cache.Entry, bool) {
					if !cmp.Equal(key, wantKey) {
						t.Fatalf("cache lookup using cacheKey %v, want %v", key, wantKey)
					}
					return nil, false
				},
				shouldThrottle: func() bool { return test.throttle },
				startRLS: func(path string, km keys.KeyMap) {
					if !test.wantRLSRequest {
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
			}
			if test.defaultPickExists {
				p.defaultPick = defaultPicker.Pick
			}

			gotResult, err := p.Pick(balancer.PickInfo{
				FullMethodName: rpcPath,
				Ctx:            metadata.NewOutgoingContext(context.Background(), md),
			})
			if err != test.wantErr {
				t.Fatalf("Pick() returned error {%v}, want {%v}", err, test.wantErr)
			}
			// If the test specified that a new RLS request should be made,
			// verify it.
			if test.wantRLSRequest {
				ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
				defer cancel()
				if rlsErr, err := rlsCh.Receive(ctx); err != nil || rlsErr != nil {
					t.Fatalf("startRLS() = %v, error receiving from channel: %v", rlsErr, err)
				}
			}
			if test.wantErr != nil {
				return
			}

			// We get here only for cases where we expect the pick to be
			// delegated to the default picker.
			if err := verifySubConn(gotResult.SubConn, defaultPicker.id); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestPick_DataCacheMiss_PendingCacheMiss verifies different Pick scenarios
// where the entry is not found in the data cache, but there is a entry in the
// pending cache. For all of these scenarios, no new RLS request will be sent.
func TestPick_DataCacheMiss_PendingCacheHit(t *testing.T) {
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

	tests := []struct {
		desc              string
		defaultPickExists bool
	}{
		{
			desc:              "default pick exists",
			defaultPickExists: true,
		},
		{
			desc: "default pick does not exists",
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			rlsCh := testutils.NewChannel()
			p := rlsPicker{
				kbm: kbm,
				// Cache lookup fails, pending entry exists.
				readCache: func(key cache.Key) (*cache.Entry, bool) {
					if !cmp.Equal(key, wantKey) {
						t.Fatalf("cache lookup using cacheKey %v, want %v", key, wantKey)
					}
					return nil, true
				},
				// Never throttle. We do not expect an RLS request to be sent out anyways.
				shouldThrottle: func() bool { return false },
				startRLS: func(_ string, _ keys.KeyMap) {
					rlsCh.Send(nil)
				},
			}
			if test.defaultPickExists {
				p.defaultPick = func(info balancer.PickInfo) (balancer.PickResult, error) {
					// We do not expect the default picker to be invoked at all.
					// So, if we get here, the test will fail, because it
					// expects the pick to be queued.
					return balancer.PickResult{}, nil
				}
			}

			if _, err := p.Pick(balancer.PickInfo{
				FullMethodName: rpcPath,
				Ctx:            metadata.NewOutgoingContext(context.Background(), md),
			}); err != balancer.ErrNoSubConnAvailable {
				t.Fatalf("Pick() returned error {%v}, want {%v}", err, balancer.ErrNoSubConnAvailable)
			}

			// Make sure that no RLS request was sent out.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err := rlsCh.Receive(ctx); err != context.DeadlineExceeded {
				t.Fatalf("RLS request sent out when pending entry exists")
			}
		})
	}
}

// TestPick_DataCacheHit_PendingCacheMiss verifies different Pick scenarios
// where the entry is found in the data cache, and there is no entry in the
// pending cache. This includes cases where the entry in the data cache is
// stale, expired or in backoff.
func TestPick_DataCacheHit_PendingCacheMiss(t *testing.T) {
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
		// Whether or not a default target is configured.
		defaultPickExists bool
		// Whether or not the RLS request should be throttled.
		throttle bool
		// Whether or not the test is expected to make a new RLS request.
		wantRLSRequest bool
		// Whether or not the rlsPicker should delegate to the child picker.
		wantChildPick bool
		// Whether or not the rlsPicker should delegate to the default picker.
		wantDefaultPick bool
		// Expected error returned by the rlsPicker under test.
		wantErr error
	}{
		{
			desc: "valid entry",
			cacheEntry: &cache.Entry{
				ExpiryTime: time.Now().Add(defaultTestMaxAge),
				StaleTime:  time.Now().Add(defaultTestMaxAge),
			},
			wantChildPick: true,
		},
		{
			desc:          "entryStale_requestThrottled",
			cacheEntry:    &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			throttle:      true,
			wantChildPick: true,
		},
		{
			desc:           "entryStale_requestNotThrottled",
			cacheEntry:     &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			wantRLSRequest: true,
			wantChildPick:  true,
		},
		{
			desc:              "entryExpired_requestThrottled_defaultPickExists",
			cacheEntry:        &cache.Entry{},
			throttle:          true,
			defaultPickExists: true,
			wantDefaultPick:   true,
		},
		{
			desc:       "entryExpired_requestThrottled_defaultPickNotExists",
			cacheEntry: &cache.Entry{},
			throttle:   true,
			wantErr:    errRLSThrottled,
		},
		{
			desc:           "entryExpired_requestNotThrottled",
			cacheEntry:     &cache.Entry{},
			wantRLSRequest: true,
			wantErr:        balancer.ErrNoSubConnAvailable,
		},
		{
			desc: "entryExpired_backoffNotExpired_defaultPickExists",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			defaultPickExists: true,
		},
		{
			desc: "entryExpired_backoffNotExpired_defaultPickNotExists",
			cacheEntry: &cache.Entry{
				BackoffTime: time.Now().Add(defaultTestMaxAge),
				CallStatus:  rlsLastErr,
			},
			wantErr: rlsLastErr,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			rlsCh := testutils.NewChannel()
			childPicker := newFakePicker()
			defaultPicker := newFakePicker()

			p := rlsPicker{
				kbm: kbm,
				readCache: func(key cache.Key) (*cache.Entry, bool) {
					if !cmp.Equal(key, wantKey) {
						t.Fatalf("cache lookup using cacheKey %v, want %v", key, wantKey)
					}
					test.cacheEntry.ChildPicker = childPicker
					return test.cacheEntry, false
				},
				shouldThrottle: func() bool { return test.throttle },
				startRLS: func(path string, km keys.KeyMap) {
					if !test.wantRLSRequest {
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
			}
			if test.defaultPickExists {
				p.defaultPick = defaultPicker.Pick
			}

			gotResult, err := p.Pick(balancer.PickInfo{
				FullMethodName: rpcPath,
				Ctx:            metadata.NewOutgoingContext(context.Background(), md),
			})
			if err != test.wantErr {
				t.Fatalf("Pick() returned error {%v}, want {%v}", err, test.wantErr)
			}
			// If the test specified that a new RLS request should be made,
			// verify it.
			if test.wantRLSRequest {
				ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
				defer cancel()
				if rlsErr, err := rlsCh.Receive(ctx); err != nil || rlsErr != nil {
					t.Fatalf("startRLS() = %v, error receiving from channel: %v", rlsErr, err)
				}
			}
			if test.wantErr != nil {
				return
			}

			// We get here only for cases where we expect the pick to be
			// delegated to the child picker or the default picker.
			if test.wantChildPick {
				if err := verifySubConn(gotResult.SubConn, childPicker.id); err != nil {
					t.Fatal(err)
				}

			}
			if test.wantDefaultPick {
				if err := verifySubConn(gotResult.SubConn, defaultPicker.id); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestPick_DataCacheHit_PendingCacheHit verifies different Pick scenarios where
// the entry is found both in the data cache and in the pending cache. This
// mostly verifies cases where the entry is stale, but there is already a
// pending RLS request, so no new request should be sent out.
func TestPick_DataCacheHit_PendingCacheHit(t *testing.T) {
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

	tests := []struct {
		desc string
		// The cache entry, as returned by the overridden readCache hook.
		cacheEntry *cache.Entry
		// Whether or not a default target is configured.
		defaultPickExists bool
		// Expected error returned by the rlsPicker under test.
		wantErr error
	}{
		{
			desc:       "stale entry",
			cacheEntry: &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
		},
		{
			desc:              "stale entry with default picker",
			cacheEntry:        &cache.Entry{ExpiryTime: time.Now().Add(defaultTestMaxAge)},
			defaultPickExists: true,
		},
		{
			desc:              "entryExpired_defaultPickExists",
			cacheEntry:        &cache.Entry{},
			defaultPickExists: true,
			wantErr:           balancer.ErrNoSubConnAvailable,
		},
		{
			desc:       "entryExpired_defaultPickNotExists",
			cacheEntry: &cache.Entry{},
			wantErr:    balancer.ErrNoSubConnAvailable,
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			rlsCh := testutils.NewChannel()
			childPicker := newFakePicker()

			p := rlsPicker{
				kbm: kbm,
				readCache: func(key cache.Key) (*cache.Entry, bool) {
					if !cmp.Equal(key, wantKey) {
						t.Fatalf("cache lookup using cacheKey %v, want %v", key, wantKey)
					}
					test.cacheEntry.ChildPicker = childPicker
					return test.cacheEntry, true
				},
				// Never throttle. We do not expect an RLS request to be sent out anyways.
				shouldThrottle: func() bool { return false },
				startRLS: func(path string, km keys.KeyMap) {
					rlsCh.Send(nil)
				},
			}
			if test.defaultPickExists {
				p.defaultPick = func(info balancer.PickInfo) (balancer.PickResult, error) {
					// We do not expect the default picker to be invoked at all.
					// So, if we get here, we return an error.
					return balancer.PickResult{}, errors.New("default picker invoked when expecting a child pick")
				}
			}

			gotResult, err := p.Pick(balancer.PickInfo{
				FullMethodName: rpcPath,
				Ctx:            metadata.NewOutgoingContext(context.Background(), md),
			})
			if err != test.wantErr {
				t.Fatalf("Pick() returned error {%v}, want {%v}", err, test.wantErr)
			}
			// Make sure that no RLS request was sent out.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err := rlsCh.Receive(ctx); err != context.DeadlineExceeded {
				t.Fatalf("RLS request sent out when pending entry exists")
			}
			if test.wantErr != nil {
				return
			}

			// We get here only for cases where we expect the pick to be
			// delegated to the child picker.
			if err := verifySubConn(gotResult.SubConn, childPicker.id); err != nil {
				t.Fatal(err)
			}
		})
	}
}
