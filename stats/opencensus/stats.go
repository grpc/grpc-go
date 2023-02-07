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
	"time"

	ocstats "go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

var logger = grpclog.Component("opencensus-instrumentation")

var canonicalString = internal.CanonicalString.(func(codes.Code) string)

type rpcDataKey struct{}

func setRPCData(ctx context.Context, d *rpcData) context.Context {
	return context.WithValue(ctx, rpcDataKey{}, d)
}

var (
	// bounds separate variable for testing purposes.
	bytesDistributionBounds  = []float64{1024, 2048, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824, 4294967296}
	bytesDistribution        = view.Distribution(bytesDistributionBounds...)
	millisecondsDistribution = view.Distribution(0.01, 0.05, 0.1, 0.3, 0.6, 0.8, 1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000, 20000, 50000, 100000)
	countDistributionBounds  = []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}
	countDistribution        = view.Distribution(countDistributionBounds...)
)

func removeLeadingSlash(mn string) string {
	return strings.TrimLeft(mn, "/")
}

// rpcData is data about the rpc attempt client side, and the overall rpc server
// side.
type rpcData struct {
	// access these counts atomically for hedging in the future
	// number of messages sent from side (client || server)
	sentMsgs int64
	// number of bytes sent (within each message) from side (client || server)
	sentBytes int64
	// number of messages received on side (client || server)
	recvMsgs int64
	// number of bytes received (within each message) received on side (client
	// || server)
	recvBytes int64

	startTime time.Time
	method    string
}

// statsTagRPC creates a recording object to derive measurements from in the
// context, scoping the recordings to per RPC Attempt client side (scope of the
// context). It also populates the gRPC Metadata within the context with any
// opencensus specific tags set by the application in the context, binary
// encoded to send across the wire.
func (csh *clientStatsHandler) statsTagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	d := &rpcData{
		startTime: time.Now(),
		method:    info.FullMethodName,
	}
	// Populate gRPC Metadata with OpenCensus tag map if set by application.
	if tm := tag.FromContext(ctx); tm != nil {
		ctx = stats.SetTags(ctx, tag.Encode(tm))
	}
	return setRPCData(ctx, d)
}

// statsTagRPC creates a recording object to derive measurements from in the
// context, scoping the recordings to per RPC server side (scope of the
// context). It also deserializes the opencensus tags set in the context's gRPC
// Metadata, and adds a server method tag to the opencensus tags.
func (ssh *serverStatsHandler) statsTagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	d := &rpcData{
		startTime: time.Now(),
		method:    info.FullMethodName,
	}
	if tagsBin := stats.Tags(ctx); tagsBin != nil {
		if tags, err := tag.Decode(tagsBin); err == nil {
			ctx = tag.NewContext(ctx, tags)
		}
	}
	// We can ignore the error here because in the error case, the context
	// passed in is returned. If the call errors, the server side application
	// layer won't get this key server method information in the tag map, but
	// this instrumentation code will function as normal.
	ctx, _ = tag.New(ctx, tag.Upsert(keyServerMethod, removeLeadingSlash(info.FullMethodName)))
	return setRPCData(ctx, d)
}

func recordRPCData(ctx context.Context, s stats.RPCStats) {
	d, ok := ctx.Value(rpcDataKey{}).(*rpcData)
	if !ok {
		// Shouldn't happen, as gRPC calls TagRPC which populates the rpcData in
		// context.
		return
	}
	switch st := s.(type) {
	case *stats.InHeader, *stats.OutHeader, *stats.InTrailer, *stats.OutTrailer:
		// Headers and Trailers are not relevant to the measures, as the
		// measures concern number of messages and bytes for messages. This
		// aligns with flow control.
	case *stats.Begin:
		recordDataBegin(ctx, d, st)
	case *stats.OutPayload:
		recordDataOutPayload(d, st)
	case *stats.InPayload:
		recordDataInPayload(d, st)
	case *stats.End:
		recordDataEnd(ctx, d, st)
	default:
		// Shouldn't happen. gRPC calls into stats handler, and will never not
		// be one of the types above.
		logger.Errorf("Received unexpected stats type (%T) with data: %v", s, s)
	}
}

