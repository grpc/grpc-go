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

package sharding_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/autosharding/internal/sharding"
	"google.golang.org/grpc/balancer/autosharding/internal/sharding/internal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	asgrpc "google.golang.org/grpc/balancer/autosharding/internal/proto"
	aspb "google.golang.org/grpc/balancer/autosharding/internal/proto"
)

const (
	defaultTestTimeout       = 10 * time.Second
	defaultTestShortTimeout  = 10 * time.Millisecond
	initialAssignmentTimeout = 500 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// testAutoshardingServer is a test implementation of the autosharding service
// that hands incoming WatchShardingAssignment streams to the test via streamCh
// and keeps each stream open until either the client cancels it or the test
// sends an error on failStreamCh.
type testAutoshardingServer struct {
	asgrpc.UnimplementedAutoshardingServiceServer
	cc           *grpc.ClientConn
	streamCh     chan asgrpc.AutoshardingService_WatchShardingAssignmentServer
	failStreamCh chan error
}

func (s *testAutoshardingServer) WatchShardingAssignment(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
	select {
	case s.streamCh <- stream:
	case <-stream.Context().Done():
		return nil
	}
	select {
	case err := <-s.failStreamCh:
		return err
	case <-stream.Context().Done():
		return nil
	}
}

func (s *testAutoshardingServer) waitForStream(ctx context.Context, t *testing.T) asgrpc.AutoshardingService_WatchShardingAssignmentServer {
	t.Helper()
	select {
	case stream := <-s.streamCh:
		return stream
	case <-ctx.Done():
		t.Fatal("Timed out waiting for WatchShardingAssignment stream")
		return nil
	}
}

// startTestShardingServer starts a test gRPC server that implements the
// autosharding service and creates a gRPC client connection to it.
func startTestShardingServer(t *testing.T) *testAutoshardingServer {
	t.Helper()

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	srv := grpc.NewServer()
	ts := &testAutoshardingServer{
		streamCh:     make(chan asgrpc.AutoshardingService_WatchShardingAssignmentServer, 1),
		failStreamCh: make(chan error, 1),
	}
	asgrpc.RegisterAutoshardingServiceServer(srv, ts)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr().String(), err)
	}
	t.Cleanup(func() { cc.Close() })
	ts.cc = cc
	return ts
}

func makeSliceAssignment(startKey, endKey []byte, epIndices ...int32) *aspb.SliceAssignment {
	eps := make([]*aspb.PerSliceEndpointState, len(epIndices))
	for i, idx := range epIndices {
		eps[i] = &aspb.PerSliceEndpointState{EndpointIndex: idx}
	}
	return &aspb.SliceAssignment{
		Slice: &aspb.Slice{
			StartKey: startKey,
			EndKey:   endKey,
		},
		Endpoints: eps,
	}
}

func makeEndpoints(names ...string) []*aspb.EndpointState {
	eps := make([]*aspb.EndpointState, len(names))
	for i, name := range names {
		eps[i] = &aspb.EndpointState{Endpoint: name}
	}
	return eps
}

func sendChunk(t *testing.T, stream asgrpc.AutoshardingService_WatchShardingAssignmentServer, endpoints []*aspb.EndpointState, slices ...*aspb.SliceAssignment) {
	t.Helper()
	if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
		Chunk: &aspb.AssignmentChunk{
			Endpoints:        endpoints,
			SliceAssignments: slices,
		},
	}); err != nil {
		t.Fatalf("stream.Send(Chunk) failed: %v", err)
	}
}

func sendMetadata(t *testing.T, stream asgrpc.AutoshardingService_WatchShardingAssignmentServer, gen int64) {
	t.Helper()
	if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
		Metadata: &aspb.AssignmentMetadata{Generation: gen},
	}); err != nil {
		t.Fatalf("stream.Send(Metadata) failed: %v", err)
	}
}

func verifyInitialClientConfig(t *testing.T, stream asgrpc.AutoshardingService_WatchShardingAssignmentServer, want *aspb.InitialClientConfig) {
	t.Helper()
	req, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() for InitialClientConfig failed: %v", err)
	}
	if diff := cmp.Diff(want, req.GetInitialClientConfig(), protocmp.Transform()); diff != "" {
		t.Fatalf("InitialClientConfig diff (-want +got):\n%s", diff)
	}
}

func verifyAssignment(ctx context.Context, t *testing.T, ch <-chan *sharding.Assignment, want *sharding.Assignment) {
	t.Helper()
	select {
	case got := <-ch:
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Fatalf("Assignment diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for Assignment (generation %d)", want.Generation)
	}
}

