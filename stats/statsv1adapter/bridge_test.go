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

package statsv1adapter

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"

	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal/grpctest"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// fakeV1Handler is a V1 stats.Handler that records every TagRPC and HandleRPC
// call, so a test can assert the events the bridge synthesized.
type fakeV1Handler struct {
	mu       sync.Mutex
	tagInfos []*stats.RPCTagInfo
	events   []stats.RPCStats
	// tagRPCFunc, if set, derives the returned context (e.g. to append outgoing
	// metadata or register a label callback), mirroring what a real V1 handler
	// does in TagRPC.
	tagRPCFunc func(context.Context, *stats.RPCTagInfo) context.Context
}

func (h *fakeV1Handler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	h.mu.Lock()
	h.tagInfos = append(h.tagInfos, info)
	h.mu.Unlock()
	if h.tagRPCFunc != nil {
		return h.tagRPCFunc(ctx, info)
	}
	return ctx
}

func (h *fakeV1Handler) HandleRPC(_ context.Context, rs stats.RPCStats) {
	h.mu.Lock()
	h.events = append(h.events, rs)
	h.mu.Unlock()
}

func (h *fakeV1Handler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *fakeV1Handler) HandleConn(context.Context, stats.ConnStats) {}

// eventTypes returns the concrete type names of the recorded events, in order,
// for asserting the event sequence.
func (h *fakeV1Handler) eventTypes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.events))
	for i, e := range h.events {
		out[i] = reflect.TypeOf(e).String()
	}
	return out
}

func addr(s string) net.Addr { return &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 5, Zone: s} }

// TestClientAttemptLifecycle drives a full client attempt through the bridge and
// asserts the ordered V1 event stream and the load-bearing fields of each event.
func (s) TestClientAttemptLifecycle(t *testing.T) {
	h := &fakeV1Handler{}
	remote, local := addr("remote"), addr("local")
	hdr := metadata.MD{"k": []string{"v"}}
	trl := metadata.MD{"t": []string{"w"}}
	wantErr := errors.New("boom")

	ct := Wrap(h).ClientCallTracer(context.Background(), &estats.ClientCallInfo{
		Method:         "/s/m",
		IsClientStream: true,
		IsServerStream: false,
	})
	at := ct.StartAttempt(&estats.AttemptInfo{
		WaitForReady:       false,
		Authority:          "auth.example",
		IsTransparentRetry: true,
	})
	at.RecordAnnotation(estats.AnnotationDelayedPickComplete)
	at.RecordOutgoingHeaders(&estats.HeadersInfo{Compression: "gzip", Headers: hdr, Peer: &peer.Peer{Addr: remote, LocalAddr: local}})
	at.RecordIncomingHeaders(&estats.HeadersInfo{WireLength: 10, Compression: "gzip", Headers: hdr})
	at.RecordOutgoingMessage(&estats.MessageInfo{Length: 5, CompressedLength: 4, WireLength: 9})
	at.RecordIncomingMessage(&estats.MessageInfo{Length: 6, CompressedLength: 5, WireLength: 10})
	at.RecordIncomingTrailers(&estats.TrailersInfo{WireLength: 3, Trailers: trl})
	at.RecordEnd(&estats.AttemptEndInfo{Error: wantErr})
	ct.RecordEnd(&estats.CallEndInfo{})

	want := []string{
		"*stats.Begin",
		"*stats.DelayedPickComplete",
		"*stats.OutHeader",
		"*stats.InHeader",
		"*stats.OutPayload",
		"*stats.InPayload",
		"*stats.InTrailer",
		"*stats.End",
	}
	if got := h.eventTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}

	// TagRPC carries method and the failFast (= !waitForReady) bit.
	if len(h.tagInfos) != 1 {
		t.Fatalf("TagRPC called %d times, want 1", len(h.tagInfos))
	}
	if ti := h.tagInfos[0]; ti.FullMethodName != "/s/m" || !ti.FailFast {
		t.Errorf("TagRPC info = %+v, want FullMethodName=/s/m FailFast=true", ti)
	}

	begin := h.events[0].(*stats.Begin)
	if !begin.Client || !begin.FailFast || !begin.IsClientStream || begin.IsServerStream || !begin.IsTransparentRetryAttempt {
		t.Errorf("Begin = %+v, want Client, FailFast, IsClientStream, !IsServerStream, IsTransparentRetryAttempt", begin)
	}
	outHdr := h.events[2].(*stats.OutHeader)
	if outHdr.FullMethod != "/s/m" || outHdr.Compression != "gzip" || outHdr.RemoteAddr.String() != remote.String() || outHdr.LocalAddr.String() != local.String() {
		t.Errorf("OutHeader = %+v, want method/gzip/remote/local populated", outHdr)
	}
	inHdr := h.events[3].(*stats.InHeader)
	if !inHdr.Client || inHdr.WireLength != 10 || inHdr.Compression != "gzip" {
		t.Errorf("InHeader = %+v, want Client WireLength=10 gzip", inHdr)
	}
	outPay := h.events[4].(*stats.OutPayload)
	if !outPay.Client || outPay.Length != 5 || outPay.CompressedLength != 4 || outPay.WireLength != 9 || outPay.Payload != nil {
		t.Errorf("OutPayload = %+v, want Client sizes 5/4/9 and nil Payload", outPay)
	}
	inTrl := h.events[6].(*stats.InTrailer)
	if !inTrl.Client || inTrl.WireLength != 3 {
		t.Errorf("InTrailer = %+v, want Client WireLength=3", inTrl)
	}
	end := h.events[7].(*stats.End)
	if !end.Client || end.Error != wantErr || !reflect.DeepEqual(end.Trailer, trl) {
		t.Errorf("End = %+v, want Client, wantErr, captured trailer", end)
	}
}

