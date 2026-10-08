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
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
	igrpclog "google.golang.org/grpc/internal/grpclog"

	asgrpc "google.golang.org/grpc/balancer/autosharding/internal/proto"
	aspb "google.golang.org/grpc/balancer/autosharding/internal/proto"
)

const (
	// The error_message field in the AssignmentAck message has a maximum length of
	// 512 Unicode code points.
	maxErrMsgLenForAssignmentACK = 512

	// The start and end keys of a slice are at most 512 bytes long.
	maxKeyLen = 512
)

var (
	logger                      = grpclog.Component("autosharding")
	errInitialAssignmentTimeout = errors.New("autosharding: initial_assignment_timeout fired before a valid assignment was received")
)

// A convenience type alias for brevity.
type shardingStream = grpc.BidiStreamingClient[aspb.WatchShardingAssignmentRequest, aspb.WatchShardingAssignmentResponse]

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

// autoshardingClient is a client that connects to the sharding service and
// fetches assignments. It runs a goroutine that maintains a streaming RPC to the
// sharding service, and invokes callbacks when new assignments are received or
// when there is an error fetching the assignment.
type autoshardingClient struct {
	// The following fields are initialized once and never modified, so they
	// don't need to be protected by a mutex.
	cc                 grpc.ClientConnInterface
	target             string
	uuid               string
	onAssignmentUpdate func(*Assignment)
	onAssignmentError  func(error)
	backoff            func(int) time.Duration
	logger             *igrpclog.PrefixLogger
	runnerDoneCh       chan struct{}
	cancel             func()

	// The following fields are only accessed from the run goroutine, so they
	// don't need to be protected by a mutex.
	chunksReceived   []*aspb.AssignmentChunk
	latestGeneration int64

	// mu guards the below fields and guarantees mutual exclusion between the
	// run goroutine that invokes callbacks to report error or valid assignment
	// and the initial assignment timer's after func that invokes the callback
	// to report an error.
	mu                      sync.Mutex
	validAssignmentReported bool
	closed                  bool
	initialAssignmentTimer  *time.Timer
}

// run is the main loop of the autosharding client. It maintains a streaming RPC
// to the sharding service, and invokes callbacks when new assignments are
// received or when there is an error fetching the assignment. It will retry the
// streaming RPC with backoff if it fails without receiving a valid assignment.
func (c *autoshardingClient) run(ctx context.Context) {
	defer close(c.runnerDoneCh)

	client := asgrpc.NewAutoshardingServiceClient(c.cc)
	runStream := func() error {
		c.chunksReceived = nil
		streamCtx, streamCancel := context.WithCancel(ctx)
		defer streamCancel()

		stream, err := client.WatchShardingAssignment(streamCtx, grpc.WaitForReady(true))
		if err != nil {
			c.reportError(fmt.Errorf("autosharding: failed to create a new stream to the sharding service: %v", err))
			if ctx.Err() != nil {
				// Force backoff.RunF to exit by returning the context error
				// until https://github.com/grpc/grpc-go/issues/9485 is fixed.
				return ctx.Err()
			}
			return nil
		}

		firstMsg := &aspb.WatchShardingAssignmentRequest{
			InitialClientConfig: &aspb.InitialClientConfig{
				Target:           c.target,
				ClientUuid:       c.uuid,
				LatestGeneration: c.latestGeneration,
			},
		}
		if err := stream.Send(firstMsg); err != nil {
			// If Send fails, continue and report the error from Recv instead.
			if c.logger.V(2) {
				c.logger.Infof("Failed to send initial request to the sharding service: %v", err)
			}
		}

		// Backoff state is reset upon successful receipt of at least one valid
		// assignment from the server.
		resetBackoff := c.recvAssignments(stream)
		if ctx.Err() != nil {
			// Force backoff.RunF to exit by returning the context error until
			// https://github.com/grpc/grpc-go/issues/9485 is fixed.
			return ctx.Err()
		}
		if resetBackoff {
			return backoff.ErrResetBackoff
		}
		return nil
	}
	backoff.RunF(ctx, runStream, c.backoff)
}