func verifyACK(t *testing.T, stream asgrpc.AutoshardingService_WatchShardingAssignmentServer, want *aspb.AssignmentAck) {
	t.Helper()
	req, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() for AssignmentAck (generation %d) failed: %v", want.GetGeneration(), err)
	}
	if diff := cmp.Diff(want, req.GetAssignmentAck(), protocmp.Transform()); diff != "" {
		t.Fatalf("AssignmentAck diff (-want +got):\n%s", diff)
	}
}

func verifyNACK(t *testing.T, stream asgrpc.AutoshardingService_WatchShardingAssignmentServer, wantGen int64, wantErrSubstr string) string {
	t.Helper()
	req, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() for NACK (generation %d) failed: %v", wantGen, err)
	}
	got := req.GetAssignmentAck()
	if got.GetAccepted() || got.GetGeneration() != wantGen || got.GetErrorMessage() == "" || !strings.Contains(got.GetErrorMessage(), wantErrSubstr) {
		t.Fatalf("AssignmentAck = %v, want Accepted=false, Generation=%d, ErrorMessage containing %q", got, wantGen, wantErrSubstr)
	}
	return got.GetErrorMessage()
}

func verifyAssignmentError(ctx context.Context, t *testing.T, ch <-chan error, wantSubstr string) {
	t.Helper()
	select {
	case err := <-ch:
		if err == nil || !strings.Contains(err.Error(), wantSubstr) {
			t.Fatalf("OnAssignmentError = %v, want error containing %q", err, wantSubstr)
		}
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for OnAssignmentError containing %q", wantSubstr)
	}
}

// Tests the happy path where the client receives valid assignments (including
// one with partial validation errors) and sends ACKs.
func (s) TestClient_ValidAssignmentsAndACK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srv := startTestShardingServer(t)
	updateCh := make(chan *sharding.Assignment, 1)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       srv.cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	stream := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 0,
	})

	// Send a LoadReportingConfig message and an empty response (oneof unset),
	// both of which the client must ignore.
	if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
		Config: &aspb.LoadReportingConfig{LoadQuantumFraction: 0.5},
	}); err != nil {
		t.Fatalf("stream.Send(Config) failed: %v", err)
	}
	if err := stream.Send(&aspb.WatchShardingAssignmentResponse{}); err != nil {
		t.Fatalf("stream.Send(empty) failed: %v", err)
	}

	// Generation 1: Send two valid chunks followed by metadata, then verify
	// update and ACK.
	sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment([]byte("m"), nil, 1))
	sendChunk(t, stream, makeEndpoints("ep1"), makeSliceAssignment(nil, []byte("m"), 0))
	sendMetadata(t, stream, 1)
	verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
		EndpointNames: []string{"ep0", "ep1"},
		Generation:    1,
		Slices: []sharding.Slice{
			{StartKey: []byte{}, Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
		},
	})
	verifyACK(t, stream, &aspb.AssignmentAck{
		Generation: 1,
		Accepted:   true,
	})

	// Generation 2: Send 1 valid slice and several invalid slices, then verify
	// update (with trailing gap filled) and ACK with error message.
	invalidSlices := []*aspb.SliceAssignment{
		makeSliceAssignment(nil, []byte("m"), 0), // valid
	}
	for range 20 {
		invalidSlices = append(invalidSlices, makeSliceAssignment([]byte("z"), []byte("a"), 0))
	}
	sendChunk(t, stream, makeEndpoints("ep0"), invalidSlices...)
	sendMetadata(t, stream, 2)
	verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
		EndpointNames: []string{"ep0"},
		Generation:    2,
		Slices: []sharding.Slice{
			{StartKey: []byte{}, Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{}},
		},
	})
	verifyACK(t, stream, &aspb.AssignmentAck{
		Generation:   2,
		Accepted:     true,
		ErrorMessage: "20 slice(s) with start_key >= end_key; encountered 1 gap(s) in the assignment",
	})

	// Generation 3: Send only valid slices with a leading gap (no invalid
	// slices), then verify update (with leading gap filled) and ACK with only
	// the gap error message.
	sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment([]byte("d"), nil, 0))
	sendMetadata(t, stream, 3)
	verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
		EndpointNames: []string{"ep0"},
		Generation:    3,
		Slices: []sharding.Slice{
			{StartKey: []byte{}, Endpoints: []int{}},
			{StartKey: []byte("d"), Endpoints: []int{0}},
		},
	})
	verifyACK(t, stream, &aspb.AssignmentAck{
		Generation:   3,
		Accepted:     true,
		ErrorMessage: "encountered 1 gap(s) in the assignment",
	})

	select {
	case err := <-errCh:
		t.Fatalf("Unexpected OnAssignmentError: %v", err)
	default:
	}
}

