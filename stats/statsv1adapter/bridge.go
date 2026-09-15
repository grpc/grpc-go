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

// Package statsv1adapter adapts a V1 google.golang.org/grpc/stats.Handler so it
// can be registered as a V2 experimental/stats.Handler. It is the sunset
// off-ramp for the V1 stats API: an application that has a V1 handler it cannot
// yet replace wraps it once, at the edge, and keeps receiving V1 events while
// gRPC drives only the V2 tracers internally.
//
// # Fidelity
//
// The adapter synthesizes V1 stats.RPCStats events from V2 tracer method calls.
// Every size, timing, status and metadata field a V1 handler reads is
// reproduced. The known departures from a natively-driven V1 handler are:
//
//   - InPayload.Payload / OutPayload.Payload are always nil. V2 does not model
//     the decoded message (only its sizes), by design. A handler that reads
//     .Payload - none in the gRPC tree, and not otelgrpc - loses it; every
//     other handler ports losslessly.
//   - Timestamps (Begin/End/{In,Out}Payload) are taken with time.Now() inside
//     the adapter, because V2 signatures carry no timestamps (the plugin owns
//     the clock). Durations a handler computes from them are accurate to within
//     the adapter's own call overhead; no in-tree handler reads the absolute
//     values.
//   - Server-side Begin.IsClientStream / Begin.IsServerStream are false: the V2
//     ServerCallInfo does not carry the stream shape. (The client side does, via
//     ClientCallInfo, and is reproduced.)
//   - Connection stats (TagConn/HandleConn/ConnBegin/ConnEnd) are never
//     delivered: V2 has no connection scope. All six in-tree HandleConn bodies
//     are empty, so this is zero-cost in practice.
//
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed in a later
// release.
package statsv1adapter

import (
	"context"
	"net"
	"time"

	estats "google.golang.org/grpc/experimental/stats"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
)

// Wrap presents an existing V1 stats.Handler as a V2 experimental/stats.Handler.
//
// The returned Handler forwards V2 tracer events to h as the corresponding V1
// stats.RPCStats events; see the package documentation for the fidelity
// contract. If h also implements experimental/stats.MetricsRecorder (as the
// OpenTelemetry plugin does, by embedding the metric registry), the returned
// Handler implements it too, forwarding to h - so gRPC's assertion-based
// discovery of non-per-call metric recorders still finds it.
func Wrap(h stats.Handler) estats.Handler {
	b := &handlerBridge{h: h}
	if mr, ok := h.(estats.MetricsRecorder); ok {
		return &recordingHandlerBridge{handlerBridge: b, MetricsRecorder: mr}
	}
	return b
}

// handlerBridge adapts one V1 handler to the V2 Handler interface. It holds no
// per-call state; a fresh call/attempt adapter is created per tracer.
type handlerBridge struct {
	estats.UnimplementedHandler
	h stats.Handler
}

// recordingHandlerBridge is the variant returned when the wrapped handler is
// also a MetricsRecorder. The embedded interface value promotes the recorder
// methods, so this type satisfies both estats.Handler and
// estats.MetricsRecorder.
type recordingHandlerBridge struct {
	*handlerBridge
	estats.MetricsRecorder
}

// ClientCallTracer begins tracing a client call. The application context is
// held so it can seed each attempt's V1 TagRPC, which is what carries the V1
// handler's context-keyed state (and its outgoing trace-context injection).
func (b *handlerBridge) ClientCallTracer(ctx context.Context, info *estats.ClientCallInfo) estats.ClientCallTracer {
	return &clientCallBridge{h: b.h, callCtx: ctx, info: info}
}

// ServerCallTracer begins tracing a server call.
func (b *handlerBridge) ServerCallTracer(info *estats.ServerCallInfo) estats.ServerCallTracer {
	return &serverCallBridge{h: b.h, info: info}
}