// TestClientNameResolutionDelay pins that a call-level name-resolution-complete
// annotation recorded before StartAttempt is reported on every attempt's TagRPC,
// matching V1's RPCTagInfo.NameResolutionDelay.
func (s) TestClientNameResolutionDelay(t *testing.T) {
	h := &fakeV1Handler{}
	ct := Wrap(h).ClientCallTracer(context.Background(), &estats.ClientCallInfo{Method: "/s/m"})
	ct.RecordAnnotation(estats.AnnotationNameResolutionComplete)
	ct.StartAttempt(&estats.AttemptInfo{})
	ct.StartAttempt(&estats.AttemptInfo{})

	if len(h.tagInfos) != 2 {
		t.Fatalf("TagRPC called %d times, want 2", len(h.tagInfos))
	}
	for i, ti := range h.tagInfos {
		if !ti.NameResolutionDelay {
			t.Errorf("attempt %d TagRPC NameResolutionDelay = false, want true", i)
		}
	}
}

// TestClientOutgoingHeaderInjection pins that metadata the wrapped handler
// appends to the outgoing context in TagRPC is surfaced through the V2
// MutateOutgoingHeaders hook - the bridge for V1 trace-context injection.
func (s) TestClientOutgoingHeaderInjection(t *testing.T) {
	h := &fakeV1Handler{
		tagRPCFunc: func(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
			return metadata.AppendToOutgoingContext(ctx, "grpc-trace-bin", "SPAN")
		},
	}
	ct := Wrap(h).ClientCallTracer(context.Background(), &estats.ClientCallInfo{Method: "/s/m"})
	at := ct.StartAttempt(&estats.AttemptInfo{})

	md := metadata.MD{}
	at.MutateOutgoingHeaders(md)
	if got := md["grpc-trace-bin"]; !reflect.DeepEqual(got, []string{"SPAN"}) {
		t.Errorf("MutateOutgoingHeaders injected %v, want [SPAN]", got)
	}
}

// TestClientOutgoingHeaderInjectionPreservesExisting pins that only the values
// the handler ADDED are injected, not values already present on the call
// context.
func (s) TestClientOutgoingHeaderInjectionPreservesExisting(t *testing.T) {
	h := &fakeV1Handler{
		tagRPCFunc: func(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
			return metadata.AppendToOutgoingContext(ctx, "x", "added")
		},
	}
	base := metadata.NewOutgoingContext(context.Background(), metadata.MD{"x": []string{"existing"}})
	ct := Wrap(h).ClientCallTracer(base, &estats.ClientCallInfo{Method: "/s/m"})
	at := ct.StartAttempt(&estats.AttemptInfo{})

	md := metadata.MD{}
	at.MutateOutgoingHeaders(md)
	if got := md["x"]; !reflect.DeepEqual(got, []string{"added"}) {
		t.Errorf("MutateOutgoingHeaders injected %v, want only [added]", got)
	}
}

// TestAddOptionalLabelFiresCallback pins that AddOptionalLabel reaches the
// telemetry-label callback the wrapped handler registered in TagRPC.
func (s) TestAddOptionalLabelFiresCallback(t *testing.T) {
	var got map[string]string
	h := &fakeV1Handler{
		tagRPCFunc: func(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
			return istats.RegisterTelemetryLabelCallback(ctx, func(l map[string]string) {
				got = l
			})
		},
	}
	ct := Wrap(h).ClientCallTracer(context.Background(), &estats.ClientCallInfo{Method: "/s/m"})
	at := ct.StartAttempt(&estats.AttemptInfo{})
	at.AddOptionalLabel(estats.LabelLocality, "region/zone/subzone")

	if want := map[string]string{estats.LabelLocality: "region/zone/subzone"}; !reflect.DeepEqual(got, want) {
		t.Errorf("label callback got %v, want %v", got, want)
	}
}