// Tests that stale or non-positive generations are NACKed without reporting an
// error to the LB policy, while keeping the stream open for newer generations.
func (s) TestClient_StaleGenerationDropped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srv := startTestShardingServer(t)
	updateCh := make(chan *sharding.Assignment, 1)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       srv.cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	stream := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 0,
	})

	// Send generation 0 which is expected to be NACKed.
	sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment(nil, nil, 0))
	sendMetadata(t, stream, 0)
	verifyNACK(t, stream, 0, "not greater than the latest generation")

	// Send generation 2 which is expected to be accepted and ACKed.
	sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment(nil, nil, 0))
	sendMetadata(t, stream, 2)
	verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
		EndpointNames: []string{"ep0"},
		Generation:    2,
		Slices:        []sharding.Slice{{StartKey: []byte{}, Endpoints: []int{0}}},
	})
	verifyACK(t, stream, &aspb.AssignmentAck{
		Generation: 2,
		Accepted:   true,
	})

	// Send duplicate generation 2 and stale generation 1 on the same stream,
	// both of which are expected to be NACKed.
	for _, staleGen := range []int64{2, 1} {
		sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment(nil, nil, 0))
		sendMetadata(t, stream, staleGen)
		verifyNACK(t, stream, staleGen, "not greater than the latest generation 2")
	}

	// Send valid generation 3 on the same stream which is expected to be
	// accepted and ACKed.
	sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment(nil, nil, 0))
	sendMetadata(t, stream, 3)
	verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
		EndpointNames: []string{"ep0"},
		Generation:    3,
		Slices:        []sharding.Slice{{StartKey: []byte{}, Endpoints: []int{0}}},
	})
	verifyACK(t, stream, &aspb.AssignmentAck{
		Generation: 3,
		Accepted:   true,
	})

	// Stale generations must never report an error to the LB policy.
	select {
	case err := <-errCh:
		t.Fatalf("Unexpected OnAssignmentError: %v", err)
	default:
	}
}

// Tests that empty assignments and assignments with no valid slices are NACKed
// and reported via OnAssignmentError only before the first valid assignment is
// reported, and that errors after a valid assignment are suppressed.
func (s) TestClient_InvalidAssignmentsAndErrorSuppression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srv := startTestShardingServer(t)
	updateCh := make(chan *sharding.Assignment, 1)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       srv.cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	stream := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 0,
	})

	// Assignment with no chunks -> NACK + OnAssignmentError.
	sendMetadata(t, stream, 1)
	nackMsg := verifyNACK(t, stream, 1, "")
	verifyAssignmentError(ctx, t, errCh, nackMsg)

	// 2. Chunk with no valid slices (Gen 2) -> NACK + OnAssignmentError.
	sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment([]byte("z"), []byte("a"), 0))
	sendMetadata(t, stream, 2)
	nackMsg = verifyNACK(t, stream, 2, "")
	verifyAssignmentError(ctx, t, errCh, nackMsg)

	// Valid assignment -> ACK + OnAssignmentUpdate.
	sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment(nil, nil, 0))
	sendMetadata(t, stream, 3)
	verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
		EndpointNames: []string{"ep0"},
		Generation:    3,
		Slices:        []sharding.Slice{{StartKey: []byte{}, Endpoints: []int{0}}},
	})
	verifyACK(t, stream, &aspb.AssignmentAck{
		Generation: 3,
		Accepted:   true,
	})

	// Assignment with no chunks after valid assignment -> NACK, no
	// OnAssignmentError.
	sendMetadata(t, stream, 4)
	verifyNACK(t, stream, 4, "")

	// Chunk with no valid slices after valid assignment -> NACK, no
	// OnAssignmentError.
	sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment([]byte("z"), []byte("a"), 0))
	sendMetadata(t, stream, 5)
	verifyNACK(t, stream, 5, "")

	select {
	case err := <-errCh:
		t.Fatalf("Unexpected OnAssignmentError after valid assignment was reported: %v", err)
	default:
	}
}

