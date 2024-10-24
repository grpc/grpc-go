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

	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

// traceInfo is data used for recording traces.
type traceInfo struct {
	span         *trace.Span
	countSentMsg uint32
	countRecvMsg uint32
}

// traceTagRPC populates context with a new span, and serializes information
// about this span into gRPC Metadata.
func (csh *clientStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) (context.Context, *traceInfo) {
	// TODO: get consensus on whether this method name of "s.m" is correct.
	mn := "Attempt." + strings.ReplaceAll(removeLeadingSlash(rti.FullMethodName), "/", ".")
	// Returned context is ignored because will populate context with data that
	// wraps the span instead. Don't set span kind client on this attempt span
	// to prevent backend from prepending span name with "Sent.".
	_, span := trace.StartSpan(ctx, mn, trace.WithSampler(csh.to.TS))

	tcBin := propagation.Binary(span.SpanContext())
	return stats.SetTrace(ctx, tcBin), &traceInfo{
		span:         span,
		countSentMsg: 0, // msg events scoped to scope of context, per attempt client side
		countRecvMsg: 0,
	}
}

// traceTagRPC populates context with new span data, with a parent based on the
// spanContext deserialized from context passed in (wire data in gRPC metadata)
// if present.
func (ssh *serverStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) (context.Context, *traceInfo) {
	mn := strings.ReplaceAll(removeLeadingSlash(rti.FullMethodName), "/", ".")

	var span *trace.Span
	if sc, ok := propagation.FromBinary(stats.Trace(ctx)); ok {
		// Returned context is ignored because will populate context with data
		// that wraps the span instead.
		_, span = trace.StartSpanWithRemoteParent(ctx, mn, sc, trace.WithSpanKind(trace.SpanKindServer), trace.WithSampler(ssh.to.TS))
		span.AddLink(trace.Link{TraceID: sc.TraceID, SpanID: sc.SpanID, Type: trace.LinkTypeChild})
	} else {
		// Returned context is ignored because will populate context with data
		// that wraps the span instead.
		_, span = trace.StartSpan(ctx, mn, trace.WithSpanKind(trace.SpanKindServer), trace.WithSampler(ssh.to.TS))
	}

	return ctx, &traceInfo{
		span:         span,
		countSentMsg: 0,
		countRecvMsg: 0,
	}
}

// populateSpan populates span information based on stats passed in (invariants
// of the RPC lifecycle), and also ends span which triggers the span to be
// exported.
func populateSpan(_ context.Context, rs stats.RPCStats, ti *traceInfo) {
	if ti == nil || ti.span == nil {
		// Shouldn't happen, tagRPC call comes before this function gets called
		// which populates this information.
		logger.Error("ctx passed into stats handler tracing event handling has no span present")
		return
	}
	span := ti.span

	switch rs := rs.(type) {
	case *stats.Begin:
		// Note: Go always added these attributes even though they are not
		// defined by the OpenCensus gRPC spec. Thus, they are unimportant for
		// correctness.
		span.AddAttributes(
			trace.BoolAttribute("Client", rs.Client),
			trace.BoolAttribute("FailFast", rs.FailFast),
		)
	case *stats.PickerUpdated:
		span.Annotate(nil, "Delayed LB pick complete")
	case *stats.InPayload:
		// message id - "must be calculated as two different counters starting
		// from one for sent messages and one for received messages."
		mi := atomic.AddUint32(&ti.countRecvMsg, 1)
		span.AddMessageReceiveEvent(int64(mi), int64(rs.Length), int64(rs.CompressedLength))
	case *stats.OutPayload:
		mi := atomic.AddUint32(&ti.countSentMsg, 1)
		span.AddMessageSendEvent(int64(mi), int64(rs.Length), int64(rs.CompressedLength))
	case *stats.End:
		if rs.Error != nil {
			// "The mapping between gRPC canonical codes and OpenCensus codes
			// can be found here", which implies 1:1 mapping to gRPC statuses
			// (OpenCensus statuses are based off gRPC statuses and a subset).
			s := status.Convert(rs.Error)
			span.SetStatus(trace.Status{Code: int32(s.Code()), Message: s.Message()})
		} else {
			span.SetStatus(trace.Status{Code: trace.StatusCodeOK}) // could get rid of this else conditional and just leave as 0 value, but this makes it explicit
		}
		span.End()
	}
}
