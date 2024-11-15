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

// CustomCarrier is a TextMapCarrier that uses `context.Context` to store and
// retrieve any propagated key-value pairs in text format.
type CustomCarrier struct {
	ctx context.Context
}

// NewCustomCarrier creates a new CustomCarrier with
// the given context.
func NewCustomCarrier(ctx context.Context) *CustomCarrier {
	return &CustomCarrier{ctx: ctx}
}

// Get returns the string value associated with the passed key from the
// carrier's context metadata.
//
// It returns an empty string if the key is not present in the carrier's
// context or if the value associated with the key is empty.
//
// If multiple values are present for a key, it returns the last one.
func (c *CustomCarrier) Get(key string) string {
	values := metadata.ValueFromIncomingContext(c.ctx, key)
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// Set stores the key-value pair in the carrier's context metadata.
//
// If the key already exists, given value is appended to the last.
func (c *CustomCarrier) Set(key, value string) {
	c.ctx = metadata.AppendToOutgoingContext(c.ctx, key, value)
}

// Keys returns the keys stored in the carrier's context metadata. It returns
// keys from both outgoing and incoming context metadata.
func (c *CustomCarrier) Keys() []string {
	var keys []string
	md, ok := metadata.FromOutgoingContext(c.ctx)
	if ok {
		keys = make([]string, 0, len(md))
		for key := range md {
			keys = append(keys, key)
		}
	}
	md, ok = metadata.FromIncomingContext(c.ctx)
	if ok {
		keys = make([]string, 0, len(md))
		for key := range md {
			keys = append(keys, key)
		}
	}
	return keys
}

// Context returns the underlying context associated with the CustomCarrier.
func (c *CustomCarrier) Context() context.Context {
	return c.ctx
}
