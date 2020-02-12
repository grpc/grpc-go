/*
 *
 * Copyright 2020 gRPC authors.
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

// Package client provides an implementation of the RouteLookupService client
// with adaptive throttling built in.
package client

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
)

// For gRPC services using RLS, the value of target_type in the
// RouteLookupServiceRequest will be set to this.
const grpcTargetType = "grpc"

// An interface which captures the methods exported by the adaptive throttler
// implementation. Allows overriding from unittests.
type adaptiveThrottlerInterface interface {
	ShouldThrottle() bool
	RegisterBackendResponse(bool)
}

// Client is a simple wrapper around a RouteLookupService client with adaptive
// throttling.
type Client struct {
	cc        *grpc.ClientConn
	stub      rlspb.RouteLookupServiceClient
	throttler adaptiveThrottlerInterface
}

// New returns an RLS client with the provided arguments. The RLS LB policy
// creates a grpc.ClientConn to the RLS server using appropriate credentials
// from the parent channel, and creates an adaptive throttler implementation
// using default values and passes them here.
func New(cc *grpc.ClientConn, throttler adaptiveThrottlerInterface) *Client {
	return &Client{
		cc:        cc,
		stub:      rlspb.NewRouteLookupServiceClient(cc),
		throttler: throttler,
	}
}

// LookupArgs wraps the values to be sent in an RLS request.
type LookupArgs struct {
	// Target is the user's dial target, e.g. "firestore.googleapis.com".
	Target string
	// Path is full RPC path, e.g. "/service/method".
	Path string
	// KeyMap is the request's keys built by the RLS LB using the
	// grpcKeyBuilders received in the service config.
	KeyMap map[string]string
}

// LookupResult wraps the values received in an RLS response.
type LookupResult struct {
	// Target is the actual addressable entity to use for routing decision,
	// e.g. "us_east_1.firestore.googleapis.com".
	Target string
	// HeaderData contains optional header values to pass along to the backend
	// in the X-Google-RLS-Data header (cached by the RLS LB policy).
	HeaderData string
}

// ErrRequestThrottled is the error returned by Lookup when the adaptive
// throttler decides that the request should be throttled.
var ErrRequestThrottled = errors.New("RLS request throttled")

// Lookup makes a RouteLookup RPC using data provided in args. It is assumed
// that a reasonable deadline is set on the provided context so that RPCs
// taking too long to complete fail automatically.
func (c *Client) Lookup(ctx context.Context, args *LookupArgs) (*LookupResult, error) {
	if c.throttler.ShouldThrottle() {
		return nil, ErrRequestThrottled
	}

	resp, err := c.stub.RouteLookup(ctx, &rlspb.RouteLookupRequest{
		Server:     args.Target,
		Path:       args.Path,
		TargetType: grpcTargetType,
		KeyMap:     args.KeyMap,
	})
	c.throttler.RegisterBackendResponse(err != nil)
	if err != nil {
		return nil, err
	}

	return &LookupResult{Target: resp.GetTarget(), HeaderData: resp.GetHeaderData()}, nil
}