// clientCallBridge is the call-scoped adapter. V1 has no call-level events - it
// is attempt-scoped - so this object mostly carries state down to each attempt.
type clientCallBridge struct {
	estats.UnimplementedClientCallTracer
	h       stats.Handler
	callCtx context.Context
	info    *estats.ClientCallInfo

	// nameResolutionDelayed records that the call blocked on the initial name
	// resolution. It is set by RecordAnnotation before the first StartAttempt
	// (name resolution completes before the first pick, and StartAttempt runs
	// before the pick) and is then reported on every attempt's TagRPC, matching
	// V1, where all attempts of a delayed call carry the bit.
	nameResolutionDelayed bool
}

// StartAttempt maps a V2 attempt onto one V1 attempt lifecycle: it calls TagRPC
// and holds the returned context, then emits Begin. Every later event on the
// attempt is reported against that held context.
func (c *clientCallBridge) StartAttempt(info *estats.AttemptInfo) estats.ClientAttemptTracer {
	// failFast is the inverse of wait-for-ready; V1 carries failFast.
	failFast := !info.WaitForReady
	attemptCtx := c.h.TagRPC(c.callCtx, &stats.RPCTagInfo{
		FullMethodName:      c.info.Method,
		FailFast:            failFast,
		NameResolutionDelay: c.nameResolutionDelayed,
	})
	beginTime := time.Now()
	c.h.HandleRPC(attemptCtx, &stats.Begin{
		Client:                    true,
		BeginTime:                 beginTime,
		FailFast:                  failFast,
		IsClientStream:            c.info.IsClientStream,
		IsServerStream:            c.info.IsServerStream,
		IsTransparentRetryAttempt: info.IsTransparentRetry,
	})
	return &clientAttemptBridge{
		h:          c.h,
		callCtx:    c.callCtx,
		attemptCtx: attemptCtx,
		info:       c.info,
		attempt:    info,
		beginTime:  beginTime,
	}
}

// RecordAnnotation records the pre-first-attempt name-resolution-delay signal.
// gRPC records it on the call tracer; V1 exposes it as RPCTagInfo.NameResolutionDelay.
func (c *clientCallBridge) RecordAnnotation(annotation string) {
	if annotation == estats.AnnotationNameResolutionComplete {
		c.nameResolutionDelayed = true
	}
}

// RecordEnd has no V1 equivalent: V1 emits End per attempt, which the attempt
// adapter already does. Nothing to forward at call scope.
func (c *clientCallBridge) RecordEnd(*estats.CallEndInfo) {}

// clientAttemptBridge is the attempt-scoped adapter. attemptCtx is the TagRPC'd
// context and is the sole context handed to every HandleRPC for this attempt.
type clientAttemptBridge struct {
	estats.UnimplementedClientAttemptTracer
	h          stats.Handler
	callCtx    context.Context
	attemptCtx context.Context
	info       *estats.ClientCallInfo
	attempt    *estats.AttemptInfo
	beginTime  time.Time

	// trailer is captured from RecordIncomingTrailers and replayed into the
	// deprecated End.Trailer field, matching V1.
	trailer metadata.MD
}

// MutateOutgoingHeaders injects the metadata the wrapped handler appended to the
// outgoing context inside TagRPC. This is exactly how a V1 handler (e.g. OTel
// trace-context injection) contributes outgoing metadata: it calls
// metadata.AppendToOutgoingContext in TagRPC and returns the context. Because
// TagRPC ran per attempt, each attempt re-injects its own headers.
func (a *clientAttemptBridge) MutateOutgoingHeaders(md metadata.MD) {
	before, _ := metadata.FromOutgoingContext(a.callCtx)
	after, ok := metadata.FromOutgoingContext(a.attemptCtx)
	if !ok {
		return
	}
	// AppendToOutgoingContext appends, and FromOutgoingContext returns base
	// values before appended ones, so the suffix past the base length is what
	// the handler added.
	for k, av := range after {
		if extra := len(av) - len(before[k]); extra > 0 {
			md[k] = append(md[k], av[len(av)-extra:]...)
		}
	}
}

