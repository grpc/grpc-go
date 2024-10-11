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

// Package tracing implements the OpenTelemetry context propagators for
// gRPC tracing.
package tracing

import (
	"errors"

	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

const GRPCTraceBinHeaderKey = "grpc-trace-bin"

// CustomMapCarrier is a TextMapCarrier that uses metadata.MD held in memory
// as a storage medium for propagated key-value pairs in both text and binary.
type CustomMapCarrier struct {
	propagation.TextMapCarrier
	// MD is metadata.MD to store values in both string and binary format.
	MD metadata.MD
}

// NewCustomCarrier creates a new CustomMapCarrier with
// embedded metadata.MD.
func NewCustomCarrier(md metadata.MD) CustomMapCarrier {
	return CustomMapCarrier{
		MD: md,
	}
}

// Get returns the string value associated with the passed key.
func (c CustomMapCarrier) Get(key string) string {
	values := c.MD[key]
	if len(values) == 0 {
		return ""
	}

	return values[0]
}

// Set stores the key-value pair in string format.
func (c CustomMapCarrier) Set(key, value string) {
	c.MD.Set(key, value)
}

// GetBinary returns the string value associated with the passed key,
// only if the passed key is `grpc-trace-bin`.
func (c CustomMapCarrier) GetBinary(key string) ([]byte, error) {
	if key != GRPCTraceBinHeaderKey {
		return nil, errors.New("only support 'grpc-trace-bin' binary header")
	}

	// Retrieve the binary data directly from metadata
	values := c.MD[key]
	if len(values) == 0 {
		return nil, errors.New("key not found")
	}

	return []byte(values[0]), nil
}

// SetBinary sets the binary value for the given key in the metadata,
// only if passed key is `grpc-trace-bin`.
func (c CustomMapCarrier) SetBinary(key string, value []byte) {
	// Only support 'grpc-trace-bin' binary header.
	if key == GRPCTraceBinHeaderKey {
		// Set the raw binary value in the metadata
		c.MD[key] = []string{string(value)}
	}
}

// Keys lists the keys stored in carriers metadata.MD.
func (c CustomMapCarrier) Keys() []string {
	keys := make([]string, 0, len(c.MD))
	for k := range c.MD {
		keys = append(keys, k)
	}
	return keys
}