// Tests stream failure recovery, clearing partial chunks from broken streams,
// updating LatestGeneration on reconnect, and resetting backoff after a stream
// receives a valid assignment.
func (s) TestClient_StreamFailureAndBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	backoffCh := make(chan int, 1)
	origBackoff := internal.DefaultBackoff
	defer func() { internal.DefaultBackoff = origBackoff }()
	internal.DefaultBackoff = func(attempt int) time.Duration {
		backoffCh <- attempt
		return defaultTestShortTimeout
	}

	srv := startTestShardingServer(t)
	updateCh := make(chan *sharding.Assignment, 1)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       srv.cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	// Attempt 1: InitialClientConfig has LatestGeneration=0, fails mid-chunk,
	// reports error, and calls Backoff(0).
	stream1 := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream1, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 0,
	})
	sendChunk(t, stream1, makeEndpoints("stale-ep"), makeSliceAssignment(nil, nil, 0))
	srv.failStreamCh <- status.Error(codes.Unavailable, "stream 1 broken mid-assignment")
	verifyAssignmentError(ctx, t, errCh, "stream 1 broken mid-assignment")
	select {
	case gotAttempt := <-backoffCh:
		if gotAttempt != 0 {
			t.Fatalf("Attempt 1 Backoff called with %d, want 0", gotAttempt)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Backoff after Attempt 1")
	}

	// Attempt 2: InitialClientConfig still has LatestGeneration=0; receives
	// Generation 5 with only "good-ep" (partial chunk from Attempt 1 was
	// discarded), then the stream fails.
	stream2 := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream2, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 0,
	})
	sendChunk(t, stream2, makeEndpoints("good-ep"), makeSliceAssignment(nil, nil, 0))
	sendMetadata(t, stream2, 5)
	verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
		EndpointNames: []string{"good-ep"},
		Generation:    5,
		Slices: []sharding.Slice{
			{StartKey: []byte{}, Endpoints: []int{0}},
		},
	})
	verifyACK(t, stream2, &aspb.AssignmentAck{
		Generation: 5,
		Accepted:   true,
	})
	srv.failStreamCh <- status.Error(codes.Unavailable, "stream 2 closed after valid assignment")

	// Attempt 3: Reconnects with LatestGeneration=5, and when it fails,
	// Backoff is called with attempt=0 (proving Attempt 2 reset backoff and did
	// not invoke Backoff between Attempt 2 and Attempt 3).
	stream3 := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream3, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 5,
	})
	srv.failStreamCh <- status.Error(codes.Unavailable, "stream 3 failed immediately")
	select {
	case gotAttempt := <-backoffCh:
		if gotAttempt != 0 {
			t.Fatalf("Attempt 3 Backoff called with %d, want 0 (reset after Attempt 2)", gotAttempt)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Backoff after Attempt 3")
	}

	// No additional errors should have been reported after Generation 5.
	select {
	case err := <-errCh:
		t.Fatalf("Unexpected OnAssignmentError after valid assignment: %v", err)
	default:
	}
}

// Tests that a NACKed generation does not advance the latest generation, i.e.
// the InitialClientConfig sent when reconnecting carries the latest accepted
// generation.
func (s) TestClient_ReconnectUsesLatestAcceptedGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Errors are not reported once a valid assignment is reported, so
	// OnAssignmentError is not set.
	srv := startTestShardingServer(t)
	updateCh := make(chan *sharding.Assignment, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       srv.cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
	})
	defer closeClient()

	// Stream 1: Generation 1 is accepted and reported, Generation 2 has no
	// valid slices and is NACKed, and then the stream fails.
	stream1 := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream1, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 0,
	})

	sendChunk(t, stream1, makeEndpoints("ep0"), makeSliceAssignment(nil, nil, 0))
	sendMetadata(t, stream1, 1)
	verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
		EndpointNames: []string{"ep0"},
		Generation:    1,
		Slices:        []sharding.Slice{{StartKey: []byte{}, Endpoints: []int{0}}},
	})
	verifyACK(t, stream1, &aspb.AssignmentAck{
		Generation: 1,
		Accepted:   true,
	})

	sendChunk(t, stream1, makeEndpoints("ep0"), makeSliceAssignment([]byte("z"), []byte("a"), 0))
	sendMetadata(t, stream1, 2)
	verifyNACK(t, stream1, 2, "")
	srv.failStreamCh <- status.Error(codes.Unavailable, "stream 1 failed after a NACK")

	// Stream 2: InitialClientConfig must carry the latest accepted generation,
	// and not the NACKed one.
	stream2 := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream2, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 1,
	})
}

