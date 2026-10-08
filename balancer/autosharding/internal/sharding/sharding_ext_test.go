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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/autosharding/internal/sharding"
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
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// testAutoshardingServer is a test implementation of the autosharding service
// that allows the test to specify a custom WatchShardingAssignment function to
// control the behavior of the server during tests.
type testAutoshardingServer struct {
	asgrpc.UnimplementedAutoshardingServiceServer
	watchFunc func(asgrpc.AutoshardingService_WatchShardingAssignmentServer) error
}

func (s *testAutoshardingServer) WatchShardingAssignment(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
	if s.watchFunc != nil {
		return s.watchFunc(stream)
	}
	return nil
}

// startTestShardingServer starts a test gRPC server that implements the
// autosharding service that delegates to the provided watchFunc for the
// WatchShardingAssignment RPC. It returns a gRPC client connection to the
// server.
func startTestShardingServer(t *testing.T, watchFunc func(asgrpc.AutoshardingService_WatchShardingAssignmentServer) error) *grpc.ClientConn {
	t.Helper()

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	srv := grpc.NewServer()
	asgrpc.RegisterAutoshardingServiceServer(srv, &testAutoshardingServer{watchFunc: watchFunc})
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr().String(), err)
	}
	t.Cleanup(func() { cc.Close() })
	return cc
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

