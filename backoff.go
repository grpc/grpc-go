/*
 *
 * Copyright 2017, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpc

import (
	"math/rand"
	"time"
)

// DefaultBackoffConfig uses values specified for backoff in
// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md.
var (
	DefaultBackoffConfig = BackoffConfig{
		MaxDelay:  120 * time.Second,
		baseDelay: 1.0 * time.Second,
		factor:    1.6,
		jitter:    0.2,
	}
)

// backoffStrategy defines the methodology for backing off after a grpc
// connection failure.
//
// This is unexported until the gRPC project decides whether or not to allow
// alternative backoff strategies. Once a decision is made, this type and its
// method may be exported.
type backoffStrategy interface {
	// backoff returns the amount of time to wait before the next retry given
	// the number of consecutive failures.
	backoff(retries int) time.Duration
}

// BackoffConfig defines the parameters for the default gRPC backoff strategy.
type BackoffConfig struct {
	// MaxDelay is the upper bound of backoff delay.
	MaxDelay time.Duration

	// TODO(stevvooe): The following fields are not exported, as allowing
	// changes would violate the current gRPC specification for backoff. If
	// gRPC decides to allow more interesting backoff strategies, these fields
	// may be opened up in the future.

	// baseDelay is the amount of time to wait before retrying after the first
	// failure.
	baseDelay time.Duration

	// factor is applied to the backoff after each retry.
	factor float64

	// jitter provides a range to randomize backoff delays.
	jitter float64
}

func setDefaults(bc *BackoffConfig) {
	md := bc.MaxDelay
	*bc = DefaultBackoffConfig

	if md > 0 {
		bc.MaxDelay = md
	}
}

func (bc BackoffConfig) backoff(retries int) time.Duration {
	if retries == 0 {
		return bc.baseDelay
	}
	backoff, max := float64(bc.baseDelay), float64(bc.MaxDelay)
	for backoff < max && retries > 0 {
		backoff *= bc.factor
		retries--
	}
	if backoff > max {
		backoff = max
	}
	// Randomize backoff delays so that if a cluster of requests start at
	// the same time, they won't operate in lockstep.
	backoff *= 1 + bc.jitter*(rand.Float64()*2-1)
	if backoff < 0 {
		return 0
	}
	return time.Duration(backoff)
}
