/*
 *
 * Copyright 2022 gRPC authors.
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

package observability

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"

	gcplogging "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	binlogpb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/grpc_testing"
)

func cmpLoggingEntryList(got []*grpcLogEntry, want []*grpcLogEntry) error {
	if diff := cmp.Diff(got, want,
		// For nondeterministic metadata iteration.
		cmp.Comparer(func(a map[string]string, b map[string]string) bool {
			if len(a) > len(b) {
				a, b = b, a
			}
			if len(a) == 0 && len(a) != len(b) { // No metadata for one and the other comparator wants metadata.
				return false
			}
			for k, v := range a {
				if b[k] != v {
					return false
				}
			}
			return true
		}),
		cmpopts.IgnoreFields(grpcLogEntry{}, "CallID", "Peer"),
		cmpopts.IgnoreFields(address{}, "IPPort", "Type"),
		cmpopts.IgnoreFields(payload{}, "Timeout")); diff != "" {
		return fmt.Errorf("got unexpected grpcLogEntry list, diff (-got, +want): %v", diff)
	}
	return nil
}

type fakeLoggingExporter struct {
	t *testing.T

	mu      sync.Mutex
	entries []*grpcLogEntry
}

func (fle *fakeLoggingExporter) EmitGcpLoggingEntry(entry gcplogging.Entry) {
	fle.mu.Lock()
	defer fle.mu.Unlock()
	if entry.Severity != 100 {
		fle.t.Errorf("entry.Severity is not 100, this should be hardcoded")
	}
	grpcLogEntry, ok := entry.Payload.(*grpcLogEntry)
	if !ok {
		fle.t.Errorf("payload passed in isn't grpcLogEntry")
	}
	fle.entries = append(fle.entries, grpcLogEntry)
}

func (fle *fakeLoggingExporter) Close() error {
	return nil
}

// setupObservabilitySystemWithConfig sets up the observability system with the
// specified config, and returns a function which cleans up the observability
// system.
func setupObservabilitySystemWithConfig(cfg *config) (func(), error) {
	validConfigJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config to JSON: %v", err)
	}
	oldObservabilityConfig := envconfig.ObservabilityConfig
	envconfig.ObservabilityConfig = string(validConfigJSON)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	err = Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("error in Start: %v", err)
	}
	return func() {
		End()
		envconfig.ObservabilityConfig = oldObservabilityConfig
	}, nil
}

// TestClientRPCEventsLogAll tests the observability system configured with a
// client RPC event that logs every call. It performs a Unary and Bidirectional
// Streaming RPC, and expects certain grpcLogEntries to make it's way to the
// exporter.
func (s) TestClientRPCEventsLogAll(t *testing.T) {
	fle := &fakeLoggingExporter{
		t: t,
	}
	defer func(ne func(ctx context.Context, config *config) (loggingExporter, error)) {
		newLoggingExporter = ne
	}(newLoggingExporter)

	newLoggingExporter = func(ctx context.Context, config *config) (loggingExporter, error) {
		return fle, nil
	}

	clientRPCEventLogAllConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{"*"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}
	cleanup, err := setupObservabilitySystemWithConfig(clientRPCEventLogAllConfig)
	if err != nil {
		t.Fatalf("error setting up observability: %v", err)
	}
	defer cleanup()

	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
			return &grpc_testing.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream grpc_testing.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			if err := stream.Send(&grpc_testing.StreamingOutputCallResponse{}); err != nil {
				return err
			}
			if _, err := stream.Recv(); err != io.EOF {
				return err
			}
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.UnaryCall(ctx, &grpc_testing.SimpleRequest{}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}

	grpcLogEntriesWant := []*grpcLogEntry{
		{
			Type:        eventTypeClientHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  1,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeClientMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  2,
			Authority:   ss.Address,
			Payload: payload{
				Message: []uint8{},
			},
		},
		{
			Type:        eventTypeServerHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  3,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeServerMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  4,
		},
		{
			Type:        eventTypeServerTrailer,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  5,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
	}
	fle.mu.Lock()
	if err := cmpLoggingEntryList(fle.entries, grpcLogEntriesWant); err != nil {
		fle.mu.Unlock()
		t.Fatalf("error in logging entry list comparison %v", err)
	}

	fle.entries = nil
	fle.mu.Unlock()

	// Make a streaming RPC. This should cause Log calls on the MethodLogger.
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}
	if err := stream.Send(&grpc_testing.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend()() failed: %v", err)
	}
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}
	grpcLogEntriesWant = []*grpcLogEntry{
		{
			Type:        eventTypeClientHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			Authority:   ss.Address,
			SequenceID:  1,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeClientMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  2,
			Authority:   ss.Address,
			Payload: payload{
				Message: []uint8{},
			},
		},
		{
			Type:        eventTypeServerHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  3,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeServerMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  4,
			Authority:   ss.Address,
		},
		{
			Type:        eventTypeClientHalfClose,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  5,
			Authority:   ss.Address,
		},
		{
			Type:        eventTypeServerTrailer,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			Authority:   ss.Address,
			SequenceID:  6,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
	}
	fle.mu.Lock()
	if err := cmpLoggingEntryList(fle.entries, grpcLogEntriesWant); err != nil {
		fle.mu.Unlock()
		t.Fatalf("error in logging entry list comparison %v", err)
	}
	fle.mu.Unlock()
}

func (s) TestServerRPCEventsLogAll(t *testing.T) {
	fle := &fakeLoggingExporter{
		t: t,
	}
	defer func(ne func(ctx context.Context, config *config) (loggingExporter, error)) {
		newLoggingExporter = ne
	}(newLoggingExporter)

	newLoggingExporter = func(ctx context.Context, config *config) (loggingExporter, error) {
		return fle, nil
	}

	serverRPCEventLogAllConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ServerRPCEvents: []serverRPCEvents{
				{
					Methods:          []string{"*"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}
	cleanup, err := setupObservabilitySystemWithConfig(serverRPCEventLogAllConfig)
	if err != nil {
		t.Fatalf("error setting up observability %v", err)
	}
	defer cleanup()

	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
			return &grpc_testing.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream grpc_testing.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			if err := stream.Send(&grpc_testing.StreamingOutputCallResponse{}); err != nil {
				return err
			}
			if _, err := stream.Recv(); err != io.EOF {
				return err
			}
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.UnaryCall(ctx, &grpc_testing.SimpleRequest{}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	grpcLogEntriesWant := []*grpcLogEntry{
		{
			Type:        eventTypeClientHeader,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  1,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeClientMessage,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  2,
			Authority:   ss.Address,
		},
		{
			Type:        eventTypeServerHeader,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  3,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeServerMessage,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  4,
			Payload: payload{
				Message: []uint8{},
			},
		},
		{
			Type:        eventTypeServerTrailer,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  5,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
	}
	fle.mu.Lock()
	if err := cmpLoggingEntryList(fle.entries, grpcLogEntriesWant); err != nil {
		fle.mu.Unlock()
		t.Fatalf("error in logging entry list comparison %v", err)
	}
	fle.entries = nil
	fle.mu.Unlock()

	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}
	if err := stream.Send(&grpc_testing.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend()() failed: %v", err)
	}
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	grpcLogEntriesWant = []*grpcLogEntry{
		{
			Type:        eventTypeClientHeader,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			Authority:   ss.Address,
			SequenceID:  1,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeClientMessage,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  2,
			Authority:   ss.Address,
		},
		{
			Type:        eventTypeServerHeader,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  3,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeServerMessage,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  4,
			Authority:   ss.Address,
			Payload: payload{
				Message: []uint8{},
			},
		},
		{
			Type:        eventTypeClientHalfClose,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  5,
			Authority:   ss.Address,
		},
		{
			Type:        eventTypeServerTrailer,
			Logger:      loggerServer,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			Authority:   ss.Address,
			SequenceID:  6,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
	}
	fle.mu.Lock()
	if err := cmpLoggingEntryList(fle.entries, grpcLogEntriesWant); err != nil {
		fle.mu.Unlock()
		t.Fatalf("error in logging entry list comparison %v", err)
	}
	fle.mu.Unlock()
}

// TestBothClientAndServerRPCEvents tests the scenario where you have both
// Client and Server RPC Events configured to log. Both sides should log and
// share the exporter, so the exporter should receive the collective amount of
// calls for both a client stream (corresponding to a Client RPC Event) and a
// server stream (corresponding ot a Server RPC Event). The specificity of the
// entries are tested in previous tests.
func (s) TestBothClientAndServerRPCEvents(t *testing.T) {
	fle := &fakeLoggingExporter{
		t: t,
	}
	defer func(ne func(ctx context.Context, config *config) (loggingExporter, error)) {
		newLoggingExporter = ne
	}(newLoggingExporter)

	newLoggingExporter = func(ctx context.Context, config *config) (loggingExporter, error) {
		return fle, nil
	}

	serverRPCEventLogAllConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{"*"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
			ServerRPCEvents: []serverRPCEvents{
				{
					Methods:          []string{"*"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}

	cleanup, err := setupObservabilitySystemWithConfig(serverRPCEventLogAllConfig)
	if err != nil {
		t.Fatalf("error setting up observability %v", err)
	}
	defer cleanup()

	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
			return &grpc_testing.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream grpc_testing.TestService_FullDuplexCallServer) error {
			_, err := stream.Recv()
			if err != io.EOF {
				return err
			}
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	// Make a Unary RPC. Both client side and server side streams should log
	// entries, which share the same exporter. The exporter should thus receive
	// entries from both the client and server streams (the specificity of
	// entries is checked in previous tests).
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.UnaryCall(ctx, &grpc_testing.SimpleRequest{}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	fle.mu.Lock()
	if len(fle.entries) != 10 {
		fle.mu.Unlock()
		t.Fatalf("Unexpected length of entries %v, want 10 (collective of client and server)", len(fle.entries))
	}
	fle.mu.Unlock()
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}
	fle.mu.Lock()
	if len(fle.entries) != 16 {
		fle.mu.Unlock()
		t.Fatalf("Unexpected length of entries %v, want 16 (collective of client and server)", len(fle.entries))
	}
	fle.mu.Unlock()
}

// TestClientRPCEventsLogAll tests the observability system configured with a
// client RPC event that logs every call and that truncates headers and
// messages. It performs a Unary RPC, and expects events with truncated payloads
// and payloadTruncated set to true, signifying the system properly truncated
// headers and messages logged.
func (s) TestClientRPCEventsTruncateHeaderAndMetadata(t *testing.T) {
	fle := &fakeLoggingExporter{
		t: t,
	}
	defer func(ne func(ctx context.Context, config *config) (loggingExporter, error)) {
		newLoggingExporter = ne
	}(newLoggingExporter)

	newLoggingExporter = func(ctx context.Context, config *config) (loggingExporter, error) {
		return fle, nil
	}

	clientRPCEventLogAllConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{"*"},
					MaxMetadataBytes: 10,
					MaxMessageBytes:  2,
				},
			},
		},
	}
	cleanup, err := setupObservabilitySystemWithConfig(clientRPCEventLogAllConfig)
	if err != nil {
		t.Fatalf("error setting up observability: %v", err)
	}
	defer cleanup()

	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
			return &grpc_testing.SimpleResponse{}, nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	md := metadata.MD{
		"key1": []string{"value1"},
		"key2": []string{"value2"},
	}
	ctx = metadata.NewOutgoingContext(ctx, md)
	if _, err := ss.Client.UnaryCall(ctx, &grpc_testing.SimpleRequest{Payload: &grpc_testing.Payload{Body: []byte("00000")}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	grpcLogEntriesWant := []*grpcLogEntry{
		{
			Type:        eventTypeClientHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  1,
			Payload: payload{
				Metadata: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
			PayloadTruncated: true,
		},
		{
			Type:        eventTypeClientMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  2,
			Authority:   ss.Address,
			Payload: payload{
				MessageLength: 9,
				Message: []uint8{
					0x1a,
					0x07,
				},
			},
			PayloadTruncated: true,
		},
		{
			Type:        eventTypeServerHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  3,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeServerMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  4,
		},
		{
			Type:        eventTypeServerTrailer,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  5,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
	}
	fle.mu.Lock()
	if err := cmpLoggingEntryList(fle.entries, grpcLogEntriesWant); err != nil {
		fle.mu.Unlock()
		t.Fatalf("error in logging entry list comparison %v", err)
	}
	// Only one metadata entry should have been present in logging due to
	// truncation.
	if mdLen := len(fle.entries[0].Payload.Metadata); mdLen != 1 {
		t.Fatalf("Metadata should have only 1 entry due to truncation, got %v", mdLen)
	}
	fle.mu.Unlock()
}

// TestPrecedenceOrderingInConfiguration tests the scenario where the logging
// part of observability is configured with three client RPC events, the first
// two on specific methods in the service, the last one for any method within
// the service. This test sends three RPC's, one corresponding to each log
// entry. The logging logic dictated by that specific event should be what is
// used for emission. The second event will specify to exclude logging on RPC's,
// which should generate no log entries if an RPC gets to and matches that
// event.
func (s) TestPrecedenceOrderingInConfiguration(t *testing.T) {
	fle := &fakeLoggingExporter{
		t: t,
	}

	defer func(ne func(ctx context.Context, config *config) (loggingExporter, error)) {
		newLoggingExporter = ne
	}(newLoggingExporter)

	newLoggingExporter = func(ctx context.Context, config *config) (loggingExporter, error) {
		return fle, nil
	}

	threeEventsConfig := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{"/grpc.testing.TestService/UnaryCall"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
				{
					Methods:          []string{"/grpc.testing.TestService/EmptyCall"},
					Exclude:          true,
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
				{
					Methods:          []string{"/grpc.testing.TestService/*"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}

	cleanup, err := setupObservabilitySystemWithConfig(threeEventsConfig)
	if err != nil {
		t.Fatalf("error setting up observability %v", err)
	}
	defer cleanup()

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *grpc_testing.Empty) (*grpc_testing.Empty, error) {
			return &grpc_testing.Empty{}, nil
		},
		UnaryCallF: func(ctx context.Context, in *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
			return &grpc_testing.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream grpc_testing.TestService_FullDuplexCallServer) error {
			_, err := stream.Recv()
			if err != io.EOF {
				return err
			}
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	// A Unary RPC should match with first event and logs should correspond
	// accordingly. The first event it matches to should be used for the
	// configuration, even though it could potentially match to events in the
	// future.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.UnaryCall(ctx, &grpc_testing.SimpleRequest{}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	grpcLogEntriesWant := []*grpcLogEntry{
		{
			Type:        eventTypeClientHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  1,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeClientMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  2,
			Authority:   ss.Address,
			Payload: payload{
				Message: []uint8{},
			},
		},
		{
			Type:        eventTypeServerHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  3,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeServerMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  4,
		},
		{
			Type:        eventTypeServerTrailer,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  5,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
	}

	fle.mu.Lock()
	if err := cmpLoggingEntryList(fle.entries, grpcLogEntriesWant); err != nil {
		fle.mu.Unlock()
		t.Fatalf("error in logging entry list comparison %v", err)
	}
	fle.entries = nil
	fle.mu.Unlock()

	// A unary empty RPC should match with the second event, which has the exclude
	// flag set. Thus, a unary empty RPC should cause no downstream logs.
	if _, err := ss.Client.EmptyCall(ctx, &grpc_testing.Empty{}); err != nil {
		t.Fatalf("Unexpected error from EmptyCall: %v", err)
	}
	// The exporter should have received no new log entries due to this call.
	fle.mu.Lock()
	if len(fle.entries) != 0 {
		fle.mu.Unlock()
		t.Fatalf("Unexpected length of entries %v, want 0", len(fle.entries))
	}
	fle.mu.Unlock()

	// A third RPC, a full duplex call, which doesn't match with first two and
	// matches to last one, due to being a wildcard for every method in the
	// service, should log accordingly to the last event's logic.
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	grpcLogEntriesWant = []*grpcLogEntry{
		{
			Type:        eventTypeClientHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			Authority:   ss.Address,
			SequenceID:  1,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeClientHalfClose,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			SequenceID:  2,
			Authority:   ss.Address,
		},
		{
			Type:        eventTypeServerTrailer,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "FullDuplexCall",
			Authority:   ss.Address,
			SequenceID:  3,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
	}
	fle.mu.Lock()
	if err := cmpLoggingEntryList(fle.entries, grpcLogEntriesWant); err != nil {
		fle.mu.Unlock()
		t.Fatalf("error in logging entry list comparison %v", err)
	}
	fle.mu.Unlock()
}

func (s) TestTranslateMetadata(t *testing.T) {
	concatBinLogValue := base64.StdEncoding.EncodeToString([]byte("value1")) + "," + base64.StdEncoding.EncodeToString([]byte("value2"))
	tests := []struct {
		name     string
		binLogMD *binlogpb.Metadata
		wantMD   map[string]string
	}{
		{
			name: "two-entries-different-key",
			binLogMD: &binlogpb.Metadata{
				Entry: []*binlogpb.MetadataEntry{
					{
						Key:   "header1",
						Value: []byte("value1"),
					},
					{
						Key:   "header2",
						Value: []byte("value2"),
					},
				},
			},
			wantMD: map[string]string{
				"header1": "value1",
				"header2": "value2",
			},
		},
		{
			name: "two-entries-same-key",
			binLogMD: &binlogpb.Metadata{
				Entry: []*binlogpb.MetadataEntry{
					{
						Key:   "header1",
						Value: []byte("value1"),
					},
					{
						Key:   "header1",
						Value: []byte("value2"),
					},
				},
			},
			wantMD: map[string]string{
				"header1": "value1,value2",
			},
		},
		{
			name: "two-entries-same-key-bin-header",
			binLogMD: &binlogpb.Metadata{
				Entry: []*binlogpb.MetadataEntry{
					{
						Key:   "header1-bin",
						Value: []byte("value1"),
					},
					{
						Key:   "header1-bin",
						Value: []byte("value2"),
					},
				},
			},
			wantMD: map[string]string{
				"header1-bin": concatBinLogValue,
			},
		},
		{
			name: "four-entries-two-keys",
			binLogMD: &binlogpb.Metadata{
				Entry: []*binlogpb.MetadataEntry{
					{
						Key:   "header1",
						Value: []byte("value1"),
					},
					{
						Key:   "header1",
						Value: []byte("value2"),
					},
					{
						Key:   "header1-bin",
						Value: []byte("value1"),
					},
					{
						Key:   "header1-bin",
						Value: []byte("value2"),
					},
				},
			},
			wantMD: map[string]string{
				"header1":     "value1,value2",
				"header1-bin": concatBinLogValue,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if gotMD := translateMetadata(test.binLogMD); !cmp.Equal(gotMD, test.wantMD) {
				t.Fatalf("translateMetadata(%v) = %v, want %v", test.binLogMD, gotMD, test.wantMD)
			}
		})
	}
}

// TestCloudLoggingAPICallsFiltered tests that the observability plugin does not
// emit logs for cloud logging API calls.
func (s) TestCloudLoggingAPICallsFiltered(t *testing.T) {
	fle := &fakeLoggingExporter{
		t: t,
	}

	defer func(ne func(ctx context.Context, config *config) (loggingExporter, error)) {
		newLoggingExporter = ne
	}(newLoggingExporter)

	newLoggingExporter = func(ctx context.Context, config *config) (loggingExporter, error) {
		return fle, nil
	}
	configLogAll := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{"*"},
					MaxMetadataBytes: 30,
					MaxMessageBytes:  30,
				},
			},
		},
	}
	cleanup, err := setupObservabilitySystemWithConfig(configLogAll)
	if err != nil {
		t.Fatalf("error setting up observability %v", err)
	}
	defer cleanup()

	ss := &stubserver.StubServer{}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Any of the three cloud logging API calls should not cause any logs to be
	// emitted, even though the configuration specifies to log any rpc
	// regardless of method.
	req := &grpc_testing.SimpleRequest{}
	resp := &grpc_testing.SimpleResponse{}

	ss.CC.Invoke(ctx, "/google.logging.v2.LoggingServiceV2/some-method", req, resp)
	ss.CC.Invoke(ctx, "/google.monitoring.v3.MetricService/some-method", req, resp)
	ss.CC.Invoke(ctx, "/google.devtools.cloudtrace.v2.TraceService/some-method", req, resp)
	// The exporter should have received no new log entries due to these three
	// calls, as they should be filtered out.
	fle.mu.Lock()
	defer fle.mu.Unlock()
	if len(fle.entries) != 0 {
		t.Fatalf("Unexpected length of entries %v, want 0", len(fle.entries))
	}
}

func (s) TestMarshalJSON(t *testing.T) {
	logEntry := &grpcLogEntry{
		CallID:     "300-300-300",
		SequenceID: 3,
		Type:       eventTypeUnknown,
		Logger:     loggerClient,
		Payload: payload{
			Metadata:      map[string]string{"header1": "value1"},
			Timeout:       20,
			StatusCode:    3,
			StatusMessage: "ok",
			StatusDetails: []byte("ok"),
			MessageLength: 3,
			Message:       []byte("wow"),
		},
		Peer: address{
			Type:    typeIPv4,
			Address: "localhost",
			IPPort:  16000,
		},
		PayloadTruncated: false,
		Authority:        "server",
		ServiceName:      "grpc-testing",
		MethodName:       "UnaryRPC",
	}
	if _, err := json.Marshal(logEntry); err != nil {
		t.Fatalf("json.Marshal(%v) failed with error: %v", logEntry, err)
	}
}

// TestMetadataTruncationAccountsKey tests that the metadata truncation takes
// into account both the key and value of metadata. It configures an
// observability system with a maximum byte length for metadata, which is
// greater than just the byte length of the metadata value but less than the
// byte length of the metadata key + metadata value. Thus, in the ClientHeader
// logging event, no metadata should be logged.
func (s) TestMetadataTruncationAccountsKey(t *testing.T) {
	fle := &fakeLoggingExporter{
		t: t,
	}
	defer func(ne func(ctx context.Context, config *config) (loggingExporter, error)) {
		newLoggingExporter = ne
	}(newLoggingExporter)

	newLoggingExporter = func(ctx context.Context, config *config) (loggingExporter, error) {
		return fle, nil
	}

	const mdValue = "value"
	configMetadataLimit := &config{
		ProjectID: "fake",
		CloudLogging: &cloudLogging{
			ClientRPCEvents: []clientRPCEvents{
				{
					Methods:          []string{"*"},
					MaxMetadataBytes: len(mdValue) + 1,
				},
			},
		},
	}

	cleanup, err := setupObservabilitySystemWithConfig(configMetadataLimit)
	if err != nil {
		t.Fatalf("error setting up observability %v", err)
	}
	defer cleanup()

	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
			return &grpc_testing.SimpleResponse{}, nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// the set config MaxMetdataBytes is in between len(mdValue) and len("key")
	// + len(mdValue), and thus shouldn't log this metadata entry.
	md := metadata.MD{
		"key": []string{mdValue},
	}
	ctx = metadata.NewOutgoingContext(ctx, md)
	if _, err := ss.Client.UnaryCall(ctx, &grpc_testing.SimpleRequest{Payload: &grpc_testing.Payload{Body: []byte("00000")}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}

	grpcLogEntriesWant := []*grpcLogEntry{
		{
			Type:        eventTypeClientHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  1,
			Payload: payload{
				Metadata: map[string]string{},
			},
			PayloadTruncated: true,
		},
		{
			Type:        eventTypeClientMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  2,
			Authority:   ss.Address,
			Payload: payload{
				MessageLength: 9,
				Message:       []uint8{},
			},
			PayloadTruncated: true,
		},
		{
			Type:        eventTypeServerHeader,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  3,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
		{
			Type:        eventTypeServerMessage,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			Authority:   ss.Address,
			SequenceID:  4,
		},
		{
			Type:        eventTypeServerTrailer,
			Logger:      loggerClient,
			ServiceName: "grpc.testing.TestService",
			MethodName:  "UnaryCall",
			SequenceID:  5,
			Authority:   ss.Address,
			Payload: payload{
				Metadata: map[string]string{},
			},
		},
	}
	fle.mu.Lock()
	if err := cmpLoggingEntryList(fle.entries, grpcLogEntriesWant); err != nil {
		fle.mu.Unlock()
		t.Fatalf("error in logging entry list comparison %v", err)
	}
	fle.mu.Unlock()
}
