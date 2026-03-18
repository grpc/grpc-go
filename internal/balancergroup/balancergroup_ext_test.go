/*
 * Copyright 2026 gRPC authors.
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
 */

package balancergroup_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/balancergroup"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 100 * time.Millisecond
)

// Tests verifies that the RemoveImmediately method of balancergroup closes the
// child balancer immediately, while the Remove method does not close the child
// balancer immediately, when the cache is enabled.
func (s) TestBalancerGroup_RemoveImmediately(t *testing.T) {
	// Channels to track child balancer creation and closure.
	childLBCreated := make(chan string, 1)
	childLBClosed := make(chan string, 1)

	childLBName1 := strings.ToLower(t.Name()) + "-child-1"
	t.Logf("Registering a child balancer with name %q", childLBName1)
	stub.Register(childLBName1, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			childLBCreated <- childLBName1
		},
		Close: func(bd *stub.BalancerData) {
			childLBClosed <- childLBName1
		},
	})

	childLBName2 := strings.ToLower(t.Name()) + "-child-2"
	t.Logf("Registering a child balancer with name %q", childLBName2)
	stub.Register(childLBName2, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			childLBCreated <- childLBName2
		},
		Close: func(bd *stub.BalancerData) {
			childLBClosed <- childLBName2
		},
	})

	t.Logf("Creating a balancergroup with cache enabled")
	tcc := testutils.NewBalancerClientConn(t)
	bg := balancergroup.New(balancergroup.Options{
		CC:                      tcc,
		BuildOpts:               balancer.BuildOptions{},
		SubBalancerCloseTimeout: defaultTestTimeout,
	})

	t.Logf("Adding a child balancer with name %q to the group", "child-1")
	bg.AddWithClientConn("child-1", childLBName1, tcc)
	select {
	case <-childLBCreated:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timeout when waiting for child LB to be created")
	}

	t.Logf("Adding a child balancer with name %q to the group", "child-2")
	bg.AddWithClientConn("child-2", childLBName2, tcc)
	select {
	case <-childLBCreated:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timeout when waiting for child LB to be created")
	}

	t.Logf("Removing the child balancer with name %q from the group with immediate effect", "child-1")
	bg.RemoveImmediately("child-1")
	select {
	case <-childLBClosed:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timeout when waiting for child LB to be closed")
	}

	t.Logf("Removing the child balancer with name %q from the group with caching", "child-2")
	bg.Remove("child-2")
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-childLBClosed:
		t.Fatalf("Child LB closed when expected to be cached")
	case <-sCtx.Done():
	}

	t.Logf("Closing the balancergroup, which should close the second child balancer")
	bg.Close()
	select {
	case <-childLBClosed:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timeout when waiting for child LB to be closed")
	}
}