// recvAssignments receives messages from the sharding service and assembles
// chunks into a complete assignment. Returns true if a valid assignment was
// received, false otherwise.
//
// Only invoked from the run goroutine.
func (c *autoshardingClient) recvAssignments(stream shardingStream) bool {
	validAssignmentReceived := false
	for {
		msg, err := stream.Recv()
		if err != nil {
			c.reportError(fmt.Errorf("autosharding: failed to receive a message from the sharding service: %v", err))
			return validAssignmentReceived
		}

		switch {
		case msg.GetChunk() != nil:
			chunk := msg.GetChunk()
			c.chunksReceived = append(c.chunksReceived, chunk)
		case msg.GetMetadata() != nil:
			if c.handleMetadata(stream, msg.GetMetadata().GetGeneration()) {
				validAssignmentReceived = true
			}
		case msg.GetConfig() != nil:
			// A119 asks for this to be ignored for now.
			if c.logger.V(2) {
				c.logger.Infof("Ignoring a LoadReportingConfig message: %v", msg)
			}
		default:
			if c.logger.V(2) {
				c.logger.Infof("Ignoring unsupported message: %v", msg)
			}
		}
	}
}

// truncateRunes truncates the input string to a maximum of maxRunes runes
// without reallocating the string.
func truncateRunes(s string, maxRunes int) string {
	count := 0
	for i := range s {
		if count >= maxRunes {
			return s[:i]
		}
		count++
	}
	return s
}

// sendAssignmentACK sends an AssignmentAck message to the sharding service with
// the given generation, accepted flag, and error message. It also clears the
// chunksReceived field to prepare for the next assignment.
//
// Only invoked from the run goroutine.
func (c *autoshardingClient) sendAssignmentACK(stream shardingStream, gen int64, accepted bool, errMsg string) {
	c.chunksReceived = nil

	// We don't report an error if the Send fails, because there will be
	// subsequent Recv on the same stream from recvAssignments which will fail,
	// and we will report that error instead.
	if err := stream.Send(&aspb.WatchShardingAssignmentRequest{
		AssignmentAck: &aspb.AssignmentAck{
			Generation:   gen,
			Accepted:     accepted,
			ErrorMessage: truncateRunes(errMsg, maxErrMsgLenForAssignmentACK),
		},
	}); err != nil {
		c.logger.Errorf("Failed to send an AssignmentAck message: %v", err)
	}
}

// handleMetadata processes the received metadata message that indicates the end
// of a sharding assignment. It validates the assignment, and sends an
// AssignmentAck message to the sharding service. It returns a boolean
// indicating whether a valid assignment was accepted.
//
// Only invoked from the run goroutine.
func (c *autoshardingClient) handleMetadata(stream shardingStream, gen int64) bool {
	if c.logger.V(2) {
		c.logger.Infof("Received metadata with generation: %d, previous generation: %d", gen, c.latestGeneration)
	}

	// Drop stale generations.
	if gen <= c.latestGeneration {
		c.sendAssignmentACK(stream, gen, false, fmt.Sprintf("received generation %d is not greater than the latest generation %d", gen, c.latestGeneration))
		return false
	}

	// NACK the case where metadata was received before any chunks were
	// received.
	if len(c.chunksReceived) == 0 {
		errMsg := fmt.Sprintf("received metadata with generation %d, but no chunks were received", gen)
		c.sendAssignmentACK(stream, gen, false, errMsg)
		c.reportError(fmt.Errorf("autosharding: %s", errMsg))
		return false
	}

	sortedSlices, endpoints, errMsg := validateAssignment(c.chunksReceived)

	// NACK assignments with no usable slices post validation.
	if len(sortedSlices) == 0 {
		msg := fmt.Sprintf("no valid slice assignments in generation %d: %s", gen, errMsg)
		c.sendAssignmentACK(stream, gen, false, msg)
		c.reportError(fmt.Errorf("autosharding: %s", msg))
		return false
	}

	// Now, we could have some valid slice assignments and some invalid ones. In
	// this case, we need to accept the assignment, and send an ACK, but also
	// include an error message if there were any invalid slice assignments.
	c.latestGeneration = gen
	assignment, gapsErrMsg := buildAssignment(sortedSlices, endpoints, gen)
	if gapsErrMsg != "" {
		if errMsg == "" {
			errMsg = gapsErrMsg
		} else {
			errMsg = fmt.Sprintf("%s; %s", errMsg, gapsErrMsg)
		}
	}
	c.sendAssignmentACK(stream, gen, true, errMsg)
	c.reportAssignment(assignment)
	return true
}

