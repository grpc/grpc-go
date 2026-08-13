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

package resolver

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/grpcutil"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	_ "google.golang.org/grpc/internal/xds/balancer/cdsbalancer" // To parse LB config
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
)

var defaultTestTimeout = 10 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// newResolverForActiveEntryTests returns a resolver with just enough state to
// exercise acquireActiveClusterInfo and the cleanup it registers. The current
// config selector is an erroring one so that pushing a new service config does
// not require an xDS config to be present.
func newResolverForActiveEntryTests(t *testing.T) *xdsResolver {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &xdsResolver{
		cc:                &testutils.ResolverClientConn{Logger: t},
		activeClusters:    make(map[string]*grpcsync.RefCounted[*clusterInfo]),
		activePlugins:     make(map[string]*grpcsync.RefCounted[*clusterInfo]),
		serializer:        grpcsync.NewCallbackSerializer(ctx),
		serializerCancel:  cancel,
		curConfigSelector: newErroringConfigSelector(errors.New("test"), ""),
	}
	r.logger = prefixLogger(r)
	return r
}

// runOnSerializer runs f in the context of a serializer callback, which is the
// only place the resolver's active cluster and plugin maps may be touched, and
// blocks until it has run. Any callback queued by an earlier call to this
// helper, including a removal queued when a reference count reached zero, is
// guaranteed to have run by the time f is invoked.
func runOnSerializer(ctx context.Context, t *testing.T, r *xdsResolver, f func()) {
	t.Helper()

	done := make(chan struct{})
	r.serializer.TrySchedule(func(context.Context) {
		defer close(done)
		f()
	})
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for serializer callback to run")
	}
}

// TestActivePluginRefCounting verifies that repeated acquisitions of a cluster
// specifier plugin share a single entry, and that the entry is removed from
// activePlugins only once the last reference is released.
func (s) TestActivePluginRefCounting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	r := newResolverForActiveEntryTests(t)
	const key = "cluster_specifier_plugin:test-plugin"

	var first, second *grpcsync.RefCounted[*clusterInfo]
	runOnSerializer(ctx, t, r, func() {
		first = r.acquireActiveClusterInfo(key, "")
		second = r.acquireActiveClusterInfo(key, "")
	})
	if first != second {
		t.Fatalf("acquireActiveClusterInfo(%q) returned a new entry; want the existing one to be reused", key)
	}

	// Two references are outstanding, so releasing one must keep the entry.
	runOnSerializer(ctx, t, r, func() { first.Decrement() })
	runOnSerializer(ctx, t, r, func() {
		if _, ok := r.activePlugins[key]; !ok {
			t.Errorf("activePlugins[%q] was removed while a reference is still held", key)
		}
	})

	// Releasing the last reference must remove the entry.
	runOnSerializer(ctx, t, r, func() { second.Decrement() })
	runOnSerializer(ctx, t, r, func() {
		if _, ok := r.activePlugins[key]; ok {
			t.Errorf("activePlugins[%q] still present after the last reference was released", key)
		}
	})
}

// TestActivePluginNotRevivedAfterRelease verifies that an entry whose reference
// count has already dropped to zero is replaced by a fresh entry rather than
// resurrected, and that the pending removal of the dead entry does not delete
// its replacement.
func (s) TestActivePluginNotRevivedAfterRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	r := newResolverForActiveEntryTests(t)
	const key = "cluster_specifier_plugin:test-plugin"

	var dead, live *grpcsync.RefCounted[*clusterInfo]
	runOnSerializer(ctx, t, r, func() {
		// Release the only reference. The entry is now dead, but its removal is
		// queued behind this callback and so has not run yet. Acquiring again
		// must therefore hand back a fresh entry rather than revive this one.
		dead = r.acquireActiveClusterInfo(key, "")
		dead.Decrement()
		live = r.acquireActiveClusterInfo(key, "")
	})
	if live == dead {
		t.Fatal("acquireActiveClusterInfo() returned an entry whose refcount had already reached zero; want a new entry")
	}

	// The dead entry's queued removal must not evict the replacement.
	runOnSerializer(ctx, t, r, func() {
		if got := r.activePlugins[key]; got != live {
			t.Errorf("activePlugins[%q] = %p, want the newly created entry %p", key, got, live)
		}
	})
}

