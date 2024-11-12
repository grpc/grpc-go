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
	"google.golang.org/grpc/metadata"
)

// CustomCarrier is a TextMapCarrier that uses `*metadata.MD` to store and
// retrieve any propagated key-value pairs in text format.
type CustomCarrier struct {
	md *metadata.MD
}

// NewCustomCarrier creates a new CustomCarrier with
// the given metadata.
func NewCustomCarrier(md *metadata.MD) *CustomCarrier {
	return &CustomCarrier{md: md}
}

// Get returns the string value associated with the passed key from the
// carrier's metadata.
//
// It returns an empty string if the key is not present in the carrier's
// metadata or if the value associated with the key is empty.
func (c *CustomCarrier) Get(key string) string {
	values := c.md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Set stores the key-value pair in the carrier's metadata.
//
// If the key already exists, its value is overwritten.
func (c *CustomCarrier) Set(key, value string) {
	c.md.Set(key, value)
}

// Keys returns the keys stored in the carrier's metadata.
func (c *CustomCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.md))
	for key := range *c.md {
		keys = append(keys, key)
	}
	return keys
}

// Metadata returns the underlying metadata associated with the CustomCarrier.
func (c *CustomCarrier) Metadata() *metadata.MD {
	return c.md
}
