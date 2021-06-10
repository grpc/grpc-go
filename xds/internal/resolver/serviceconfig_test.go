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
	"google.golang.org/grpc/internal"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"regexp"
	"testing"
	"unsafe"

	"github.com/google/go-cmp/cmp"
	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer" // To parse LB config
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
		Context: metadata.AppendToOutgoingContext(context.Background(), ":path", "/products"),
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

/*type testClientConn struct {
	resolver.ClientConn
}

func (t *testClientConn) UpdateState(s resolver.State) error {
	return nil
}

func (t *testClientConn) ReportError(err error) {

}

func (t *testClientConn) ParseServiceConfig(jsonSC string) *serviceconfig.ParseResult {
	return internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(jsonSC)
}*/

// TestGenerateHashChannelID tests generating request hashes for hash policies that specify
// to hash something that uniquely identifies the ClientConn (the pointer).
func (s) TestGenerateRequestHashChannelID(t *testing.T) {
	cs := &configSelector{
		r: &xdsResolver{
			cc: testClientConn{}, // The pointer to this will be hashed.
		},
	}

	requestHashWant := xxhash.Sum64(*(*[]byte)(unsafe.Pointer(&cs.r.cc)))

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