// TestServerLifecycleOrder drives a server call through the bridge and asserts
// the V1 order (TagRPC -> InHeader -> Begin -> ... -> End) is reproduced even
// though V2 delivers the incoming headers before FilterContext.
func (s) TestServerLifecycleOrder(t *testing.T) {
	h := &fakeV1Handler{}
	remote, local := addr("remote"), addr("local")
	hdr := metadata.MD{"k": []string{"v"}}

	st := Wrap(h).ServerCallTracer(&estats.ServerCallInfo{
		Method:  "/s/m",
		Headers: hdr,
		Peer:    &peer.Peer{Addr: remote, LocalAddr: local},
	})
	st.RecordIncomingHeaders(&estats.HeadersInfo{WireLength: 12, Compression: "gzip", Headers: hdr, Peer: &peer.Peer{Addr: remote, LocalAddr: local}})
	ctx := st.FilterContext(context.Background())
	if ctx == nil {
		t.Fatal("FilterContext returned nil context")
	}
	st.RecordIncomingMessage(&estats.MessageInfo{Length: 1, CompressedLength: 1, WireLength: 6})
	st.RecordOutgoingHeaders(&estats.HeadersInfo{Compression: "gzip", Headers: hdr})
	st.RecordOutgoingMessage(&estats.MessageInfo{Length: 2, CompressedLength: 2, WireLength: 7})
	st.RecordOutgoingTrailers(&estats.TrailersInfo{Trailers: hdr})
	st.RecordEnd(&estats.CallEndInfo{})

	want := []string{
		"*stats.InHeader",
		"*stats.Begin",
		"*stats.InPayload",
		"*stats.OutHeader",
		"*stats.OutPayload",
		"*stats.OutTrailer",
		"*stats.End",
	}
	if got := h.eventTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}

	if len(h.tagInfos) != 1 || h.tagInfos[0].FullMethodName != "/s/m" || h.tagInfos[0].FailFast {
		t.Errorf("TagRPC infos = %+v, want one with FullMethodName=/s/m FailFast=false", h.tagInfos)
	}
	inHdr := h.events[0].(*stats.InHeader)
	if inHdr.Client || inHdr.FullMethod != "/s/m" || inHdr.WireLength != 12 || inHdr.Compression != "gzip" ||
		inHdr.RemoteAddr.String() != remote.String() || inHdr.LocalAddr.String() != local.String() {
		t.Errorf("InHeader = %+v, want server-side fields populated", inHdr)
	}
	if begin := h.events[1].(*stats.Begin); begin.Client {
		t.Errorf("Begin.Client = true, want false (server)")
	}
	if end := h.events[6].(*stats.End); end.Client {
		t.Errorf("End.Client = true, want false (server)")
	}
}

// TestServerMutateOutgoingIsNoop pins that the server mutate hooks do not touch
// the metadata (a V1 server stats.Handler has no outgoing-injection path).
func (s) TestServerMutateOutgoingIsNoop(t *testing.T) {
	h := &fakeV1Handler{}
	st := Wrap(h).ServerCallTracer(&estats.ServerCallInfo{Method: "/s/m"})
	st.FilterContext(context.Background())
	md := metadata.MD{}
	st.MutateOutgoingHeaders(md)
	st.MutateOutgoingTrailers(md)
	if len(md) != 0 {
		t.Errorf("server mutate hooks wrote %v, want no changes", md)
	}
}

// recordingV1Handler is a V1 handler that also implements
// estats.MetricsRecorder, like the OpenTelemetry plugin.
type recordingV1Handler struct {
	fakeV1Handler
	estats.UnimplementedMetricsRecorder
}

// TestWrapDiscoversMetricsRecorder pins SD-6b's forwarding: a wrapped handler is
// discoverable as a MetricsRecorder iff the underlying V1 handler is one.
func (s) TestWrapDiscoversMetricsRecorder(t *testing.T) {
	if _, ok := Wrap(&fakeV1Handler{}).(estats.MetricsRecorder); ok {
		t.Error("bridge over a plain V1 handler satisfies MetricsRecorder; it must not")
	}
	if _, ok := Wrap(&recordingV1Handler{}).(estats.MetricsRecorder); !ok {
		t.Error("bridge over a recorder V1 handler does not satisfy MetricsRecorder")
	}
}

// TestWrapNeverReturnsNilTracers pins that both bridge variants vend non-nil
// tracers, since gRPC invokes methods on whatever they return.
func (s) TestWrapNeverReturnsNilTracers(t *testing.T) {
	for _, h := range []stats.Handler{&fakeV1Handler{}, &recordingV1Handler{}} {
		w := Wrap(h)
		if got := w.ClientCallTracer(context.Background(), &estats.ClientCallInfo{Method: "/s/m"}); got == nil {
			t.Errorf("Wrap(%T).ClientCallTracer() = nil, want non-nil", h)
		}
		if got := w.ServerCallTracer(&estats.ServerCallInfo{Method: "/s/m"}); got == nil {
			t.Errorf("Wrap(%T).ServerCallTracer() = nil, want non-nil", h)
		}
	}
}
