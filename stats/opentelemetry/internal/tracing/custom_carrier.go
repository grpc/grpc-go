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
	"errors"

	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
)

// GRPCTraceBinHeaderKey is the gRPC metadata header key `grpc-trace-bin` used
// to propagate trace context in binary format.
const GRPCTraceBinHeaderKey = "grpc-trace-bin"

// CustomCarrier is a TextMapCarrier that uses gRPC context to store and
// retrieve any propagated key-value pairs in text format along with binary
// format for `grpc-trace-bin` header
type CustomCarrier struct {
	propagation.TextMapCarrier

	Ctx *context.Context
}

// NewCustomCarrier creates a new CustomMapCarrier with
// the given context.
func NewCustomCarrier(ctx context.Context) CustomCarrier {
	return CustomCarrier{
		Ctx: &ctx,
	}
}

// Get returns the string value associated with the passed key from the gRPC
// context.
func (c CustomCarrier) Get(key string) string {
	md, ok := metadata.FromIncomingContext(*c.Ctx)
	if !ok {
		return ""
	}
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Set stores the key-value pair in string format in the gRPC context.
// If the key already exists, its value will be overwritten.
func (c CustomCarrier) Set(key, value string) {
	md, ok := metadata.FromOutgoingContext(*c.Ctx)
	if !ok {
		md = metadata.MD{}
	}
	md.Set(key, value)
	*c.Ctx = metadata.NewOutgoingContext(*c.Ctx, md)
}

// GetBinary returns the binary value from the gRPC context in the incoming RPC,
// associated with the header `grpc-trace-bin`.
func (c CustomCarrier) GetBinary() ([]byte, error) {
	values := stats.Trace(*c.Ctx)
	if len(values) == 0 {
		return nil, errors.New("`grpc-trace-bin` header not found")
	}

	return values, nil
}

// SetBinary sets the binary value to the gRPC context, which will be sent in
// the outgoing RPC with the header grpc-trace-bin.
func (c CustomCarrier) SetBinary(value []byte) {
	*c.Ctx = stats.SetTrace(*c.Ctx, value)
}

// Keys lists the keys stored in the gRPC context for the outgoing RPC.
func (c CustomCarrier) Keys() []string {
	md, _ := metadata.FromOutgoingContext(*c.Ctx)
	keys := make([]string, 0, len(md))
	for k := range md {
		keys = append(keys, k)
	}
	return keys
}
