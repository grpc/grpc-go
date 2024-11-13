/*
 *
 * Copyright 2024 gRPC authors.
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

package delegatingresolver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
)

// TestDelegatingResolverNoProxy verifies the behavior of the delegating resolver when no proxy is configured.
func TestDelegatingResolverNoProxy(t *testing.T) {
	// t.Setenv("HTTPS_PROXY", "")               // Explicitly set proxy environment to empty to mimic no proxy environment set
	mr := manual.NewBuilderWithScheme("test") // Set up a manual resolver to control the address resolution.
	target := "test:///localhost:1234"

	stateCh := make(chan resolver.State, 1)
	updateStateF := func(s resolver.State) error {

		stateCh <- s

		return nil
	}

	errCh := make(chan error, 1)
	reportErrorF := func(err error) {
		errCh <- err
	}

	tcc := &testutils.ResolverClientConn{Logger: t, UpdateStateF: updateStateF, ReportErrorF: reportErrorF}
	// Create a delegating resolver with no proxy configuration
	dr, err := New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mr)
	if err != nil || dr == nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	// Update the manual resolver with a test address.
	mr.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: "test-addr"}}, ServiceConfig: &serviceconfig.ParseResult{}})

	// Verify that the delegating resolver outputs the same address.
	expectedState := resolver.State{Addresses: []resolver.Address{{Addr: "test-addr"}}, ServiceConfig: &serviceconfig.ParseResult{}}
	state := <-stateCh
	if len(state.Addresses) != 1 || !cmp.Equal(expectedState, state) {
		t.Errorf("Unexpected state from delegating resolver: %v, want %v", state, expectedState)
	}
}
