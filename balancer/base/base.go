/*
 *
 * Copyright 2017 gRPC authors.
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

// Package base defines a balancer base that can be used to build balancers with
// different picking algorithms.
//
// The base balancer creates a new SubConn for each resolved address. The
// provided picker will only be notified about READY SubConns.
//
// This package is the base of round_robin balancer, its purpose is to be used
// to build round_robin like balancers with complex picking algorithms.
// Balancers with more complicated logic should try to implement a balancer
// builder from scratch.
//
// All APIs in this package are experimental.
package base

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
)

// PickerBuilder creates a balancer.Picker. It can maintain state which will be
// scoped to a single balancer.ClientConn.
type PickerBuilder interface {
	// Build takes a slice of ready SubConns, and returns a picker that will be
	// used by gRPC to pick a SubConn.
	Build(readySCs map[resolver.Address]balancer.SubConn) balancer.Picker
}

// NewBalancerBuilder returns a balancer builder. The balancers
// built by this builder will use the picker builder to build pickers.
func NewBalancerBuilder(name string, pb PickerBuilder) balancer.Builder {
	return NewBalancerBuilderWithConfig(name, pb, Config{})
}

type Config struct {
	// PickerBuilderFactory will be used to construct a picker builder for each
	// balancer.ClientConn.
	PickerBuilderFactory func() PickerBuilder

	// ConnectionManagerFactory will be used to construct a connection manager
	// for each balancer.ClientConn. If it is not set DefaultConnectionManager
	// is used.
	ConnectionManagerFactory func(SubConnManager, Config) ConnectionManager

	// HealthCheck indicates whether health checking should be enabled for this specific balancer.
	HealthCheck bool
}

// NewBalancerBuilderWithConfig returns a base balancer builder configured by the provided config.
func NewBalancerBuilderWithConfig(name string, pb PickerBuilder, config Config) balancer.Builder {
	config.PickerBuilderFactory = func() PickerBuilder {
		return pb
	}
	return MakeBalancerBuilder(name, config)
}

// MakeBalancerBuilder returns a new balancer.Builder with the given name and
// configuration. The configuration must, at a minimum, specify a
// PickerBuilderFactory. If the configured ConnectionManagerFactory is nil,
// DefaultConnectionManager is used.
func MakeBalancerBuilder(name string, config Config) balancer.Builder {
	if config.PickerBuilderFactory == nil {
		panic("config is missing a PickerBuilderFactory")
	}
	if config.ConnectionManagerFactory == nil {
		config.ConnectionManagerFactory = DefaultConnectionManager
	}
	return &baseBuilder{
		name:   name,
		config: config,
	}
}
