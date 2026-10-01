/*
 *
 * Copyright 2026 gRPC authors.
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

// Package sharding provides communication with an external sharding service
// to fetch shard assignments and make them available to the autosharding
// load balancing policy.
package sharding

import (
	"time"

	"google.golang.org/grpc"
)

// Slice represents a key range and its assigned endpoints. The end key of the
// range is not stored here because it is always the start key of the next
// Slice, or nil if this is the last Slice. This is guaranteed by the client
// responsible for generating the Assignment.
type Slice struct {
	StartKey  []byte // Inclusive start key of the key-range
	Endpoints []int  // Indices into Assignment.EndpointNames
}

// Assignment represents a complete snapshot of sharding assignments, and is
// expected to cover the entire key range.
type Assignment struct {
	Slices        []Slice  // Sorted by StartKey
	EndpointNames []string // Complete list of endpoint names
	Generation    int64
}

// TODO(easwars): Implement the autosharding client that connects to the
// autosharding service and receives assignments.
type autoshardingClient struct {
	cc                 grpc.ClientConnInterface
	target             string
	uuid               string
	timeout            time.Duration
	onAssignmentUpdate func(*Assignment)
	onAssignmentError  func(error)
}

func (c *autoshardingClient) close() {
	// TODO(easwars): Implement the close method to clean up resources used by
	// the client.

	// TODO(easwars): Guarantee that once close returns, no more callbacks will
	// be invoked.
}

// ClientOptions contains the options for creating a new autosharding client.
type ClientOptions struct {
	CC                       grpc.ClientConnInterface // The gRPC channel to the sharding service.
	AutoshardingTarget       string                   // The target for the autosharding service.
	UUID                     string                   // The unique identifier for this client.
	InitialAssignmentTimeout time.Duration            // The timeout for the initial assignment fetch.
	OnAssignmentUpdate       func(*Assignment)        // Callback invoked when a new assignment is received from the server.
	OnAssignmentError        func(error)              // Callback invoked when there is an error fetching the assignment.
}

// NewClient creates a new autosharding client that connects to the sharding
// service using the provided options and fetches assignments. The return value
// is a cancel function that the caller must invoke when they no longer need the
// autosharding client.
func NewClient(opts ClientOptions) func() {
	client := &autoshardingClient{
		cc:                 opts.CC,
		target:             opts.AutoshardingTarget,
		uuid:               opts.UUID,
		timeout:            opts.InitialAssignmentTimeout,
		onAssignmentUpdate: opts.OnAssignmentUpdate,
		onAssignmentError:  opts.OnAssignmentError,
	}

	// TODO(easwars): Spawn a goroutine to connect to the sharding service to
	// fetch assignments, and invoke the appropriate callbacks on assignment
	// updates or errors.
	return client.close
}