// RecordOutgoingHeaders emits the V1 client OutHeader. FullMethod and Authority
// come from the held call/attempt info; the peer supplies the addresses.
func (a *clientAttemptBridge) RecordOutgoingHeaders(info *estats.HeadersInfo) {
	remote, local := addrs(info.Peer)
	a.h.HandleRPC(a.attemptCtx, &stats.OutHeader{
		Client:      true,
		Compression: info.Compression,
		Header:      info.Headers,
		Authority:   a.attempt.Authority,
		FullMethod:  a.info.Method,
		RemoteAddr:  remote,
		LocalAddr:   local,
	})
}

// RecordIncomingHeaders emits the V1 client InHeader.
func (a *clientAttemptBridge) RecordIncomingHeaders(info *estats.HeadersInfo) {
	a.h.HandleRPC(a.attemptCtx, &stats.InHeader{
		Client:      true,
		WireLength:  info.WireLength,
		Compression: info.Compression,
		Header:      info.Headers,
	})
}

// RecordIncomingTrailers emits the V1 client InTrailer and captures the trailer
// for the deprecated End.Trailer field.
func (a *clientAttemptBridge) RecordIncomingTrailers(info *estats.TrailersInfo) {
	a.trailer = info.Trailers
	a.h.HandleRPC(a.attemptCtx, &stats.InTrailer{
		Client:     true,
		WireLength: info.WireLength,
		Trailer:    info.Trailers,
	})
}

// RecordOutgoingMessage emits the V1 client OutPayload (sizes only; Payload nil).
func (a *clientAttemptBridge) RecordOutgoingMessage(info *estats.MessageInfo) {
	a.h.HandleRPC(a.attemptCtx, &stats.OutPayload{
		Client:           true,
		Length:           info.Length,
		CompressedLength: info.CompressedLength,
		WireLength:       info.WireLength,
		SentTime:         time.Now(),
	})
}

// RecordIncomingMessage emits the V1 client InPayload (sizes only; Payload nil).
func (a *clientAttemptBridge) RecordIncomingMessage(info *estats.MessageInfo) {
	a.h.HandleRPC(a.attemptCtx, &stats.InPayload{
		Client:           true,
		Length:           info.Length,
		CompressedLength: info.CompressedLength,
		WireLength:       info.WireLength,
		RecvTime:         time.Now(),
	})
}

// AddOptionalLabel bridges an xDS optional label back to the callback the
// wrapped handler registered in its TagRPC, which is how the V1 label-delivery
// path (istats.UpdateLabels) reaches it.
func (a *clientAttemptBridge) AddOptionalLabel(key, value string) {
	istats.UpdateLabels(a.attemptCtx, map[string]string{key: value})
}

// RecordAnnotation forwards the delayed-pick annotation as V1 DelayedPickComplete.
func (a *clientAttemptBridge) RecordAnnotation(annotation string) {
	if annotation == estats.AnnotationDelayedPickComplete {
		a.h.HandleRPC(a.attemptCtx, &stats.DelayedPickComplete{})
	}
}

// RecordEnd emits the V1 client End.
func (a *clientAttemptBridge) RecordEnd(info *estats.AttemptEndInfo) {
	a.h.HandleRPC(a.attemptCtx, &stats.End{
		Client:    true,
		BeginTime: a.beginTime,
		EndTime:   time.Now(),
		Trailer:   a.trailer,
		Error:     info.Error,
	})
}

// serverCallBridge is the server-scoped adapter. The server tracer plays the
// role of both call and attempt tracer, matching V1's single server RPC scope.
type serverCallBridge struct {
	estats.UnimplementedServerCallTracer
	h    stats.Handler
	info *estats.ServerCallInfo

	// incoming is captured from RecordIncomingHeaders and replayed as the V1
	// InHeader inside FilterContext, after TagRPC, to preserve V1's
	// TagRPC -> InHeader -> Begin order (V2 delivers incoming headers before
	// FilterContext).
	incoming *estats.HeadersInfo
	// callCtx is the TagRPC'd context, established in FilterContext and handed
	// to every later HandleRPC for this call.
	callCtx   context.Context
	beginTime time.Time
}