// recordDataBegin takes a measurement related to the RPC beginning,
// client/server started RPCs dependent on the caller.
func recordDataBegin(ctx context.Context, d *rpcData, b *stats.Begin) {
	if b.Client {
		ocstats.RecordWithOptions(ctx,
			ocstats.WithTags(tag.Upsert(keyClientMethod, removeLeadingSlash(d.method))),
			ocstats.WithMeasurements(clientStartedRPCs.M(1)))
		return
	}
	ocstats.RecordWithOptions(ctx,
		ocstats.WithTags(tag.Upsert(keyServerMethod, removeLeadingSlash(d.method))),
		ocstats.WithMeasurements(serverStartedRPCs.M(1)))
}

// recordDataOutPayload records the length in bytes of outgoing messages and
// increases total count of sent messages both stored in the RPCs (attempt on
// client side) context for use in taking measurements at RPC end.
func recordDataOutPayload(d *rpcData, op *stats.OutPayload) {
	atomic.AddInt64(&d.sentMsgs, 1)
	atomic.AddInt64(&d.sentBytes, int64(op.Length))
}

// recordDataInPayload records the length in bytes of incoming messages and
// increases total count of sent messages both stored in the RPCs (attempt on
// client side) context for use in taking measurements at RPC end.
func recordDataInPayload(d *rpcData, ip *stats.InPayload) {
	atomic.AddInt64(&d.recvMsgs, 1)
	atomic.AddInt64(&d.recvBytes, int64(ip.Length))
}

// recordDataEnd takes per RPC measurements derived from information derived
// from the lifetime of the RPC (RPC attempt client side).
func recordDataEnd(ctx context.Context, d *rpcData, e *stats.End) {
	// latency bounds for distribution data (speced millisecond bounds) have
	// fractions, thus need a float.
	latency := float64(time.Since(d.startTime)) / float64(time.Millisecond)
	var st string
	if e.Error != nil {
		s, _ := status.FromError(e.Error)
		st = canonicalString(s.Code())
	} else {
		st = "OK"
	}

	// TODO: Attach trace data through attachments?!?!

	if e.Client {
		ocstats.RecordWithOptions(ctx,
			ocstats.WithTags(
				tag.Upsert(keyClientMethod, removeLeadingSlash(d.method)),
				tag.Upsert(keyClientStatus, st)),
			ocstats.WithMeasurements(
				clientSentBytesPerRPC.M(atomic.LoadInt64(&d.sentBytes)),
				clientSentMessagesPerRPC.M(atomic.LoadInt64(&d.sentMsgs)),
				clientReceivedMessagesPerRPC.M(atomic.LoadInt64(&d.recvMsgs)),
				clientReceivedBytesPerRPC.M(atomic.LoadInt64(&d.recvBytes)),
				clientRoundtripLatency.M(latency),
				clientServerLatency.M(latency),
			))
		return
	}
	ocstats.RecordWithOptions(ctx,
		ocstats.WithTags(
			tag.Upsert(keyServerMethod, removeLeadingSlash(d.method)),
			tag.Upsert(keyServerStatus, st),
		),
		ocstats.WithMeasurements(
			serverSentBytesPerRPC.M(atomic.LoadInt64(&d.sentBytes)),
			serverSentMessagesPerRPC.M(atomic.LoadInt64(&d.sentMsgs)),
			serverReceivedMessagesPerRPC.M(atomic.LoadInt64(&d.recvMsgs)),
			serverReceivedBytesPerRPC.M(atomic.LoadInt64(&d.recvBytes)),
			serverLatency.M(latency)))
}
