/*
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
 */

package opencensus

import (
	"context"
	"strings"
	"sync/atomic"

	"go.opencensus.io/trace"
	"go.opencensus.io/trace/propagation"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

type spanWithMsgCountKey struct{}

// getSpanWithMsgCount returns the msgEventsCount stored in the context, or nil
// if there isn't one.
func getSpanWithMsgCount(ctx context.Context) *spanWithMsgCount {
	swmc, _ := ctx.Value(spanWithMsgCountKey{}).(*spanWithMsgCount)
	return swmc
}

// setSpanWithMsgCount stores a spanWithMsgCount in the context.
func setSpanWithMsgCount(ctx context.Context, swmc *spanWithMsgCount) context.Context {
	// needs to be on the heap to persist changes over time so pointer.
	return context.WithValue(ctx, spanWithMsgCountKey{}, swmc)
}

// spanWithMsgCount wraps a trace.Span to count msg events.
type spanWithMsgCount struct {
	*trace.Span
	countSentMsg uint32
	countRecvMsg uint32
}

// used to attach data client side and deserialize server side
// traceContextMDKey is the speced key for gRPC Metadata that carries binary
// serialized traceContext.
const traceContextMDKey = "grpc-trace-bin"

// traceTagRPC populates context with a new span, and serializes information
// about this span into gRPC Metadata.
func (csh *clientStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) context.Context {
	// TODO: get consensus on whether this method name of "s.m" is correct.
	mn := strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)
	_, span := trace.StartSpan(ctx, mn, trace.WithSampler(csh.to.TS), trace.WithSpanKind(trace.SpanKindClient))

	swmc := &spanWithMsgCount{
		Span:         span,
		countSentMsg: 0, // msg events scoped to scope of context, per attempt client side
		countRecvMsg: 0,
	}
	ctx = setSpanWithMsgCount(ctx, swmc)
	tcBin := propagation.Binary(span.SpanContext())
	return metadata.AppendToOutgoingContext(ctx, traceContextMDKey, string(tcBin))
}

// traceTagRPC populates context with new span data, with a parent based on the
// spanContext deserialized from context passed in (wire data in gRPC metadata)
// if present.
func (ssh *serverStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) context.Context {
	// TODO: get consensus on whether this method name of "s.m" is correct.
	mn := strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)
	var sc trace.SpanContext
	// gotSpanContext represents if after all the logic to get span context out
	// of context passed in, whether there was a span context which represents
	// the future created server span's parent or not.
	var gotSpanContext bool

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// Shouldn't happen, but if does just notify caller through error log,
		// and create a span without remote parent.
		logger.Error("server side context passed into stats handler has no metadata")
	} else {
		tcMDVal := md[traceContextMDKey]
		if len(tcMDVal) != 0 {
			tcBin := []byte(tcMDVal[0])
			// Only if this function gets through all of these operations and the
			// binary metadata value successfully unmarshals means the server
			// received a span context, meaning this created server span should have
			// a remote parent corresponding to whatever was received off the wire
			// and unmarshaled into this span context.
			sc, gotSpanContext = propagation.FromBinary(tcBin)
		}
	}

	var span *trace.Span
	if gotSpanContext {
		_, span = trace.StartSpanWithRemoteParent(ctx, mn, sc, trace.WithSpanKind(trace.SpanKindServer), trace.WithSampler(ssh.to.TS))
		span.AddLink(trace.Link{TraceID: sc.TraceID, SpanID: sc.SpanID, Type: trace.LinkTypeChild})
	} else {
		_, span = trace.StartSpan(ctx, mn, trace.WithSpanKind(trace.SpanKindServer), trace.WithSampler(ssh.to.TS))
	}

	swmc := &spanWithMsgCount{
		Span:         span,
		countSentMsg: 0,
		countRecvMsg: 0,
	}
	ctx = setSpanWithMsgCount(ctx, swmc)
	return ctx
}

// populateSpan populates span information based on stats passed in (invariants
// of the RPC lifecycle), and also ends span which triggers the span to be
// exported.
func populateSpan(ctx context.Context, rs stats.RPCStats) {
	swmc := getSpanWithMsgCount(ctx)
	if swmc == nil || swmc.Span == nil {
		// Shouldn't happen, tagRPC call comes before this function gets called
		// which populates this information.
		logger.Error("ctx passed into stats handler tracing event handling has no span present")
		return
	}
	span := swmc.Span

	switch rs := rs.(type) {
	case *stats.Begin:
		// Note: not present in Java, but left in because already there, so why
		// not. Thus untested due to no formal definition and not important for
		// correctness.
		span.AddAttributes(
			trace.BoolAttribute("Client", rs.Client),
			trace.BoolAttribute("FailFast", rs.FailFast),
		)
	case *stats.InPayload:
		// message id - "must be calculated as two different counters starting
		// from 1 one for sent messages and one for received messages."
		mi := atomic.AddUint32(&swmc.countRecvMsg, 1)
		span.AddMessageReceiveEvent(int64(mi), int64(rs.Length), int64(rs.WireLength))
	case *stats.OutPayload:
		mi := atomic.AddUint32(&swmc.countSentMsg, 1)
		span.AddMessageSendEvent(int64(mi), int64(rs.Length), int64(rs.WireLength))
	case *stats.End:
		if rs.Error != nil {
			// "The mapping between gRPC canonical codes and OpenCensus codes
			// can be found here", which implies 1:1 mapping to gRPC statuses
			// (OpenCensus statuses are based off gRPC statuses and a subset).
			s, _ := status.FromError(rs.Error) // ignore second argument because codes.Unknown is fine and is correct the correct status to populate span with.
			span.SetStatus(trace.Status{Code: int32(s.Code()), Message: s.Message()})
		} else {
			span.SetStatus(trace.Status{Code: trace.StatusCodeOK}) // could get rid of this else conditional and just leave as 0 value, but this makes it explicit
		}
		span.End()
	}
}