// RecordIncomingHeaders captures the request headers; the V1 InHeader is not
// emitted until FilterContext has produced the tagged context.
func (s *serverCallBridge) RecordIncomingHeaders(info *estats.HeadersInfo) {
	s.incoming = info
}

// FilterContext calls TagRPC, then emits InHeader and Begin against the tagged
// context, reproducing the V1 server order. The tagged context is returned so
// the values the handler set flow into the served call, and is held for every
// later event.
func (s *serverCallBridge) FilterContext(ctx context.Context) context.Context {
	ctx = s.h.TagRPC(ctx, &stats.RPCTagInfo{FullMethodName: s.info.Method})
	s.callCtx = ctx

	var remote, local net.Addr
	var compression string
	var wireLength int
	header := s.info.Headers
	if s.incoming != nil {
		remote, local = addrs(s.incoming.Peer)
		compression = s.incoming.Compression
		wireLength = s.incoming.WireLength
		if s.incoming.Headers != nil {
			header = s.incoming.Headers
		}
	} else {
		remote, local = addrs(s.info.Peer)
	}
	s.h.HandleRPC(ctx, &stats.InHeader{
		FullMethod:  s.info.Method,
		RemoteAddr:  remote,
		LocalAddr:   local,
		Compression: compression,
		WireLength:  wireLength,
		Header:      header,
	})
	s.beginTime = time.Now()
	s.h.HandleRPC(ctx, &stats.Begin{
		BeginTime: s.beginTime,
	})
	return ctx
}

// RecordOutgoingHeaders emits the V1 server OutHeader.
func (s *serverCallBridge) RecordOutgoingHeaders(info *estats.HeadersInfo) {
	s.h.HandleRPC(s.callCtx, &stats.OutHeader{
		Compression: info.Compression,
		Header:      info.Headers,
	})
}

// RecordOutgoingTrailers emits the V1 server OutTrailer.
func (s *serverCallBridge) RecordOutgoingTrailers(info *estats.TrailersInfo) {
	s.h.HandleRPC(s.callCtx, &stats.OutTrailer{
		Trailer: info.Trailers,
	})
}

// RecordIncomingMessage emits the V1 server InPayload (sizes only; Payload nil).
func (s *serverCallBridge) RecordIncomingMessage(info *estats.MessageInfo) {
	s.h.HandleRPC(s.callCtx, &stats.InPayload{
		Length:           info.Length,
		CompressedLength: info.CompressedLength,
		WireLength:       info.WireLength,
		RecvTime:         time.Now(),
	})
}

// RecordOutgoingMessage emits the V1 server OutPayload (sizes only; Payload nil).
func (s *serverCallBridge) RecordOutgoingMessage(info *estats.MessageInfo) {
	s.h.HandleRPC(s.callCtx, &stats.OutPayload{
		Length:           info.Length,
		CompressedLength: info.CompressedLength,
		WireLength:       info.WireLength,
		SentTime:         time.Now(),
	})
}

// RecordEnd emits the V1 server End.
func (s *serverCallBridge) RecordEnd(info *estats.CallEndInfo) {
	s.h.HandleRPC(s.callCtx, &stats.End{
		BeginTime: s.beginTime,
		EndTime:   time.Now(),
		Error:     info.Error,
	})
}

// MutateOutgoingHeaders and MutateOutgoingTrailers are no-ops: a V1 server
// stats.Handler has no method by which to inject outgoing metadata (the V1
// server injection idioms are grpc.SetHeader/SetTrailer inside the application
// handler, and interceptors - none of which is the stats handler). The client
// side differs because V1 client injection genuinely rides the TagRPC context,
// which MutateOutgoingHeaders on the client attempt reproduces.

// addrs returns the remote and local addresses of p, tolerating a nil peer.
func addrs(p *peer.Peer) (remote, local net.Addr) {
	if p == nil {
		return nil, nil
	}
	return p.Addr, p.LocalAddr
}
