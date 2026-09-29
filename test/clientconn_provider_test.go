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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

type testClientConn struct {
	grpc.ClientConnInterface
	key string
}

// Tests that ClientConnProviderFromResolverState returns nil when the
// resolver.State does not have a ClientConnProvider attribute.
func (s) TestClientConnProviderFromResolverState_WithoutProvider(t *testing.T) {
	tests := []struct {
		name  string
		state resolver.State
	}{
		{
			name:  "no_attributes",
			state: resolver.State{},
		},
		{
			name: "no_clientConnProvider_attribute",
			state: resolver.State{
				Attributes: attributes.New("some_key", "some_value"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotProvider := grpc.ClientConnProviderFromResolverState(tt.state)
			if gotProvider != nil {
				t.Fatalf("ClientConnProviderFromResolverState() = %v, want <nil>", gotProvider)
			}
		})
	}
}

// Tests that ClientConnProviderFromResolverState returns the same provider that
// was set in the resolver.State attributes. This is verified indirectly by
// calling the returned provider and checking that it returns the expected
// ClientConnInterface.
func (s) TestClientConnProvider_ResolverStateAttributes_WithProvider(t *testing.T) {
	state := grpc.SetClientConnProvider(resolver.State{}, func(key string) (grpc.ClientConnInterface, func(), error) {
		return &testClientConn{key: key}, func() {}, nil
	})

	ccProvider := grpc.ClientConnProviderFromResolverState(state)
	if ccProvider == nil {
		t.Fatalf("ClientConnProviderFromResolverState() returned nil provider, want non-nil")
	}

	const testKey = "test-key"
	cc, _, err := ccProvider(testKey)
	if err != nil {
		t.Fatalf("ClientConnProvider(%q) returned error: %v", testKey, err)
	}
	testCC, ok := cc.(*testClientConn)
	if !ok {
		t.Fatalf("ClientConnProvider(%q) returned type %T, want *testClientConn", testKey, cc)
	}
	if testCC.key != testKey {
		t.Errorf("ClientConnProvider(%q) returned ClientConn with key %q, want %q", testKey, testCC.key, testKey)
	}
}

// Tests that setting a ClientConnProvider in the resolver.State attributes does not
// overwrite other existing attributes.
func (s) TestClientConnProvider_ResolverStateAttributes_OtherKeyVal(t *testing.T) {
	provider := func(key string) (grpc.ClientConnInterface, func(), error) {
		return &testClientConn{key: key}, func() {}, nil
	}

	type otherKey struct{}
	const otherVal = "other-val"
	state := resolver.State{Attributes: attributes.New(otherKey{}, otherVal)}
	state = grpc.SetClientConnProvider(state, provider)
	if got := state.Attributes.Value(otherKey{}); got != otherVal {
		t.Errorf("state.Attributes.Value(otherKey{}) = %v, want %q", got, otherVal)
	}
}

// Tests that two resolver.State values with the same ClientConnProvider function
// compare equal via Attributes.Equal, and two resolver.State values with different
// ClientConnProvider functions do not compare equal via Attributes.Equal.
func (s) TestClientConnProvider_ResolverStateAttributes_Equal(t *testing.T) {
	provider1 := func(key string) (grpc.ClientConnInterface, func(), error) {
		return &testClientConn{key: key}, func() {}, nil
	}
	provider2 := func(key string) (grpc.ClientConnInterface, func(), error) {
		return &testClientConn{key: key}, func() {}, nil
	}

	// Two states with the same provider function must compare equal via
	// Attributes.Equal.
	state1 := grpc.SetClientConnProvider(resolver.State{}, provider1)
	state1Copy := grpc.SetClientConnProvider(resolver.State{}, provider1)
	if !cmp.Equal(state1, state1Copy) {
		t.Errorf("cmp.Equal(state1, state1Copy) = false, want true")
	}

	// Two states with different provider functions must not compare equal via
	// Attributes.Equal.
	state2 := grpc.SetClientConnProvider(resolver.State{}, provider2)
	if cmp.Equal(state1, state2) {
		t.Errorf("cmp.Equal(state1, state2) = true, want false")
	}
}

// Tests that a ClientConnProvider configured via WithClientConnProvider is
// propagated in the resolver.State passed to the LB policy's
// UpdateClientConnState method.
func (s) TestClientConnProvider_DialOptionPlumbing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create a stub balancer that captures the ClientConnState passed to
	// UpdateClientConnState.
	balancerName := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "")
	stateCh := make(chan balancer.ClientConnState, 1)
	stub.Register(balancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(_ *stub.BalancerData, ccs balancer.ClientConnState) error {
			select {
			case stateCh <- ccs:
			default:
			}
			return nil
		},
	})

	// Create a manual resolver and a ClientConnProvider that returns a test
	// ClientConnInterface.
	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{Endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: "dummy"}}}}})
	provider := func(key string) (grpc.ClientConnInterface, func(), error) {
		return &testClientConn{key: key}, func() {}, nil
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithClientConnProvider(provider),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, balancerName)),
	}
	cc, err := grpc.NewClient(r.Scheme()+":///", opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()
	cc.Connect()

	var gotCCS balancer.ClientConnState
	select {
	case gotCCS = <-stateCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for UpdateClientConnState")
	}

	gotProvider := grpc.ClientConnProviderFromResolverState(gotCCS.ResolverState)
	if gotProvider == nil {
		t.Fatal("ClientConnProviderFromResolverState() = nil, want non-nil")
	}
	const testKey = "test-key"
	gotCC, _, err := gotProvider(testKey)
	if err != nil {
		t.Fatalf("ClientConnProvider(%q) failed with error: %v", testKey, err)
	}
	testCC, ok := gotCC.(*testClientConn)
	if !ok {
		t.Fatalf("ClientConnProvider(%q) returned type %T, want *testClientConn", testKey, gotCC)
	}
	if testCC.key != testKey {
		t.Errorf("ClientConnProvider(%q) returned ClientConn with key %q, want %q", testKey, testCC.key, testKey)
	}
}
