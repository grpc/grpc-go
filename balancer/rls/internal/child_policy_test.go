/*
 *
 * Copyright 2021 gRPC authors.
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
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/rls/internal/test/e2e"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancergroup"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
)

func init() {
	balancergroup.DefaultSubBalancerCloseTimeout = time.Millisecond
}

type fakeBalancerGroup struct {
	idInAddCh       *testutils.Channel
	builderInAddCh  *testutils.Channel
	idInRemoveCh    *testutils.Channel
	stateInUpdateCh *testutils.Channel
}

func newFakeBalancerGroup() *fakeBalancerGroup {
	return &fakeBalancerGroup{
		idInAddCh:       testutils.NewChannel(),
		builderInAddCh:  testutils.NewChannel(),
		idInRemoveCh:    testutils.NewChannel(),
		stateInUpdateCh: testutils.NewChannel(),
	}
}

func (fbg *fakeBalancerGroup) Add(id string, builder balancer.Builder) {
	fbg.idInAddCh.Send(id)
	fbg.builderInAddCh.Send(builder)
}

func (fbg *fakeBalancerGroup) Remove(id string) {
	fbg.idInRemoveCh.Send(id)
}

func (fbg *fakeBalancerGroup) UpdateClientConnState(id string, state balancer.ClientConnState) error {
	fbg.stateInUpdateCh.Send(state)
	return nil
}

func verifyChildPolicyIsNotAddedToGroup(t *testing.T, fbg *fakeBalancerGroup) {
	t.Helper()
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := fbg.idInAddCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Child policy added to balancer group when not expected")
	}
}

func verifyChildPolicyIsAddedToGroup(ctx context.Context, t *testing.T, fbg *fakeBalancerGroup, wantID string, wantBuilder balancer.Builder) {
	t.Helper()
	val, err := fbg.idInAddCh.Receive(ctx)
	if err != nil {
		t.Fatal()
	}
	if got := val.(string); got != wantID {
		t.Fatalf("Child policy added to balancer group with id %q, want %q", got, wantID)
	}
	val, err = fbg.builderInAddCh.Receive(ctx)
	if err != nil {
		t.Fatal()
	}
	if got := val.(balancer.Builder); got != wantBuilder {
		t.Fatalf("Child policy added to balancer group with builder %v, want %v", got, wantBuilder)
	}
}

func verifyConfigIsPushedToChildPolicy(ctx context.Context, t *testing.T, fbg *fakeBalancerGroup, wantCCS balancer.ClientConnState) {
	t.Helper()
	val, err := fbg.stateInUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatal()
	}
	if diff := cmp.Diff(wantCCS, val); diff != "" {
		t.Fatalf("Child policy configuration contains unexpected diff (-want +got):\n%s", diff)
	}
}

func verifyChildPolicyIsNotRemovedFromGroup(t *testing.T, fbg *fakeBalancerGroup) {
	t.Helper()
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := fbg.idInRemoveCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Child policy removed from balancer group when not expected")
	}
}

func verifyChildPolicyIsRemovedFromGroup(ctx context.Context, t *testing.T, fbg *fakeBalancerGroup, wantID string) {
	t.Helper()
	val, err := fbg.idInRemoveCh.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := val.(string); got != wantID {
		t.Fatalf("Child policy removed from balancer group with id %q, want %q", got, wantID)
	}
}

func (s) TestChildPolicyWrapperReferenceCounting(t *testing.T) {
	const (
		childPolicyTarget = "test-child-policy-target"
		childPolicyName   = "test-child-policy-wrapper-ref-counting"
	)

	// Register an LB policy capable of being a child of the RLS LB policy.
	e2e.RegisterRLSChildPolicy(childPolicyName)
	t.Logf("Registered child policy with name %q", childPolicyName)

	// Create a child policy wrapper with a fake balancer group.
	fakeBG := newFakeBalancerGroup()
	resolverState := resolver.State{Addresses: []resolver.Address{{Addr: "dummy-address"}}}
	args := childPolicyWrapperArgs{
		policyName:    childPolicyName,
		target:        childPolicyTarget,
		targetField:   e2e.RLSChildPolicyTargetNameField,
		config:        make(map[string]json.RawMessage),
		resolverState: resolverState,
		bg:            fakeBG,
	}
	cpw := newChildPolicyWrapper(args)

	// Verify that the child policy is added to the balancer group.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	childPolicyBuilder := balancer.Get(childPolicyName)
	verifyChildPolicyIsAddedToGroup(ctx, t, fakeBG, childPolicyTarget, childPolicyBuilder)

	// Verify that the configuration is pushed to the child policy.
	childPolicyParser := childPolicyBuilder.(balancer.ConfigParser)
	sc, err := childPolicyParser.ParseConfig([]byte(fmt.Sprintf(`{%q: %q}`, e2e.RLSChildPolicyTargetNameField, childPolicyTarget)))
	if err != nil {
		t.Fatal(err)
	}
	wantClientConnState := balancer.ClientConnState{
		ResolverState:  resolverState,
		BalancerConfig: sc,
	}
	verifyConfigIsPushedToChildPolicy(ctx, t, fakeBG, wantClientConnState)

	// Acquire another reference to the child policy wrapper and verify that it
	// is not added to the balancer group.
	cpw.acquireRef()
	verifyChildPolicyIsNotAddedToGroup(t, fakeBG)

	// Release one reference and verify that the child policy is not removed.
	cpw.releaseRef()
	verifyChildPolicyIsNotRemovedFromGroup(t, fakeBG)

	// Release the last reference and verify that the child policy is removed.
	cpw.releaseRef()
	verifyChildPolicyIsRemovedFromGroup(ctx, t, fakeBG, childPolicyTarget)
}

func (s) TestChildPolicyConfigUpdates(t *testing.T) {
	const (
		childPolicyTarget = "test-child-policy-target"
		childPolicyName1  = "test-child-policy-config-updates-1"
		childPolicyName2  = "test-child-policy-config-updates-2"
	)

	// Register LB policies capable of being a child of the RLS LB policy.
	e2e.RegisterRLSChildPolicy(childPolicyName1)
	t.Logf("Registered child policy with name %q", childPolicyName1)
	e2e.RegisterRLSChildPolicy(childPolicyName2)
	t.Logf("Registered child policy with name %q", childPolicyName2)

	// Create a child policy wrapper with a fake balancer group.
	fakeBG := newFakeBalancerGroup()
	resolverState := resolver.State{Addresses: []resolver.Address{{Addr: "dummy-address"}}}
	args := childPolicyWrapperArgs{
		policyName:    childPolicyName1,
		target:        childPolicyTarget,
		targetField:   e2e.RLSChildPolicyTargetNameField,
		config:        make(map[string]json.RawMessage),
		resolverState: resolverState,
		bg:            fakeBG,
	}
	cpw := newChildPolicyWrapper(args)

	// Verify that the child policy is added to the balancer group.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	childPolicyBuilder := balancer.Get(childPolicyName1)
	verifyChildPolicyIsAddedToGroup(ctx, t, fakeBG, childPolicyTarget, childPolicyBuilder)

	// Verify that the configuration is pushed to the child policy.
	childPolicyParser := childPolicyBuilder.(balancer.ConfigParser)
	sc, err := childPolicyParser.ParseConfig([]byte(fmt.Sprintf(`{%q: %q}`, e2e.RLSChildPolicyTargetNameField, childPolicyTarget)))
	if err != nil {
		t.Fatal(err)
	}
	wantClientConnState := balancer.ClientConnState{
		ResolverState:  resolverState,
		BalancerConfig: sc,
	}
	verifyConfigIsPushedToChildPolicy(ctx, t, fakeBG, wantClientConnState)

	// Change the child policy configuration, and verify the update.
	randomStr := "random"
	randomJSON, _ := json.Marshal(randomStr)
	args.config["Random"] = randomJSON
	sc, err = childPolicyParser.ParseConfig([]byte(fmt.Sprintf(`
{
  %q: %q,
  "Random": "%s"
}`, e2e.RLSChildPolicyTargetNameField, childPolicyTarget, randomStr)))
	if err != nil {
		t.Fatal(err)
	}
	wantClientConnState = balancer.ClientConnState{
		ResolverState:  resolverState,
		BalancerConfig: sc,
	}
	cpw.handleNewConfigs(args)
	verifyConfigIsPushedToChildPolicy(ctx, t, fakeBG, wantClientConnState)

	// Change the child policy name and verify the old policy is removed from
	// the balancer group and the new one is added.
	args.policyName = childPolicyName2
	cpw.handleNewConfigs(args)
	verifyChildPolicyIsRemovedFromGroup(ctx, t, fakeBG, childPolicyTarget)
	childPolicyBuilder = balancer.Get(childPolicyName2)
	verifyChildPolicyIsAddedToGroup(ctx, t, fakeBG, childPolicyTarget, childPolicyBuilder)
	verifyConfigIsPushedToChildPolicy(ctx, t, fakeBG, wantClientConnState)
}

func (s) TestChildPolicyBadConfigs(t *testing.T) {
	const (
		childPolicyTarget = "test-child-policy-target"
		childPolicyName   = "test-child-policy-bad-configs"
	)

	// Register an LB policy capable of being a child of the RLS LB policy.
	e2e.RegisterRLSChildPolicy(childPolicyName)
	t.Logf("Registered child policy with name %q", childPolicyName)

	// Create a child policy wrapper with a fake balancer group. Specify bad
	// child policy configuration.
	fakeBG := newFakeBalancerGroup()
	resolverState := resolver.State{Addresses: []resolver.Address{{Addr: "dummy-address"}}}
	args := childPolicyWrapperArgs{
		policyName:  childPolicyName,
		target:      childPolicyTarget,
		targetField: e2e.RLSChildPolicyTargetNameField,
		config: map[string]json.RawMessage{
			"dummy": []byte("not jSON"),
		},
		resolverState: resolverState,
		bg:            fakeBG,
	}
	cpw := newChildPolicyWrapper(args)
	if state := cpw.state.ConnectivityState; state != connectivity.TransientFailure {
		t.Fatalf("Child policy wrapper in state %s, want %s", state, connectivity.TransientFailure)
	}
}