// Tests that InitialAssignmentTimeout fires when the server does not send a
// complete assignment in time, and does not fire if a valid assignment arrives
// before the timeout.
func (s) TestClient_InitialAssignmentTimeout(t *testing.T) {
	t.Run("timeout_fires_before_valid_assignment", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		srv := startTestShardingServer(t)
		updateCh := make(chan *sharding.Assignment, 1)
		errCh := make(chan error, 1)
		closeClient := sharding.NewClient(sharding.ClientOptions{
			CC:                       srv.cc,
			AutoshardingTarget:       "test-target",
			UUID:                     "test-uuid",
			InitialAssignmentTimeout: defaultTestShortTimeout,
			OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
			OnAssignmentError:        func(err error) { errCh <- err },
		})
		defer closeClient()

		stream := srv.waitForStream(ctx, t)
		verifyInitialClientConfig(t, stream, &aspb.InitialClientConfig{
			Target:           "test-target",
			ClientUuid:       "test-uuid",
			LatestGeneration: 0,
		})

		// Wait until the initial assignment timer fires on the client.
		verifyAssignmentError(ctx, t, errCh, "initial_assignment_timeout fired")

		// Now send a valid assignment and verify the client still accepts it.
		sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment(nil, nil, 0))
		sendMetadata(t, stream, 1)
		verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
			EndpointNames: []string{"ep0"},
			Generation:    1,
			Slices:        []sharding.Slice{{StartKey: []byte{}, Endpoints: []int{0}}},
		})
		verifyACK(t, stream, &aspb.AssignmentAck{
			Generation: 1,
			Accepted:   true,
		})
	})

	t.Run("valid_assignment_cancels_initial_assignment_timer", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		srv := startTestShardingServer(t)
		updateCh := make(chan *sharding.Assignment, 1)
		errCh := make(chan error, 1)
		closeClient := sharding.NewClient(sharding.ClientOptions{
			CC:                       srv.cc,
			AutoshardingTarget:       "test-target",
			UUID:                     "test-uuid",
			InitialAssignmentTimeout: initialAssignmentTimeout,
			OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
			OnAssignmentError:        func(err error) { errCh <- err },
		})
		defer closeClient()

		stream := srv.waitForStream(ctx, t)
		verifyInitialClientConfig(t, stream, &aspb.InitialClientConfig{
			Target:           "test-target",
			ClientUuid:       "test-uuid",
			LatestGeneration: 0,
		})
		sendChunk(t, stream, makeEndpoints("ep0"), makeSliceAssignment(nil, nil, 0))
		sendMetadata(t, stream, 1)
		verifyAssignment(ctx, t, updateCh, &sharding.Assignment{
			EndpointNames: []string{"ep0"},
			Generation:    1,
			Slices:        []sharding.Slice{{StartKey: []byte{}, Endpoints: []int{0}}},
		})
		verifyACK(t, stream, &aspb.AssignmentAck{
			Generation: 1,
			Accepted:   true,
		})

		// Wait for the initial assignment timer to fire, and verify that no error
		// is reported.
		select {
		case err := <-errCh:
			t.Fatalf("Unexpected OnAssignmentError after a valid assignment: %v", err)
		case <-time.After(initialAssignmentTimeout + defaultTestShortTimeout):
		}
	})
}

// Tests that closing the client cancels the stream to the sharding service and
// is idempotent, and that no error is reported, either for the stream
// cancellation caused by the close, or by the initial assignment timer after
// the close.
func (s) TestClient_Close(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// The server never sends an assignment, so OnAssignmentUpdate is not set.
	srv := startTestShardingServer(t)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       srv.cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: initialAssignmentTimeout,
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	stream := srv.waitForStream(ctx, t)
	verifyInitialClientConfig(t, stream, &aspb.InitialClientConfig{
		Target:           "test-target",
		ClientUuid:       "test-uuid",
		LatestGeneration: 0,
	})

	closeClient()

	// Verify that the server sees the stream being cancelled.
	select {
	case <-stream.Context().Done():
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the stream to be cancelled after close")
	}

	// close() waits for the client's goroutine to exit. So, if an error was
	// reported for the stream cancellation caused by the close, it would
	// already be on errCh.
	select {
	case err := <-errCh:
		t.Fatalf("Unexpected OnAssignmentError during close: %v", err)
	default:
	}

	// Verify that a second call to close does not block.
	closeDoneCh := make(chan struct{})
	go func() {
		closeClient()
		close(closeDoneCh)
	}()
	select {
	case <-closeDoneCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the second call to close to return")
	}

	// Wait past the initial assignment timeout, and verify that no error is
	// reported after close.
	select {
	case err := <-errCh:
		t.Fatalf("Unexpected OnAssignmentError after close: %v", err)
	case <-time.After(initialAssignmentTimeout + defaultTestShortTimeout):
	}
}
