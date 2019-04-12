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

// Package backoff implement the backoff strategy for gRPC.
//
// This is kept in internal until the gRPC project decides whether or not to
// allow alternative backoff strategies.
package backoff

import (
	"time"

	"google.golang.org/grpc/internal/grpcrand"
)

// Strategy defines the methodology for backing off after a grpc connection
// failure.
type Strategy interface {
	// Backoff returns the amount of time to wait before the next retry given
	// the number of consecutive failures.
	Backoff(retries int) time.Duration
}

// StrategyBuilder helps build an implementation of the Strategy interface,
// with setters provided to set individual parameters of the backoff strategy.
type StrategyBuilder interface {
	// BaseDelay sets the amount of time to backoff after the first connection
	// failure.
	BaseDelay(time.Duration) StrategyBuilder
	// Multiplier sets the the factor with which to multiply backoffs after a
	// failed retry.
	Multiplier(float64) StrategyBuilder
	// Jitter sets the factor with which backoffs are randomized.
	Jitter(float64) StrategyBuilder
	// MaxDelay sets the upper bound of backoff delay.
	MaxDelay(time.Duration) StrategyBuilder
	// Build builds an implementation of the Strategy interface configured using
	// the above setter methods.
	Build() Strategy
}

// Default values for the different backoff parameters as defined in
// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md.
const (
	baseDelay  = 1.0 * time.Second
	multiplier = 1.6
	jitter     = 0.2
	maxDelay   = 120 * time.Second
)

// NewExponentialBuilder returns an implementation of the StrategyBuilder
// interface used to build an exponential backoff strategy implementation, as
// defined in
// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md.
func NewExponentialBuilder() StrategyBuilder {
	return &exponentialBuilder{
		bd:     baseDelay,
		mltpr:  multiplier,
		jitter: jitter,
		md:     maxDelay,
	}
}

// exponentialBuilder implements the StrategyBuilder interface for an
// exponential backoff strategy.
type exponentialBuilder struct {
	bd     time.Duration
	mltpr  float64
	jitter float64
	md     time.Duration
}

// BaseDelay helps implement StrategyBuilder.
func (eb *exponentialBuilder) BaseDelay(bd time.Duration) StrategyBuilder {
	eb.bd = bd
	return eb
}

// BaseDelay helps implement StrategyBuilder.
func (eb *exponentialBuilder) Multiplier(m float64) StrategyBuilder {
	eb.mltpr = m
	return eb
}

// BaseDelay helps implement StrategyBuilder.
func (eb *exponentialBuilder) Jitter(j float64) StrategyBuilder {
	eb.jitter = j
	return eb
}

// BaseDelay helps implement StrategyBuilder.
func (eb *exponentialBuilder) MaxDelay(md time.Duration) StrategyBuilder {
	eb.md = md
	return eb
}

// BaseDelay helps implement StrategyBuilder.
func (eb *exponentialBuilder) Build() Strategy {
	return Exponential{
		baseDelay:  eb.bd,
		multiplier: eb.mltpr,
		jitter:     eb.jitter,
		maxDelay:   eb.md,
	}
}

// Exponential implements exponential backoff algorithm as defined in
// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md.
type Exponential struct {
	// baseDelay is the amount of time to backoff after the first connection
	// failure.
	baseDelay time.Duration
	// multiplier is the factor with which to multiply backoffs after a failed
	// retry.
	multiplier float64
	// jitter is the factor with which backoffs are randomized.
	jitter float64
	// maxDelay is the upper bound of backoff delay.
	maxDelay time.Duration
}

// Backoff returns the amount of time to wait before the next retry given the
// number of retries.
// Helps implement Strategy.
func (bc Exponential) Backoff(retries int) time.Duration {
	if retries == 0 {
		return bc.baseDelay
	}
	backoff, max := float64(bc.baseDelay), float64(bc.maxDelay)
	for backoff < max && retries > 0 {
		backoff *= bc.multiplier
		retries--
	}
	if backoff > max {
		backoff = max
	}
	// Randomize backoff delays so that if a cluster of requests start at
	// the same time, they won't operate in lockstep.
	backoff *= 1 + bc.jitter*(grpcrand.Float64()*2-1)
	if backoff < 0 {
		return 0
	}
	return time.Duration(backoff)
}