func (s) TestGenerateRequestHash(t *testing.T) {
	const channelID = 12378921
	cs := &configSelector{channelID: channelID}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	tests := []struct {
		name            string
		hashPolicies    []*xdsresource.HashPolicy
		requestHashWant uint64
		rpcInfo         iresolver.RPCInfo
	}{
		// TestGenerateRequestHashHeaders tests generating request hashes for
		// hash policies that specify to hash headers.
		{
			name: "test-generate-request-hash-headers",
			hashPolicies: []*xdsresource.HashPolicy{{
				HashPolicyType:    xdsresource.HashPolicyTypeHeader,
				HeaderName:        ":path",
				Regex:             func() *regexp.Regexp { return regexp.MustCompile("/products") }(), // Will replace /products with /new-products, to test find and replace functionality.
				RegexSubstitution: "/new-products",
			}},
			requestHashWant: xxhash.Sum64String("/new-products"),
			rpcInfo: iresolver.RPCInfo{
				Context: metadata.NewOutgoingContext(ctx, metadata.Pairs(":path", "/products")),
				Method:  "/some-method",
			},
		},
		// TestGenerateHashChannelID tests generating request hashes for hash
		// policies that specify to hash something that uniquely identifies the
		// ClientConn (the pointer).
		{
			name: "test-generate-request-hash-channel-id",
			hashPolicies: []*xdsresource.HashPolicy{{
				HashPolicyType: xdsresource.HashPolicyTypeChannelID,
			}},
			requestHashWant: channelID,
			rpcInfo:         iresolver.RPCInfo{},
		},
		// TestGenerateRequestHashEmptyString tests generating request hashes
		// for hash policies that specify to hash headers and replace empty
		// strings in the headers.
		{
			name: "test-generate-request-hash-empty-string",
			hashPolicies: []*xdsresource.HashPolicy{{
				HashPolicyType:    xdsresource.HashPolicyTypeHeader,
				HeaderName:        ":path",
				Regex:             func() *regexp.Regexp { return regexp.MustCompile("") }(),
				RegexSubstitution: "e",
			}},
			requestHashWant: xxhash.Sum64String("eaebece"),
			rpcInfo: iresolver.RPCInfo{
				Context: metadata.NewOutgoingContext(ctx, metadata.Pairs(":path", "abc")),
				Method:  "/some-method",
			},
		},
		// Tests that bin headers are skipped.
		{
			name: "skip-bin",
			hashPolicies: []*xdsresource.HashPolicy{{
				HashPolicyType: xdsresource.HashPolicyTypeHeader,
				HeaderName:     "something-bin",
			}, {
				HashPolicyType: xdsresource.HashPolicyTypeChannelID,
			}},
			requestHashWant: channelID,
			rpcInfo: iresolver.RPCInfo{
				Context: metadata.NewOutgoingContext(ctx, metadata.Pairs("something-bin", "xyz")),
			},
		},
		// Tests that extra metadata takes precedence over the user's metadata.
		{
			name: "extra-metadata",
			hashPolicies: []*xdsresource.HashPolicy{{
				HashPolicyType: xdsresource.HashPolicyTypeHeader,
				HeaderName:     "content-type",
			}},
			requestHashWant: xxhash.Sum64String("grpc value"),
			rpcInfo: iresolver.RPCInfo{
				Context: grpcutil.WithExtraMetadata(
					metadata.NewOutgoingContext(ctx, metadata.Pairs("content-type", "user value")),
					metadata.Pairs("content-type", "grpc value"),
				),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestHashGot := cs.generateHash(test.rpcInfo, test.hashPolicies)
			if requestHashGot != test.requestHashWant {
				t.Fatalf("requestHashGot = %v, requestHashWant = %v", requestHashGot, test.requestHashWant)
			}
		})
	}
}