// Tests the happy path where the client receives valid assignments (including
// one with partial validation errors) and sends ACKs.
func (s) TestClient_ValidAssignmentsAndACK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	initCfgCh := make(chan *aspb.InitialClientConfig, 1)
	ackCh := make(chan *aspb.AssignmentAck, 1)
	cc := startTestShardingServer(t, func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		initCfgCh <- req.GetInitialClientConfig()

		// Send a LoadReportingConfig message, which the client must ignore.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Config: &aspb.LoadReportingConfig{LoadQuantumFraction: 0.5},
		}); err != nil {
			return err
		}

		// Generation 1: Send two valid chunks followed by metadata.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep0"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment([]byte("m"), nil, 1),
				},
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep1"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment(nil, []byte("m"), 0),
				},
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 1},
		}); err != nil {
			return err
		}

		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// Generation 2: Send 1 valid slice and several invalid slices.
		invalidSlices := []*aspb.SliceAssignment{
			makeSliceAssignment(nil, []byte("m"), 0), // valid
		}
		for range 20 {
			invalidSlices = append(invalidSlices, makeSliceAssignment([]byte("z"), []byte("a"), 0))
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints:        makeEndpoints("ep0"),
				SliceAssignments: invalidSlices,
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 2},
		}); err != nil {
			return err
		}

		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// Generation 3: Send only valid slices with a leading gap (no invalid
		// slices).
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep0"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment([]byte("d"), nil, 0),
				},
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 3},
		}); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		<-stream.Context().Done()
		return nil
	})

	updateCh := make(chan *sharding.Assignment, 1)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	// Verify InitialClientConfig.
	select {
	case gotCfg := <-initCfgCh:
		wantCfg := &aspb.InitialClientConfig{
			Target:           "test-target",
			ClientUuid:       "test-uuid",
			LatestGeneration: 0,
		}
		if diff := cmp.Diff(wantCfg, gotCfg, protocmp.Transform()); diff != "" {
			t.Fatalf("InitialClientConfig diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for InitialClientConfig")
	}

	// Verify Generation 1 update and ACK.
	select {
	case gotAssignment := <-updateCh:
		wantAssignment := &sharding.Assignment{
			EndpointNames: []string{"ep0", "ep1"},
			Generation:    1,
			Slices: []sharding.Slice{
				{StartKey: []byte{}, Endpoints: []int{0}},
				{StartKey: []byte("m"), Endpoints: []int{1}},
			},
		}
		if diff := cmp.Diff(wantAssignment, gotAssignment, cmpopts.EquateEmpty()); diff != "" {
			t.Fatalf("Generation 1 Assignment diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 1 Assignment")
	}

	select {
	case gotACK := <-ackCh:
		wantACK := &aspb.AssignmentAck{
			Generation:   1,
			Accepted:     true,
			ErrorMessage: "",
		}
		if diff := cmp.Diff(wantACK, gotACK, protocmp.Transform()); diff != "" {
			t.Fatalf("Generation 1 AssignmentAck diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 1 AssignmentAck")
	}

	// Verify Generation 2 update (with trailing gap filled) and ACK with error
	// message.
	select {
	case gotAssignment := <-updateCh:
		wantAssignment := &sharding.Assignment{
			EndpointNames: []string{"ep0"},
			Generation:    2,
			Slices: []sharding.Slice{
				{StartKey: []byte{}, Endpoints: []int{0}},
				{StartKey: []byte("m"), Endpoints: []int{}},
			},
		}
		if diff := cmp.Diff(wantAssignment, gotAssignment, cmpopts.EquateEmpty()); diff != "" {
			t.Fatalf("Generation 2 Assignment diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 2 Assignment")
	}

	select {
	case gotACK := <-ackCh:
		if !gotACK.GetAccepted() || gotACK.GetGeneration() != 2 {
			t.Fatalf("Generation 2 AssignmentAck = %v, want Accepted=true, Generation=2", gotACK)
		}
		const wantErrMsg = "20 slice(s) with start_key >= end_key; encountered 1 gap(s) in the assignment"
		if gotErrMsg := gotACK.GetErrorMessage(); gotErrMsg != wantErrMsg {
			t.Fatalf("Generation 2 AssignmentAck ErrorMessage = %q, want %q", gotErrMsg, wantErrMsg)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 2 AssignmentAck")
	}

	// Verify Generation 3 update (with leading gap filled) and ACK with only
	// the gap error message.
	select {
	case gotAssignment := <-updateCh:
		wantAssignment := &sharding.Assignment{
			EndpointNames: []string{"ep0"},
			Generation:    3,
			Slices: []sharding.Slice{
				{StartKey: []byte{}, Endpoints: []int{}},
				{StartKey: []byte("d"), Endpoints: []int{0}},
			},
		}
		if diff := cmp.Diff(wantAssignment, gotAssignment, cmpopts.EquateEmpty()); diff != "" {
			t.Fatalf("Generation 3 Assignment diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 3 Assignment")
	}

	select {
	case gotACK := <-ackCh:
		wantACK := &aspb.AssignmentAck{
			Generation:   3,
			Accepted:     true,
			ErrorMessage: "encountered 1 gap(s) in the assignment",
		}
		if diff := cmp.Diff(wantACK, gotACK, protocmp.Transform()); diff != "" {
			t.Fatalf("Generation 3 AssignmentAck diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 3 AssignmentAck")
	}

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

	// A helper function to send a valid assignment chunk followed by metadata
	// with the given generation.
	sendValidChunkAndGen := func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer, gen int64) error {
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep0"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment(nil, nil, 0),
				},
			},
		}); err != nil {
			return err
		}
		return stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: gen},
		})
	}

	ackCh := make(chan *aspb.AssignmentAck, 1)
	cc := startTestShardingServer(t, func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}

		// 1. Send generation 0 (not > initial latestGeneration 0).
		if err := sendValidChunkAndGen(stream, 0); err != nil {
			return err
		}
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// 2. Send valid generation 2.
		if err := sendValidChunkAndGen(stream, 2); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// 3. Send duplicate generation 2 and stale generation 1 on the same stream.
		for _, staleGen := range []int64{2, 1} {
			if err := sendValidChunkAndGen(stream, staleGen); err != nil {
				return err
			}
			req, err = stream.Recv()
			if err != nil {
				return err
			}
			ackCh <- req.GetAssignmentAck()
		}

		// 4. Send valid generation 3 on the same stream.
		if err := sendValidChunkAndGen(stream, 3); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		<-stream.Context().Done()
		return nil
	})

	updateCh := make(chan *sharding.Assignment, 1)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	// 1. Generation 0 should be NACKed.
	select {
	case gotACK := <-ackCh:
		if gotACK.GetAccepted() || gotACK.GetGeneration() != 0 || !strings.Contains(gotACK.GetErrorMessage(), "not greater than the latest generation") {
			t.Fatalf("Generation 0 ACK = %v, want Accepted=false with stale generation error", gotACK)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 0 NACK")
	}

	// 2. Generation 2 should be accepted and reported.
	select {
	case gotAssignment := <-updateCh:
		if gotAssignment.Generation != 2 {
			t.Fatalf("Got Assignment generation %d, want 2", gotAssignment.Generation)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 2 Assignment")
	}
	select {
	case gotACK := <-ackCh:
		if !gotACK.GetAccepted() || gotACK.GetGeneration() != 2 {
			t.Fatalf("Generation 2 ACK = %v, want Accepted=true", gotACK)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 2 ACK")
	}

	// 3. Duplicate generation 2 and stale generation 1 should both be NACKed.
	for _, wantGen := range []int64{2, 1} {
		select {
		case gotACK := <-ackCh:
			if gotACK.GetAccepted() || gotACK.GetGeneration() != wantGen || !strings.Contains(gotACK.GetErrorMessage(), "not greater than the latest generation 2") {
				t.Fatalf("Stale generation %d ACK = %v, want Accepted=false", wantGen, gotACK)
			}
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for stale generation %d NACK", wantGen)
		}
	}

	// 4. Generation 3 should be accepted and reported.
	select {
	case gotAssignment := <-updateCh:
		if gotAssignment.Generation != 3 {
			t.Fatalf("Got Assignment generation %d, want 3", gotAssignment.Generation)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 3 Assignment")
	}
	select {
	case gotACK := <-ackCh:
		if !gotACK.GetAccepted() || gotACK.GetGeneration() != 3 {
			t.Fatalf("Generation 3 ACK = %v, want Accepted=true", gotACK)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 3 ACK")
	}

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

	ackCh := make(chan *aspb.AssignmentAck, 1)
	cc := startTestShardingServer(t, func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}

		// 1. Metadata with no chunks (Gen 1) -> NACK + OnAssignmentError.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 1},
		}); err != nil {
			return err
		}
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// 2. Chunk with no valid slices (Gen 2) -> NACK + OnAssignmentError.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep0"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment([]byte("z"), []byte("a"), 0), // start_key >= end_key
				},
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 2},
		}); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// 3. Valid assignment (Gen 3) -> ACK + OnAssignmentUpdate.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep0"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment(nil, nil, 0),
				},
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 3},
		}); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// 4. Metadata with no chunks after valid assignment (Gen 4) -> NACK, no
		// OnAssignmentError.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 4},
		}); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// 5. Chunk with no valid slices after valid assignment (Gen 5) -> NACK,
		// no OnAssignmentError.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep0"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment([]byte("z"), []byte("a"), 0),
				},
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 5},
		}); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		<-stream.Context().Done()
		return nil
	})

	updateCh := make(chan *sharding.Assignment, 1)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	// Verify Gen 1 and Gen 2 both NACK and trigger OnAssignmentError with the
	// same message as the NACK.
	for _, wantGen := range []int64{1, 2} {
		var nackMsg string
		select {
		case gotACK := <-ackCh:
			if gotACK.GetAccepted() || gotACK.GetGeneration() != wantGen || gotACK.GetErrorMessage() == "" {
				t.Fatalf("Gen %d ACK = %v, want Accepted=false with non-empty ErrorMessage", wantGen, gotACK)
			}
			nackMsg = gotACK.GetErrorMessage()
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for Gen %d NACK", wantGen)
		}
		select {
		case err := <-errCh:
			if !strings.Contains(err.Error(), nackMsg) {
				t.Fatalf("OnAssignmentError for Gen %d = %q, want it to contain the NACK message %q", wantGen, err, nackMsg)
			}
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for OnAssignmentError for Gen %d", wantGen)
		}
	}

	// Verify Gen 3 is accepted and reported.
	select {
	case gotAssignment := <-updateCh:
		if gotAssignment.Generation != 3 {
			t.Fatalf("Got Assignment generation %d, want 3", gotAssignment.Generation)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Gen 3 Assignment")
	}
	select {
	case gotACK := <-ackCh:
		if !gotACK.GetAccepted() || gotACK.GetGeneration() != 3 || gotACK.GetErrorMessage() != "" {
			t.Fatalf("Gen 3 ACK = %v, want Accepted=true with empty ErrorMessage", gotACK)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Gen 3 ACK")
	}

	// Verify Gen 4 and Gen 5 are NACKed, but OnAssignmentError is not called.
	for _, wantGen := range []int64{4, 5} {
		select {
		case gotACK := <-ackCh:
			if gotACK.GetAccepted() || gotACK.GetGeneration() != wantGen || gotACK.GetErrorMessage() == "" {
				t.Fatalf("Gen %d ACK = %v, want Accepted=false with non-empty ErrorMessage", wantGen, gotACK)
			}
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for Gen %d NACK", wantGen)
		}
	}

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

	var streamAttempt atomic.Int32
	initCfgCh := make(chan *aspb.InitialClientConfig, 1)
	cc := startTestShardingServer(t, func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
		attempt := streamAttempt.Add(1)
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		initCfgCh <- req.GetInitialClientConfig()

		switch attempt {
		case 1:
			// Send a partial chunk with "stale-ep", then fail the stream before
			// sending AssignmentMetadata.
			if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
				Chunk: &aspb.AssignmentChunk{
					Endpoints: makeEndpoints("stale-ep"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment(nil, nil, 0),
					},
				},
			}); err != nil {
				return err
			}
			return status.Error(codes.Unavailable, "stream 1 broken mid-assignment")

		case 2:
			// Send a complete valid assignment (Generation 5), wait for ACK,
			// then fail the stream.
			if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
				Chunk: &aspb.AssignmentChunk{
					Endpoints: makeEndpoints("good-ep"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment(nil, nil, 0),
					},
				},
			}); err != nil {
				return err
			}
			if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
				Metadata: &aspb.AssignmentMetadata{Generation: 5},
			}); err != nil {
				return err
			}
			if _, err := stream.Recv(); err != nil {
				return err
			}
			return status.Error(codes.Unavailable, "stream 2 closed after valid assignment")

		case 3:
			// Fail immediately so we can verify that backoff was reset to
			// attempt 0 after stream 2 succeeded.
			return status.Error(codes.Unavailable, "stream 3 failed immediately")

		default:
			<-stream.Context().Done()
			return nil
		}
	})

	backoffCh := make(chan int, 1)
	updateCh := make(chan *sharding.Assignment, 1)
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
		OnAssignmentError:        func(err error) { errCh <- err },
		Backoff: func(attempt int) time.Duration {
			backoffCh <- attempt
			return defaultTestShortTimeout
		},
	})
	defer closeClient()

	// Attempt 1: InitialClientConfig has LatestGeneration=0, fails mid-chunk,
	// reports error, and calls Backoff(0).
	select {
	case gotCfg := <-initCfgCh:
		if gotCfg.GetLatestGeneration() != 0 || gotCfg.GetClientUuid() != "test-uuid" {
			t.Fatalf("Attempt 1 InitialClientConfig = %v, want LatestGeneration=0, ClientUuid=test-uuid", gotCfg)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Attempt 1 InitialClientConfig")
	}
	select {
	case err := <-errCh:
		if !strings.Contains(err.Error(), "stream 1 broken mid-assignment") {
			t.Fatalf("OnAssignmentError from Attempt 1 = %v, want it to contain %q", err, "stream 1 broken mid-assignment")
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for OnAssignmentError from Attempt 1")
	}
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
	// discarded).
	select {
	case gotCfg := <-initCfgCh:
		if gotCfg.GetLatestGeneration() != 0 || gotCfg.GetClientUuid() != "test-uuid" {
			t.Fatalf("Attempt 2 InitialClientConfig = %v, want LatestGeneration=0, ClientUuid=test-uuid", gotCfg)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Attempt 2 InitialClientConfig")
	}
	select {
	case gotAssignment := <-updateCh:
		wantAssignment := &sharding.Assignment{
			EndpointNames: []string{"good-ep"},
			Generation:    5,
			Slices: []sharding.Slice{
				{StartKey: []byte{}, Endpoints: []int{0}},
			},
		}
		if diff := cmp.Diff(wantAssignment, gotAssignment, cmpopts.EquateEmpty()); diff != "" {
			t.Fatalf("Attempt 2 Assignment diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Attempt 2 Assignment")
	}

	// Attempt 3: Reconnects with LatestGeneration=5, and when it fails,
	// Backoff is called with attempt=0 (proving Attempt 2 reset backoff and did
	// not invoke Backoff between Attempt 2 and Attempt 3).
	select {
	case gotCfg := <-initCfgCh:
		if gotCfg.GetLatestGeneration() != 5 || gotCfg.GetClientUuid() != "test-uuid" {
			t.Fatalf("Attempt 3 InitialClientConfig = %v, want LatestGeneration=5, ClientUuid=test-uuid", gotCfg)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Attempt 3 InitialClientConfig")
	}
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

	var streamAttempt atomic.Int32
	initCfgCh := make(chan *aspb.InitialClientConfig, 1)
	ackCh := make(chan *aspb.AssignmentAck, 1)
	cc := startTestShardingServer(t, func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
		attempt := streamAttempt.Add(1)
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		initCfgCh <- req.GetInitialClientConfig()
		if attempt > 1 {
			<-stream.Context().Done()
			return nil
		}

		// Generation 1 is valid, and is accepted.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep0"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment(nil, nil, 0),
				},
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 1},
		}); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		// Generation 2 has no valid slices, and is NACKed.
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Chunk: &aspb.AssignmentChunk{
				Endpoints: makeEndpoints("ep0"),
				SliceAssignments: []*aspb.SliceAssignment{
					makeSliceAssignment([]byte("z"), []byte("a"), 0), // start_key >= end_key
				},
			},
		}); err != nil {
			return err
		}
		if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
			Metadata: &aspb.AssignmentMetadata{Generation: 2},
		}); err != nil {
			return err
		}
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		ackCh <- req.GetAssignmentAck()

		return status.Error(codes.Unavailable, "stream 1 failed after a NACK")
	})

	// Errors are not reported once a valid assignment is reported, so
	// OnAssignmentError is not set.
	updateCh := make(chan *sharding.Assignment, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: defaultTestTimeout,
		OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
	})
	defer closeClient()

	// Stream 1: Generation 1 is accepted and reported, and Generation 2 is
	// NACKed.
	select {
	case gotCfg := <-initCfgCh:
		if gotCfg.GetLatestGeneration() != 0 {
			t.Fatalf("Stream 1 InitialClientConfig = %v, want LatestGeneration=0", gotCfg)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Stream 1 InitialClientConfig")
	}
	select {
	case gotAssignment := <-updateCh:
		if gotAssignment.Generation != 1 {
			t.Fatalf("Got Assignment generation %d, want 1", gotAssignment.Generation)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 1 Assignment")
	}
	select {
	case gotACK := <-ackCh:
		if !gotACK.GetAccepted() || gotACK.GetGeneration() != 1 {
			t.Fatalf("Generation 1 ACK = %v, want Accepted=true", gotACK)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 1 ACK")
	}
	select {
	case gotACK := <-ackCh:
		if gotACK.GetAccepted() || gotACK.GetGeneration() != 2 {
			t.Fatalf("Generation 2 ACK = %v, want Accepted=false", gotACK)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Generation 2 NACK")
	}

	// Stream 2: InitialClientConfig must carry the latest accepted generation,
	// and not the NACKed one.
	select {
	case gotCfg := <-initCfgCh:
		if gotCfg.GetLatestGeneration() != 1 {
			t.Fatalf("Stream 2 InitialClientConfig = %v, want LatestGeneration=1", gotCfg)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for Stream 2 InitialClientConfig")
	}
}

// Tests that InitialAssignmentTimeout fires when the server does not send a
// complete assignment in time, and does not fire if a valid assignment arrives
// before the timeout.
func (s) TestClient_InitialAssignmentTimeout(t *testing.T) {
	t.Run("timeout_fires_before_valid_assignment", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		unblockServer := make(chan struct{})
		cc := startTestShardingServer(t, func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			// Wait until the initial assignment timer fires on the client.
			select {
			case <-unblockServer:
			case <-stream.Context().Done():
				return nil
			}
			// Now send a valid assignment and verify the client still accepts it.
			if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
				Chunk: &aspb.AssignmentChunk{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment(nil, nil, 0),
					},
				},
			}); err != nil {
				return err
			}
			if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
				Metadata: &aspb.AssignmentMetadata{Generation: 1},
			}); err != nil {
				return err
			}
			<-stream.Context().Done()
			return nil
		})

		updateCh := make(chan *sharding.Assignment, 1)
		errCh := make(chan error, 1)
		closeClient := sharding.NewClient(sharding.ClientOptions{
			CC:                       cc,
			AutoshardingTarget:       "test-target",
			UUID:                     "test-uuid",
			InitialAssignmentTimeout: defaultTestShortTimeout,
			OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
			OnAssignmentError:        func(err error) { errCh <- err },
		})
		defer closeClient()

		select {
		case err := <-errCh:
			if !strings.Contains(err.Error(), "initial_assignment_timeout fired") {
				t.Fatalf("Got error %v, want initial_assignment_timeout error", err)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for initial_assignment_timeout error")
		}

		close(unblockServer)

		select {
		case gotAssignment := <-updateCh:
			if gotAssignment.Generation != 1 {
				t.Fatalf("Got Assignment generation %d, want 1", gotAssignment.Generation)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for Assignment after timeout")
		}
	})

	t.Run("valid_assignment_cancels_initial_assignment_timer", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		cc := startTestShardingServer(t, func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
				Chunk: &aspb.AssignmentChunk{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment(nil, nil, 0),
					},
				},
			}); err != nil {
				return err
			}
			if err := stream.Send(&aspb.WatchShardingAssignmentResponse{
				Metadata: &aspb.AssignmentMetadata{Generation: 1},
			}); err != nil {
				return err
			}
			<-stream.Context().Done()
			return nil
		})

		const initialAssignmentTimeout = 500 * time.Millisecond
		updateCh := make(chan *sharding.Assignment, 1)
		errCh := make(chan error, 1)
		closeClient := sharding.NewClient(sharding.ClientOptions{
			CC:                       cc,
			AutoshardingTarget:       "test-target",
			UUID:                     "test-uuid",
			InitialAssignmentTimeout: initialAssignmentTimeout,
			OnAssignmentUpdate:       func(a *sharding.Assignment) { updateCh <- a },
			OnAssignmentError:        func(err error) { errCh <- err },
		})
		defer closeClient()

		select {
		case <-updateCh:
		case <-ctx.Done():
			t.Fatal("Timed out waiting for valid assignment")
		}

		// Wait for the initial assignment timer to fire, and verify that no error
		// is reported.
		<-time.After(initialAssignmentTimeout)
		select {
		case err := <-errCh:
			t.Fatalf("Unexpected OnAssignmentError after a valid assignment: %v", err)
		case <-time.After(defaultTestShortTimeout):
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

	streamStartedCh := make(chan struct{}, 1)
	streamDoneCh := make(chan struct{}, 1)
	cc := startTestShardingServer(t, func(stream asgrpc.AutoshardingService_WatchShardingAssignmentServer) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}
		streamStartedCh <- struct{}{}

		// Never send an assignment, and wait for the client to cancel the
		// stream.
		<-stream.Context().Done()
		streamDoneCh <- struct{}{}
		return nil
	})

	// The server never sends an assignment, so OnAssignmentUpdate is not set.
	const initialAssignmentTimeout = 500 * time.Millisecond
	errCh := make(chan error, 1)
	closeClient := sharding.NewClient(sharding.ClientOptions{
		CC:                       cc,
		AutoshardingTarget:       "test-target",
		UUID:                     "test-uuid",
		InitialAssignmentTimeout: initialAssignmentTimeout,
		OnAssignmentError:        func(err error) { errCh <- err },
	})
	defer closeClient()

	select {
	case <-streamStartedCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the stream to the sharding service to start")
	}

	closeClient()

	// Verify that the server sees the stream being cancelled.
	select {
	case <-streamDoneCh:
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
	<-time.After(initialAssignmentTimeout)
	select {
	case err := <-errCh:
		t.Fatalf("Unexpected OnAssignmentError after close: %v", err)
	case <-time.After(defaultTestShortTimeout):
	}
}
