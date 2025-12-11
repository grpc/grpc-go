/*
 *
 * Copyright 2025 gRPC authors.
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

package randomsubsetting

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestParseConfig(t *testing.T) {
	parser := bb{}
	tests := []struct {
		name    string
		input   string
		wantCfg serviceconfig.LoadBalancingConfig
		wantErr string
	}{
		{
			name:    "invalid_json",
			input:   "{{invalidjson{{",
			wantErr: "json.Unmarshal failed for configuration",
		},
		{
			name:    "empty_config",
			input:   `{}`,
			wantErr: "SubsetSize must be greater than 0",
		},
		{
			name:    "subset_size_zero",
			input:   `{ "subsetSize": 0 }`,
			wantErr: "SubsetSize must be greater than 0",
		},
		{
			name:    "child_policy_missing",
			input:   `{ "subsetSize": 1 }`,
			wantErr: "ChildPolicy must be specified",
		},
		{
			name:    "child_policy_not_registered",
			input:   `{ "subsetSize": 1 , "childPolicy": [{"unregistered_lb": {}}] }`,
			wantErr: "no supported policies found",
		},
		{
			name:  "success",
			input: `{ "subsetSize": 3, "childPolicy": [{"round_robin": {}}]}`,
			wantCfg: &lbConfig{
				SubsetSize:  3,
				ChildPolicy: &iserviceconfig.BalancerConfig{Name: "round_robin"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCfg, gotErr := parser.ParseConfig(json.RawMessage(test.input))
			// Substring match makes this very tightly coupled to the
			// internalserviceconfig.BalancerConfig error strings. However, it
			// is important to distinguish the different types of error messages
			// possible as the parser has a few defined buckets of ways it can
			// error out.
			if (gotErr != nil) != (test.wantErr != "") {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", test.input, gotErr, test.wantErr)
			}
			if gotErr != nil && !strings.Contains(gotErr.Error(), test.wantErr) {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", test.input, gotErr, test.wantErr)
			}
			if test.wantErr != "" {
				return
			}
			if diff := cmp.Diff(test.wantCfg, gotCfg); diff != "" {
				t.Fatalf("ParseConfig(%s) got unexpected output, diff (-want +got):\n%s", test.input, diff)
			}
		})
	}
}

func makeEndpoints(n int) []resolver.Endpoint {
	endpoints := make([]resolver.Endpoint, n)
	for i := 0; i < n; i++ {
		endpoints[i] = resolver.Endpoint{
			Addresses: []resolver.Address{{Addr: fmt.Sprintf("endpoint-%d", i)}},
		}
	}
	return endpoints
}

func (s) TestCalculateSubset_Simple(t *testing.T) {
	tests := []struct {
		name       string
		endpoints  []resolver.Endpoint
		subsetSize uint32
		want       []resolver.Endpoint
	}{
		{
			name:       "NoEndpoints",
			endpoints:  []resolver.Endpoint{},
			subsetSize: 3,
			want:       []resolver.Endpoint{},
		},
		{
			name:       "SubsetSizeLargerThanNumberOfEndpoints",
			endpoints:  makeEndpoints(5),
			subsetSize: 10,
			want:       makeEndpoints(5),
		},
		{
			name:       "SubsetSizeEqualToNumberOfEndpoints",
			endpoints:  makeEndpoints(5),
			subsetSize: 5,
			want:       makeEndpoints(5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &subsettingBalancer{
				cfg:        &lbConfig{SubsetSize: tt.subsetSize},
				hashSeed:   0,
				hashDigest: xxhash.New(),
			}
			got := b.calculateSubset(tt.endpoints)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("calculateSubset() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestCalculateSubset_EndpointsRetainHashValues(t *testing.T) {
	endpoints := makeEndpoints(10)
	const subsetSize = 5
	// The subset is deterministic based on the hash, so we can hardcode
	// the expected output.
	want := []resolver.Endpoint{
		{Addresses: []resolver.Address{{Addr: "endpoint-6"}}},
		{Addresses: []resolver.Address{{Addr: "endpoint-0"}}},
		{Addresses: []resolver.Address{{Addr: "endpoint-1"}}},
		{Addresses: []resolver.Address{{Addr: "endpoint-7"}}},
		{Addresses: []resolver.Address{{Addr: "endpoint-3"}}},
	}

	b := &subsettingBalancer{
		cfg:        &lbConfig{SubsetSize: subsetSize},
		hashSeed:   0,
		hashDigest: xxhash.New(),
	}
	for range 10 {
		got := b.calculateSubset(endpoints)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("calculateSubset() returned diff (-want +got):\n%s", diff)
		}
	}
}

func startTestServiceBackends(t *testing.T, num int) []resolver.Address {
	t.Helper()

	addresses := make([]resolver.Address, num)
	for i := 0; i < num; i++ {
		server := stubserver.StartTestService(t, nil)
		t.Cleanup(server.Stop)
		addresses[i] = resolver.Address{Addr: server.Address}
	}
	return addresses
}

func startTestSetupClients(t *testing.T, clientsCount int, subsetSize int, addresses []resolver.Address) []testgrpc.TestServiceClient {
	t.Helper()

	clients := make([]testgrpc.TestServiceClient, clientsCount)
	ccs := make([]*grpc.ClientConn, clientsCount)
	var err error

	for i := 0; i < clientsCount; i++ {
		mr := manual.NewBuilderWithScheme("subsetting-e2e")
		jsonConfig := fmt.Sprintf(`
		{
		  "loadBalancingConfig": [
			{
			  "random_subsetting": {
				"subsetSize": %d,
				"childPolicy": [{"round_robin": {}}]
			  }
			}
		  ]
		}`, subsetSize)

		sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(string(jsonConfig))
		mr.InitialState(resolver.State{
			Addresses:     addresses,
			ServiceConfig: sc,
		})

		ccs[i], err = grpc.Dial(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("grpc.Dial() failed: %v", err)
		}
		clients[i] = testgrpc.NewTestServiceClient(ccs[i])
	}

	t.Cleanup(func() {
		for _, cc := range ccs {
			cc.Close()
		}
	})
	return clients
}

func checkRoundRobinRPCs(ctx context.Context, t *testing.T, clients []testgrpc.TestServiceClient, subsetSize int, maxDiff int) {
	clientsPerBackend := map[string]map[int]struct{}{}

	for clientIdx, client := range clients {
		// make sure that every client send exactly 1 request to each server in its subset
		for i := 0; i < subsetSize; i++ {
			var peer peer.Peer
			_, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer))
			if err != nil {
				t.Fatalf("failed to call server: %v", err)
			}
			if peer.Addr != nil {
				if m, ok := clientsPerBackend[peer.Addr.String()]; !ok {
					clientsPerBackend[peer.Addr.String()] = map[int]struct{}{clientIdx: {}}
				} else if _, ok := m[clientIdx]; !ok {
					m[clientIdx] = struct{}{}
				} else {
					// The backend receives a second request from the same client. This could happen if the client have 1 backend in READY
					// state while the other  are CONNECTING. In this case round_robbin will pick the same address twice.
					// We are going to retry after short timeout.
					time.Sleep(10 * time.Microsecond)
					i--
				}
			} else {
				t.Fatalf("peer.Addr == nil, peer: %v", peer)
			}
		}
	}

	minClientsPerBackend := math.MaxInt
	maxClientsPerBackend := 0
	for _, v := range clientsPerBackend {
		if len(v) < minClientsPerBackend {
			minClientsPerBackend = len(v)
		}
		if len(v) > maxClientsPerBackend {
			maxClientsPerBackend = len(v)
		}
	}

	if maxClientsPerBackend > minClientsPerBackend+maxDiff {
		t.Fatalf("the difference between min and max clients per backend should be <= %d, clientsPerBackend: %v", maxDiff, clientsPerBackend)
	}
}

func (s) TestSubsettingE2E(t *testing.T) {
	// Save the original hash seed and restore it at the end of the test.
	originalHashSeed := hashSeed
	defer func() {
		hashSeed = originalHashSeed
	}()

	// Set a fixed hash seed to make the test deterministic.
	hashSeed = func() uint64 { return 1 }
	tests := []struct {
		name       string
		subsetSize int
		clients    int
		backends   int
		maxDiff    int
	}{
		{
			name:       "backends could be evenly distributed between small number of clients",
			backends:   3,
			clients:    2,
			subsetSize: 2,
			maxDiff:    1,
		},
		{
			name:       "backends could be evenly distributed between clients",
			backends:   12,
			clients:    8,
			subsetSize: 3,
			maxDiff:    3,
		},
		{
			name:       "backends could NOT be evenly distributed between clients",
			backends:   37,
			clients:    22,
			subsetSize: 5,
			maxDiff:    15,
		},
		{
			name:       "backends %% subsetSize == 0, but there are not enough clients to fill the last round",
			backends:   20,
			clients:    7,
			subsetSize: 5,
			maxDiff:    20,
		},
		{
			name:       "last round is completely filled, but there are some excluded backends on every round",
			backends:   21,
			clients:    15,
			subsetSize: 5,
			maxDiff:    4,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addresses := startTestServiceBackends(t, test.backends)

			clients := startTestSetupClients(t, test.clients, test.subsetSize, addresses)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			checkRoundRobinRPCs(ctx, t, clients, test.subsetSize, test.maxDiff)
		})
	}
}
