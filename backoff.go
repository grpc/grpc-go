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

// See internal/backoff package for the backoff implementation. This file is
// kept for the exported types and API backward compatibility.

package grpc

import (
	"time"
)

// DefaultBackoffConfig uses values specified for backoff in
// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md.
// Deprecated: use ConnectParams instead.
var DefaultBackoffConfig = BackoffConfig{
	MaxDelay: 120 * time.Second,
}

// BackoffConfig defines the parameters for the default gRPC backoff strategy.
// Deprecated: use ConnectParams instead.
type BackoffConfig struct {
	// MaxDelay is the upper bound of backoff delay.
	MaxDelay time.Duration
}

// ConnectParams defines the parameters for connecting and retrying. Users
// are encouraged to use this instead of BackoffConfig.
// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md.
//
// This API is EXPERIMENTAL.
type ConnectParams struct {
	// BackoffBaseDelay is the amount of time to backoff after the first
	// connection failure.
	BackoffBaseDelay time.Duration
	// BackoffMultiplier is the factor with which to multiply backoffs after a
	// failed retry. Should ideally be greater than 1.
	BackoffMultiplier float64
	// BackoffJitter is the factor with which backoffs are randomized.
	BackoffJitter float64
	// BackoffMaxDelay is the upper bound of backoff delay.
	BackoffMaxDelay time.Duration
	// MinConnectTimeout is the minimum amount of time we are willing to give a
	// connection to complete.
	MinConnectTimeout time.Duration
}
