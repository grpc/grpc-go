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

// Package resolver provides internal resolver-related functionality.
package resolver

import (
	"context"
	"sync"

	"google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
)

// ConfigSelector controls what configuration to use for every RPC.
type ConfigSelector interface {
	// Selects the configuration for the RPC.
	SelectConfig(RPCInfo) *RPCConfig
}

// RPCInfo contains RPC information needed by a ConfigSelector.
type RPCInfo struct {
	Context context.Context // Contains headers, application timeout
	Method  string          // i.e. "/Service/Method"
}

// RPCConfig describes the configuration to use for each RPC.
type RPCConfig struct {
	Context      context.Context            // passes info to LB Policy Picker
	MethodConfig serviceconfig.MethodConfig // configuration to use for this RPC
	OnCommitted  func()                     // Called when the RPC has been committed (retries no longer possible)
}

type csKeyType string

const csKey = csKeyType("grpc.internal.resolver.configSelector")

// SetConfigSelector sets the config selector in state and returns the new
// state.
func SetConfigSelector(state resolver.State, cs ConfigSelector) resolver.State {
	state.Attributes = state.Attributes.WithValues(csKey, cs)
	return state
}

// GetConfigSelector retrieves the config selector from state, if present, and
// returns it or nil if absent.
func GetConfigSelector(state resolver.State) ConfigSelector {
	cs, _ := state.Attributes.Value(csKey).(ConfigSelector)
	return cs
}

type countingConfigSelector struct {
	ConfigSelector
	wg sync.WaitGroup // number of in-flight calls to this ConfigSelector
}
