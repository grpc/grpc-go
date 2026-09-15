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

package stats

import (
	"context"
	"testing"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/metadata"
)

// The Unimplemented and Nop types must themselves satisfy the interfaces they
// exist to make implementable. Asserting this at compile time is not ceremony:
// a marker-only struct compiles on its own but fails to satisfy its interface,
// so every implementer embedding it would break instead.
var (
	_ Handler             = UnimplementedHandler{}
	_ ClientCallTracer    = UnimplementedClientCallTracer{}
	_ ClientAttemptTracer = UnimplementedClientAttemptTracer{}
	_ ServerCallTracer    = UnimplementedServerCallTracer{}
	_ ClientCallTracer    = NopClientCallTracer{}
	_ ClientAttemptTracer = NopClientAttemptTracer{}
	_ ServerCallTracer    = NopServerCallTracer{}
)

// minimalHandler is the smallest thing a user can write: embed the
// Unimplemented type and override nothing. It must satisfy Handler - which does
// NOT include MetricsRecorder, so a bare handler is not a recorder.
type minimalHandler struct {
	UnimplementedHandler
}

var _ Handler = minimalHandler{}

// recordingHandler opts into non-per-call metrics by ALSO embedding
// MetricsRecorder, exactly as a V1 stats.Handler does today. gRPC discovers
// this by assertion.
type recordingHandler struct {
	UnimplementedHandler
	UnimplementedMetricsRecorder
}

var _ Handler = recordingHandler{}

// TestBareHandlerIsNotRecorder pins that a Handler is not automatically a
// MetricsRecorder; the capability is opt-in (SD-6).
func (s) TestBareHandlerIsNotRecorder(t *testing.T) {
	if _, ok := any(minimalHandler{}).(MetricsRecorder); ok {
		t.Error("minimalHandler satisfies MetricsRecorder; it must not without opting in")
	}
}

// TestRecordingHandlerIsRecorder pins that a Handler that embeds
// MetricsRecorder is discovered as one, and is still a Handler.
func (s) TestRecordingHandlerIsRecorder(t *testing.T) {
	if _, ok := any(recordingHandler{}).(MetricsRecorder); !ok {
		t.Error("recordingHandler does not satisfy MetricsRecorder despite embedding it")
	}
}

// TestUnimplementedHandlerTracersNonNil verifies the tracer-returning methods
// never hand back nil. gRPC invokes methods on whatever it receives, so a nil
// return would panic; the Nop types are the opt-out instead.
func (s) TestUnimplementedHandlerTracersNonNil(t *testing.T) {
	h := minimalHandler{}
	if got := h.ClientCallTracer(context.Background(), &ClientCallInfo{Method: "/s/m"}); got == nil {
		t.Error("ClientCallTracer() = nil, want non-nil")
	}
	if got := h.ServerCallTracer(&ServerCallInfo{Method: "/s/m"}); got == nil {
		t.Error("ServerCallTracer() = nil, want non-nil")
	}
	if got := (UnimplementedClientCallTracer{}).StartAttempt(&AttemptInfo{}); got == nil {
		t.Error("StartAttempt() = nil, want non-nil")
	}
}

// TestNopTracersAreCallable verifies every method on the Nop tracers can be
// invoked without panicking, since declining a call means gRPC will still
// report every event to the returned tracer.
func (s) TestNopTracersAreCallable(t *testing.T) {
	ct := NopClientCallTracer{}
	ct.RecordAnnotation(AnnotationNameResolutionComplete)
	at := ct.StartAttempt(&AttemptInfo{})
	at.MutateOutgoingHeaders(metadata.MD{})
	at.RecordOutgoingHeaders(&HeadersInfo{})
	at.RecordIncomingHeaders(&HeadersInfo{})
	at.RecordIncomingTrailers(&TrailersInfo{})
	at.RecordOutgoingMessage(&MessageInfo{})
	at.RecordIncomingMessage(&MessageInfo{})
	at.AddOptionalLabel(LabelLocality, "region/zone/subzone")
	at.RecordAnnotation(AnnotationDelayedPickComplete)
	at.RecordEnd(&AttemptEndInfo{})
	ct.RecordEnd(&CallEndInfo{})

	st := NopServerCallTracer{}
	st.RecordIncomingHeaders(&HeadersInfo{})
	st.MutateOutgoingHeaders(metadata.MD{})
	st.RecordOutgoingHeaders(&HeadersInfo{})
	st.MutateOutgoingTrailers(metadata.MD{})
	st.RecordOutgoingTrailers(&TrailersInfo{})
	st.RecordIncomingMessage(&MessageInfo{})
	st.RecordOutgoingMessage(&MessageInfo{})
	st.RecordEnd(&CallEndInfo{})
}

// TestFilterContextReturnsInputUnchanged verifies the no-op FilterContext is
// identity. A no-op that returned nil or context.Background() would silently
// destroy the call's context, so this default has to be the safe one.
func (s) TestFilterContextReturnsInputUnchanged(t *testing.T) {
	type keyType struct{}
	ctx := context.WithValue(context.Background(), keyType{}, "value")
	got := UnimplementedServerCallTracer{}.FilterContext(ctx)
	if got == nil {
		t.Fatal("FilterContext() = nil, want the input context")
	}
	if got.Value(keyType{}) != "value" {
		t.Errorf("FilterContext() dropped the context value; got %v", got.Value(keyType{}))
	}
}

// TestStatsHandlerV2DisabledByDefault guards the gate: the V2 API must be inert
// unless explicitly enabled, so that adding it cannot change existing behavior.
func (s) TestStatsHandlerV2DisabledByDefault(t *testing.T) {
	if envconfig.StatsHandlerV2 {
		t.Error("envconfig.StatsHandlerV2 = true, want false by default")
	}
}