// validateAssignment validates the assignment received from the sharding
// service. It returns the list of valid slice assignments (sorted by start
// key), the combined list of endpoints, and an error message if there were any
// validation errors. A slice assignment without a slice is treated like one
// with an empty slice, i.e. it covers the whole keyspace ["", +∞).
//
// The validation rules are:
//  1. The start and end keys of the slice must not be longer than 512 bytes.
//  2. The start key of the slice must be less than the end key of the slice.
//  3. No overlapping slices are allowed. Therefore, the end key of a slice
//     must be less than or equal to the start key of the next slice. In this
//     case, we need to drop all overlapping slices.
//  4. The indices in the slice's endpoints must be valid indices into the
//     combined list of endpoints.
func validateAssignment(chunks []*aspb.AssignmentChunk) ([]*aspb.SliceAssignment, []string, string) {
	// Combine endpoint state from all chunks into a single list of endpoints.
	// This is required because the assignments have indices into the combined
	// list of endpoints, and we need to ensure that the indices are valid.
	combinedEndpoints := make([]string, 0)
	for _, chunk := range chunks {
		for _, endpoint := range chunk.GetEndpoints() {
			combinedEndpoints = append(combinedEndpoints, endpoint.GetEndpoint())
		}
	}

	var errs []string
	combinedSliceAssignments := make([]*aspb.SliceAssignment, 0)
	for _, chunk := range chunks {
		combinedSliceAssignments = append(combinedSliceAssignments, chunk.GetSliceAssignments()...)
	}
	if len(combinedSliceAssignments) == 0 {
		return nil, nil, "no slice assignments received"
	}

	// Validate individual slice assignments and filter out invalid ones.
	var numLargeStartKeys, numLargeEndKeys, numStartGreaterThanEnd, numInvalidEndpointSlices int
	var validSlices []*aspb.SliceAssignment
	for _, slice := range combinedSliceAssignments {
		startKey := slice.GetSlice().GetStartKey() // nil or []byte{} both mean b""
		endKey := slice.GetSlice().GetEndKey()     // nil means +∞

		if len(startKey) > maxKeyLen {
			numLargeStartKeys++
			continue
		}
		if len(endKey) > maxKeyLen {
			numLargeEndKeys++
			continue
		}

		// The end_key field inside the Slice message when unset represents the
		// last slice in the assignment.
		if endKey != nil && bytes.Compare(startKey, endKey) >= 0 {
			numStartGreaterThanEnd++
			continue
		}

		endpointsValid := true
		for _, epState := range slice.GetEndpoints() {
			epIdx := int(epState.GetEndpointIndex())
			if epIdx < 0 || epIdx >= len(combinedEndpoints) {
				numInvalidEndpointSlices++
				endpointsValid = false
				break
			}
		}
		if !endpointsValid {
			continue
		}
		validSlices = append(validSlices, slice)
	}
	if numLargeStartKeys > 0 {
		errs = append(errs, fmt.Sprintf("%d slice(s) with start_key longer than 512 bytes", numLargeStartKeys))
	}
	if numLargeEndKeys > 0 {
		errs = append(errs, fmt.Sprintf("%d slice(s) with end_key longer than 512 bytes", numLargeEndKeys))
	}
	if numStartGreaterThanEnd > 0 {
		errs = append(errs, fmt.Sprintf("%d slice(s) with start_key >= end_key", numStartGreaterThanEnd))
	}
	if numInvalidEndpointSlices > 0 {
		errs = append(errs, fmt.Sprintf("%d slice(s) with invalid endpoint indices", numInvalidEndpointSlices))
	}

	slices.SortFunc(validSlices, func(a, b *aspb.SliceAssignment) int {
		// bytes.Compare performs a lexicographical comparison of the two byte
		// slices, returning an integer indicating their relative order. It
		// returns a negative value if a < b, zero if a == b, and a positive
		// value if a > b.
		return bytes.Compare(a.GetSlice().GetStartKey(), b.GetSlice().GetStartKey())
	})

	// Drop every slice that overlaps another slice. validSlices is sorted by
	// start_key, so slice i overlaps:
	//  - an earlier slice if and only if it starts before the largest end_key
	//    seen so far.
	//  - a later slice if and only if the next slice starts before slice i
	//    ends.
	// A nil end_key means +∞.
	var maxEnd []byte   // Largest non-nil end_key in validSlices[:i].
	seenNilEnd := false // Whether any slice in validSlices[:i] has a nil (+∞) end_key.
	var numOverlaps int
	filteredSliceAssignments := make([]*aspb.SliceAssignment, 0, len(validSlices))
	for i, slice := range validSlices {
		startKey, endKey := slice.GetSlice().GetStartKey(), slice.GetSlice().GetEndKey()
		overlapsEarlier := seenNilEnd || bytes.Compare(startKey, maxEnd) < 0
		overlapsLater := i+1 < len(validSlices) && (endKey == nil || bytes.Compare(validSlices[i+1].GetSlice().GetStartKey(), endKey) < 0)
		if overlapsEarlier || overlapsLater {
			numOverlaps++
		} else {
			filteredSliceAssignments = append(filteredSliceAssignments, slice)
		}
		if endKey == nil {
			seenNilEnd = true
		} else if bytes.Compare(endKey, maxEnd) > 0 {
			maxEnd = endKey
		}
	}
	if numOverlaps > 0 {
		errs = append(errs, fmt.Sprintf("%d overlapping slices", numOverlaps))
	}

	return filteredSliceAssignments, combinedEndpoints, strings.Join(errs, "; ")
}

