// +build go1.12

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
	"fmt"
	"regexp"
	"testing"

	"github.com/cespare/xxhash"
	"github.com/google/go-cmp/cmp"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/metadata"
	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer" // To parse LB config
	"google.golang.org/grpc/xds/internal/xdsclient"
)

func (s) TestPruneActiveClusters(t *testing.T) {
	r := &xdsResolver{activeClusters: map[string]*clusterInfo{
		"zero":        {refCount: 0},
		"one":         {refCount: 1},
		"two":         {refCount: 2},
		"anotherzero": {refCount: 0},
	}}
	want := map[string]*clusterInfo{
		"one": {refCount: 1},
		"two": {refCount: 2},
	}
	r.pruneActiveClusters()
	if d := cmp.Diff(r.activeClusters, want, cmp.AllowUnexported(clusterInfo{})); d != "" {
		t.Fatalf("r.activeClusters = %v; want %v\nDiffs: %v", r.activeClusters, want, d)
	}
}

// TestGenerateRequestHashHeaders tests generating request hashes for hash policies that specify
// to hash headers.
func (s) TestGenerateRequestHashHeaders(t *testing.T) {
	cs := &configSelector{}

	rpcInfo := iresolver.RPCInfo{
		Context: metadata.NewIncomingContext(context.Background(), metadata.Pairs(":path", "/products")),
		Method:  "/some-method",
	}

	hashPolicies := []*xdsclient.HashPolicy{
		{
			HashPolicyType:    xdsclient.HashPolicyTypeHeader,
			HeaderName:        ":path",
			Regex:             func() *regexp.Regexp { return regexp.MustCompile("/products") }(), // Will replace /products with /products, to test find and replace functionality.
			RegexSubstitution: "/products",
		},
	}

	requestHashGot := cs.generateHash(rpcInfo, hashPolicies)

	// Precompute the expected hash here - logically representing the value of the :path header which will be
	// "/products".
	requestHashWant := xxhash.Sum64String("/products")

	if requestHashGot != requestHashWant {
		t.Fatalf("requestHashGot = %v, requestHashWant = %v", requestHashGot, requestHashWant)
	}
}

// TestGenerateHashChannelID tests generating request hashes for hash policies that specify
// to hash something that uniquely identifies the ClientConn (the pointer).
func (s) TestGenerateRequestHashChannelID(t *testing.T) {
	cs := &configSelector{
		r: &xdsResolver{
			cc: &testClientConn{},
		},
	}

	requestHashWant := xxhash.Sum64String(fmt.Sprintf("%p", &cs.r.cc))

	hashPolicies := []*xdsclient.HashPolicy{
		{
			HashPolicyType: xdsclient.HashPolicyTypeChannelID,
		},
	}
	requestHashGot := cs.generateHash(iresolver.RPCInfo{}, hashPolicies)

	if requestHashGot != requestHashWant {
		t.Fatalf("requestHashGot = %v, requestHashWant = %v", requestHashGot, requestHashWant)
	}
}
