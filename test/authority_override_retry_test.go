/*
 *
 * Copyright 2026 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// authorityOverridePicker returns an authority override in the pick result
// metadata for the first pick only, and no metadata for subsequent picks. This
// mirrors an xDS cluster (gRFC A81) where only some of the endpoints carry a
// hostname, so only some picks rewrite the authority.
type authorityOverridePicker struct {
	sc        balancer.SubConn
	authority string
	picks     atomic.Int32
}

func (p *authorityOverridePicker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	res := balancer.PickResult{SubConn: p.sc}
	if p.picks.Add(1) == 1 {
		res.Metadata = metadata.Pairs(":authority", p.authority)
	}
	return res, nil
}

// TestAuthorityOverrideNotReusedAcrossAttempts verifies that an authority
// override supplied by the LB picker applies only to the attempt it was picked
// for. A retry attempt whose pick carries no override must fall back to the
// channel's authority instead of reusing the previous attempt's override.
func (s) TestAuthorityOverrideNotReusedAcrossAttempts(t *testing.T) {
	const (
		balancerName      = "authority-override-retry-balancer"
		overrideAuthority = "picked-endpoint.example.com"
		wantAuthority     = "test.server"
	)

	bf := stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			addrs := ccs.ResolverState.Addresses
			if len(addrs) == 0 {
				return nil
			}
			var sc balancer.SubConn
			sc, err := bd.ClientConn.NewSubConn(addrs[:1], balancer.NewSubConnOptions{
				StateListener: func(state balancer.SubConnState) {
					bd.ClientConn.UpdateState(balancer.State{
						ConnectivityState: state.ConnectivityState,
						Picker:            &authorityOverridePicker{sc: sc, authority: overrideAuthority},
					})
				},
			})
			if err != nil {
				return err
			}
			sc.Connect()
			return nil
		},
	}
	stub.Register(balancerName, bf)

	var mu sync.Mutex
	var authorities []string
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			mu.Lock()
			authorities = append(authorities, md.Get(":authority")...)
			attempt := len(authorities)
			mu.Unlock()
			// Fail the first attempt with a retryable code so that the RPC is
			// retried, and let the second attempt succeed.
			if attempt == 1 {
				return nil, status.Error(codes.Unavailable, "forcing a retry")
			}
			return &testpb.Empty{}, nil
		},
	}
	if err := ss.StartServer(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer ss.Stop()

	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: ss.Address}}})

	sc := fmt.Sprintf(`{
		"loadBalancingConfig": [{%q: {}}],
		"methodConfig": [{
			"name": [{"service": "grpc.testing.TestService"}],
			"retryPolicy": {
				"maxAttempts": 2,
				"initialBackoff": "0.01s",
				"maxBackoff": "0.01s",
				"backoffMultiplier": 1.0,
				"retryableStatusCodes": ["UNAVAILABLE"]
			}
		}]
	}`, balancerName)

	cc, err := grpc.NewClient(r.Scheme()+":///"+wantAuthority,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithDefaultServiceConfig(sc),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authorities) != 2 {
		t.Fatalf("Server saw %d attempts (%q), want 2", len(authorities), authorities)
	}
	if authorities[0] != overrideAuthority {
		t.Errorf("First attempt used authority %q, want %q", authorities[0], overrideAuthority)
	}
	if authorities[1] != wantAuthority {
		t.Errorf("Retry attempt used authority %q, want %q", authorities[1], wantAuthority)
	}
}