// buildAssignment builds an Assignment from the given slice assignments and
// endpoint names. It also fills in any gaps in the key range with empty slices
// that have no endpoints.
//
// sortedSlices must contain at least one slice assignment, and must be sorted
// by start key. The end key of the last slice may be nil, indicating that it
// extends to the end of the key range.
//
// Returns the built Assignment and a string describing any errors encountered
// while building it.
func buildAssignment(sortedSlices []*aspb.SliceAssignment, endpoints []string, gen int64) (*Assignment, string) {
	assignment := &Assignment{
		EndpointNames: endpoints,
		Generation:    gen,
	}

	// Handle gaps in assignments.
	var numGaps int
	for i, slice := range sortedSlices {
		if i == 0 && len(slice.GetSlice().GetStartKey()) != 0 {
			// If the first slice does not start with an empty key, we need to
			// fill the gap with a Slice with no endpoints.
			firstSlice := Slice{
				StartKey:  []byte{},
				Endpoints: []int{},
			}
			assignment.Slices = []Slice{firstSlice}
			numGaps++
		}

		if i > 0 {
			prevEndKey := sortedSlices[i-1].GetSlice().GetEndKey()
			currStartKey := slice.GetSlice().GetStartKey()
			if bytes.Compare(prevEndKey, currStartKey) < 0 {
				// If there is a gap between the previous slice's end key and
				// the current slice's start key, we need to fill the gap with a
				// Slice with no endpoints.
				gapSlice := Slice{
					StartKey:  prevEndKey,
					Endpoints: []int{},
				}
				assignment.Slices = append(assignment.Slices, gapSlice)
				numGaps++
			}
		}

		// Finally, we can add the current slice to the assignment.
		assignment.Slices = append(assignment.Slices, Slice{
			StartKey:  slice.GetSlice().GetStartKey(),
			Endpoints: make([]int, len(slice.GetEndpoints())),
		})
		for j, epState := range slice.GetEndpoints() {
			assignment.Slices[len(assignment.Slices)-1].Endpoints[j] = int(epState.GetEndpointIndex())
		}
	}
	if lastEndKey := sortedSlices[len(sortedSlices)-1].GetSlice().GetEndKey(); lastEndKey != nil {
		// If the last slice does not end with a nil key, we need to fill the
		// gap with a Slice with no endpoints.
		lastSlice := Slice{
			StartKey:  lastEndKey,
			Endpoints: []int{},
		}
		assignment.Slices = append(assignment.Slices, lastSlice)
		numGaps++
	}
	if numGaps > 0 {
		return assignment, fmt.Sprintf("encountered %d gap(s) in the assignment", numGaps)
	}
	return assignment, ""
}

