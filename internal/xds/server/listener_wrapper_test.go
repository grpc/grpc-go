/*
 *
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
 *
 */

package server

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Test verifies that an error received from RDS before any valid update is
// received is correctly propagated to the filter chains.
func (s) TestHandleRDSUpdate_ErrorBeforeUpdate(t *testing.T) {
	const routeName = "route-name"
	nodeID := "node-id"
	rdsErr := errors.New("rds error")

	// Create a filter chain that points to routeName.
	fc := &filterChain{
		routeConfigName:          routeName,
		usableRouteConfiguration: &atomic.Pointer[usableRouteConfiguration]{},
	}
	fc.usableRouteConfiguration.Store(&usableRouteConfiguration{}) // Initial state is an empty configuration with no error.

	// Create a listener wrapper with the above filter chain. This simulates the
	// state of the server after LDS has been processed, but before RDS has
	// returned any response for the route configuration.
	lw := &listenerWrapper{
		xdsNodeID: nodeID,
		activeFilterChainManager: &filterChainManager{
			filterChains: []*filterChain{fc},
		},
		rdsHandler: &rdsHandler{
			updates: make(map[string]rdsWatcherUpdate),
			cancels: make(map[string]func()),
		},
	}

	// Simulate an RDS update with an error for the route configuration.
	lw.handleRDSUpdate(routeName, rdsWatcherUpdate{err: rdsErr})

	// Verify that the error is propagated to the filter chain.
	gotURC := fc.usableRouteConfiguration.Load()
	wantURC := &usableRouteConfiguration{err: rdsErr, nodeID: nodeID}
	if diff := cmp.Diff(gotURC, wantURC, cmp.AllowUnexported(usableRouteConfiguration{}), cmp.Comparer(func(x, y error) bool {
		if x == nil || y == nil {
			return x == y
		}
		return x.Error() == y.Error()
	})); diff != "" {
		t.Fatalf("Unexpected usableRouteConfiguration (-got, +want):\n%s", diff)
	}
}

// Test verifies that a successful RDS update correctly populates the nodeID.
func (s) TestHandleRDSUpdate_Success(t *testing.T) {
	const routeName = "route-name"
	nodeID := "node-id"

	// Create a filter chain that points to routeName.
	fc := &filterChain{
		routeConfigName:          routeName,
		usableRouteConfiguration: &atomic.Pointer[usableRouteConfiguration]{},
	}
	fc.usableRouteConfiguration.Store(&usableRouteConfiguration{})

	lw := &listenerWrapper{
		xdsNodeID: nodeID,
		activeFilterChainManager: &filterChainManager{
			filterChains: []*filterChain{fc},
		},
		rdsHandler: &rdsHandler{
			updates: make(map[string]rdsWatcherUpdate),
			cancels: make(map[string]func()),
		},
	}

	// Simulate an RDS update with a valid route configuration for the routeName.
	lw.handleRDSUpdate(routeName, rdsWatcherUpdate{data: inlineRouteConfig})

	// Verify that the nodeID is populated in the resulting configuration.
	gotURC := fc.usableRouteConfiguration.Load()
	if gotURC.nodeID != nodeID {
		t.Fatalf("gotURC.nodeID = %q, want %q", gotURC.nodeID, nodeID)
	}
}

// Test verifies that an error received from RDS *after* a valid update is
// received is correctly propagated, and does not result in the filter chain
// keeping the "stale" successful configuration.
func (s) TestHandleRDSUpdate_ErrorAfterSuccess(t *testing.T) {
	const routeName = "route-name"
	nodeID := "node-id"
	rdsErr := errors.New("subsequent rds error")

	fc := &filterChain{
		routeConfigName:          routeName,
		usableRouteConfiguration: &atomic.Pointer[usableRouteConfiguration]{},
	}
	fc.usableRouteConfiguration.Store(&usableRouteConfiguration{})

	// Create a listener wrapper with the above filter chain. This simulates the
	// state of the server after LDS has been processed, but before RDS has
	// returned any response for the route configuration.
	lw := &listenerWrapper{
		xdsNodeID: nodeID,
		activeFilterChainManager: &filterChainManager{
			filterChains: []*filterChain{fc},
		},
		rdsHandler: &rdsHandler{
			updates: make(map[string]rdsWatcherUpdate),
			cancels: make(map[string]func()),
		},
	}

	// Simulate an RDS update with a valid route configuration for the routeName.
	lw.handleRDSUpdate(routeName, rdsWatcherUpdate{data: inlineRouteConfig})
	if err := fc.usableRouteConfiguration.Load().err; err != nil {
		t.Fatalf("Initial update failed: %v", err)
	}

	// Simulate an RDS update with an error for the route configuration.
	lw.handleRDSUpdate(routeName, rdsWatcherUpdate{err: rdsErr})

	// Verify that the error is propagated.
	gotURC := fc.usableRouteConfiguration.Load()
	if !errors.Is(gotURC.err, rdsErr) {
		t.Fatalf("urc.err = %v, want %v", gotURC.err, rdsErr)
	}
}
