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

// Package tracing implements the OpenTelemetry carrier for context propagation
// in gRPC tracing.
package tracing

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// propagationDirection specifies whether the propagation is incoming or
// outgoing.
type propagationDirection int

const (
	incoming propagationDirection = iota // incoming propagation direction
	outgoing                             // outgoing propagation direction
)

// Carrier is a TextMapCarrier that uses `context.Context` to store and
// retrieve any propagated key-value pairs in text format. The propagation
// direction (incoming or outgoing) determines whether the `Keys()` method
// should return keys from the incoming or outgoing context metadata.
type Carrier struct {
	ctx       context.Context
	direction propagationDirection
}

// NewIncomingCarrier creates a new Carrier with the incoming context from the
// given context and incoming propagation direction. The incoming carrier
// should be used with propagator's `Extract()` method.
func NewIncomingCarrier(ctx context.Context) *Carrier {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return &Carrier{ctx: metadata.NewIncomingContext(ctx, metadata.MD{}), direction: incoming}
	}
	return &Carrier{ctx: metadata.NewIncomingContext(ctx, md), direction: incoming}
}

// NewOutgoingCarrier creates a new Carrier with the outgoing context from the
// given context and outgoing propagation direction. The outgoing carrier
// should be used with propagator's `Inject()` method.
func NewOutgoingCarrier(ctx context.Context) *Carrier {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return &Carrier{ctx: metadata.NewOutgoingContext(ctx, metadata.MD{}), direction: outgoing}
	}
	return &Carrier{ctx: metadata.NewOutgoingContext(ctx, md), direction: outgoing}
}

// Get returns the string value associated with the passed key from the
// carrier's incoming context metadata.
//
// It returns an empty string if the key is not present in the carrier's
// context or if the value associated with the key is empty.
//
// If multiple values are present for a key, it returns the last one.
func (c *Carrier) Get(key string) string {
	values := metadata.ValueFromIncomingContext(c.ctx, key)
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// Set stores the key-value pair in the carrier's outgoing context metadata.
//
// If the key already exists, given value is appended to the last.
func (c *Carrier) Set(key, value string) {
	c.ctx = metadata.AppendToOutgoingContext(c.ctx, key, value)
}

// Keys returns the keys stored in the carrier's context metadata. It returns
// keys from outgoing context metadata if propagation direction is outgoing and
// if propagation direction is incoming, it returns keys from incoming context
// metadata. In all other cases, it returns nil.
func (c *Carrier) Keys() []string {
	var md metadata.MD
	var ok bool

	switch c.direction {
	case outgoing:
		md, ok = metadata.FromOutgoingContext(c.ctx)
	case incoming:
		md, ok = metadata.FromIncomingContext(c.ctx)
	}

	if !ok {
		return nil
	}
	keys := make([]string, 0, len(md))
	for key := range md {
		keys = append(keys, key)
	}
	return keys
}

// Context returns the underlying context associated with the Carrier.
func (c *Carrier) Context() context.Context {
	return c.ctx
}