// reportAssignment reports a valid assignment to the onAssignmentUpdate
// callback. It also stops the initial assignment timer, and sets the
// validAssignmentReported flag to true.
//
// Only invoked from the run goroutine.
func (c *autoshardingClient) reportAssignment(a *Assignment) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	if c.logger.V(2) {
		c.logger.Infof("Reporting new assignment with generation %d, %d slices and %d endpoints", a.Generation, len(a.Slices), len(a.EndpointNames))
	}

	if c.onAssignmentUpdate != nil {
		c.onAssignmentUpdate(a)
	}
	c.validAssignmentReported = true
	if c.initialAssignmentTimer != nil {
		c.initialAssignmentTimer.Stop()
		c.initialAssignmentTimer = nil
	}
}

// reportError reports an error to the onAssignmentError callback only if no
// valid assignment has been reported yet.
//
// Called from the run goroutine and from the initial assignment timer's
// AfterFunc, so we need to lock the mutex to ensure mutual exclusion.
func (c *autoshardingClient) reportError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	if c.logger.V(2) {
		c.logger.Infof("Reporting error: %v", err)
	}

	if !c.validAssignmentReported {
		if c.onAssignmentError != nil {
			c.onAssignmentError(err)
		}
	}
}

// close closes the autosharding client, cancelling the context used for the
// streaming RPC and waiting for the run goroutine to exit.
//
// It is guaranteed that no more callbacks will be invoked once this returns.
func (c *autoshardingClient) close() {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.cancel()
	}
	if c.initialAssignmentTimer != nil {
		c.initialAssignmentTimer.Stop()
		c.initialAssignmentTimer = nil
	}
	c.mu.Unlock()

	<-c.runnerDoneCh
	if c.logger.V(2) {
		c.logger.Infof("Shutdown")
	}
}

// ClientOptions contains the options for creating a new autosharding client.
type ClientOptions struct {
	CC                       grpc.ClientConnInterface // The gRPC channel to the sharding service.
	AutoshardingTarget       string                   // The target for the autosharding service.
	UUID                     string                   // The unique identifier for this client.
	InitialAssignmentTimeout time.Duration            // The timeout for the initial assignment fetch.
	OnAssignmentUpdate       func(*Assignment)        // Invoked when a new assignment is received. Must not block.
	OnAssignmentError        func(error)              // Invoked when there is an error fetching the assignment. Must not block.
	Backoff                  func(int) time.Duration  // Backoff for retries, after stream failures.
	LogPrefix                string                   // Prefix for log messages from this client.
}

// NewClient creates a new autosharding client that connects to the sharding
// service using the provided options and fetches assignments. It returns a
// function to close the client. The close function blocks until the client's
// goroutine has exited, and no callbacks are invoked after it returns.
//
// OnAssignmentUpdate and OnAssignmentError are invoked serially, while an
// internal lock is held. They must not block and must not call the close
// function, or they will deadlock.
func NewClient(opts ClientOptions) func() {
	ac := &autoshardingClient{
		cc:                 opts.CC,
		target:             opts.AutoshardingTarget,
		uuid:               opts.UUID,
		onAssignmentUpdate: opts.OnAssignmentUpdate,
		onAssignmentError:  opts.OnAssignmentError,
		backoff:            opts.Backoff,
		runnerDoneCh:       make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	ac.cancel = cancel
	ac.logger = igrpclog.NewPrefixLogger(logger, opts.LogPrefix+fmt.Sprintf("[autosharding-client %p] ", ac))
	if ac.backoff == nil {
		ac.backoff = backoff.DefaultExponential.Backoff
	}

	// Start a timer that will fire after the initial assignment timeout. If a
	// valid assignment is not received before the timer fires, the
	// onAssignmentError callback will be invoked.
	ac.initialAssignmentTimer = time.AfterFunc(opts.InitialAssignmentTimeout, func() { ac.reportError(errInitialAssignmentTimeout) })

	go ac.run(ctx)
	return ac.close
}
