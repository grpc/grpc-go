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

package backoff

import (
	"time"

	"google.golang.org/grpc/internal/grpcrand"
)

// Strategy defines the methodology for backing off after a grpc connection
// failure.
//
// This is kept in internal until the gRPC project decides whether or not to
// allow alternative backoff strategies.
type Strategy interface {
	// Backoff returns the amount of time to wait before the next retry given
	// the number of consecutive failures.
	Backoff(retries int) time.Duration
}

// Config defines the parameters for the default gRPC backoff strategy.
type Config struct {
	// MaxDelay is the upper bound of backoff delay.
	MaxDelay time.Duration

	// TODO(stevvooe): The following fields are not exported, as allowing
	// changes would violate the current gRPC specification for backoff. If
	// gRPC decides to allow more interesting backoff strategies, these fields
	// may be opened up in the future.

	// BaseDelay is the amount of time to wait before retrying after the first
	// failure.
	BaseDelay time.Duration

	// Factor is applied to the backoff after each retry.
	Factor float64

	// Jitter provides a range to randomize backoff delays.
	Jitter float64
}

// Backoff returns the amount of time to wait before the next retry given the
// number of retries.
func (bc Config) Backoff(retries int) time.Duration {
	if retries == 0 {
		return bc.BaseDelay
	}
	backoff, max := float64(bc.BaseDelay), float64(bc.MaxDelay)
	for backoff < max && retries > 0 {
		backoff *= bc.Factor
		retries--
	}
	if backoff > max {
		backoff = max
	}
	// Randomize backoff delays so that if a cluster of requests start at
	// the same time, they won't operate in lockstep.
	backoff *= 1 + bc.Jitter*(grpcrand.Float64()*2-1)
	if backoff < 0 {
		return 0
	}
	return time.Duration(backoff)
}
