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
	"encoding/base64"
	"strings"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
)

// GRPCTraceBinHeaderKey is the gRPC metadata header key `grpc-trace-bin` used
// to propagate trace context in binary format.
const GRPCTraceBinHeaderKey = "grpc-trace-bin"

var logger = grpclog.Component("otel-plugin")

// CustomCarrier is a TextMapCarrier that uses gRPC context to store and
// retrieve any propagated key-value pairs in text format along with binary
// format for `grpc-trace-bin` header.
type CustomCarrier struct {
	ctx context.Context
}

// NewCustomCarrier creates a new CustomMapCarrier with
// the given context.
func NewCustomCarrier(ctx context.Context) *CustomCarrier {
	return &CustomCarrier{ctx: ctx}
}

// Get returns the string value associated with the passed key from the gRPC
// context. It returns an empty string in any of the following cases:
// - the key is not present in the context
// - the value associated with the key is empty
// - the key ends with "-bin" and is not `grpc-trace-bin`
//
// If the key is `grpc-trace-bin`, it retrieves the binary value using
// `stats.Trace()` and then base64 encodes it before returning. For all other
// string keys, it retrieves the value from the context's metadata.
func (c *CustomCarrier) Get(key string) string {
	if key == GRPCTraceBinHeaderKey {
		return base64.StdEncoding.EncodeToString(stats.Trace(c.ctx))
	}
	if strings.HasSuffix(key, "-bin") && key != GRPCTraceBinHeaderKey {
		logger.Warningf("encountered a binary header %s which is not: %s", key, GRPCTraceBinHeaderKey)
		return ""
	}

	md, ok := metadata.FromIncomingContext(c.ctx)
	if !ok {
		return ""
	}
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Set stores the key-value pair in the gRPC context. If the key already
// exists, its value will be overwritten. If the key ends with "-bin" and is
// not `grpc-trace-bin`, the key-value pair is not stored.
//
// If the key is `grpc-trace-bin`, it base64 decodes the `value` and stores the
// resulting binary data using `stats.SetTrace()`. For all other keys, it stores
// the key-value pair in the string format in context's metadata.
func (c *CustomCarrier) Set(key, value string) {
	if key == GRPCTraceBinHeaderKey {
		b, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			logger.Errorf("encountered error in decoding %s value", GRPCTraceBinHeaderKey)
			return
		}
		c.ctx = stats.SetTrace(c.ctx, b)
		return
	}
	if strings.HasSuffix(key, "-bin") && key != GRPCTraceBinHeaderKey {
		logger.Warningf("encountered a binary header %s which is not: %s", key, GRPCTraceBinHeaderKey)
		return
	}

	md, ok := metadata.FromOutgoingContext(c.ctx)
	if !ok {
		md = metadata.MD{}
	}
	md.Set(key, value)
	c.ctx = metadata.NewOutgoingContext(c.ctx, md)
}

// GetBinary returns the binary value from the gRPC context in the incoming
// RPC, associated with the header `grpc-trace-bin`. If header is not found or
// is empty, it returns nil.
func (c *CustomCarrier) GetBinary() []byte {
	values := stats.Trace(c.ctx)
	if len(values) == 0 {
		return nil
	}
	return values
}

// SetBinary sets the binary value in the carrier's context, which will be sent
// in the outgoing RPC with the header `grpc-trace-bin`.
func (c *CustomCarrier) SetBinary(value []byte) {
	c.ctx = stats.SetTrace(c.ctx, value)
}

// Keys returns the keys stored in the carrier's context.
func (c *CustomCarrier) Keys() []string {
	md, _ := metadata.FromOutgoingContext(c.ctx)
	keys := make([]string, 0, len(md)+1) // +1 for grpc-trace-bin (if present)
	for k := range md {
		keys = append(keys, k)
	}
	if stats.OutgoingTrace(c.ctx) != nil {
		keys = append(keys, GRPCTraceBinHeaderKey) // add grpc-trace-bin key if grpc-trace-bin is present in the context
	}
	return keys
}

// Context returns the underlying context associated with the CustomCarrier.
func (c *CustomCarrier) Context() context.Context {
	return c.ctx
}
