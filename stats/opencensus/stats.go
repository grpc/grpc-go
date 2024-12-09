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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

var logger = grpclog.Component("opencensus-instrumentation")

var canonicalString = internal.CanonicalString.(func(codes.Code) string)

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

// metricsInfo is data used for recording metrics about the rpc attempt client
// side, and the overall rpc server side.
type metricsInfo struct {
	// access these counts atomically for hedging in the future
	// number of messages sent from side (client || server)
	sentMsgs int64
	// number of bytes sent (within each message) from side (client || server)
	sentBytes int64
	// number of bytes after compression (within each message) from side (client || server)
	sentCompressedBytes int64
	// number of messages received on side (client || server)
	recvMsgs int64
	// number of bytes received (within each message) received on side (client
	// || server)
	recvBytes int64
	// number of compressed bytes received (within each message) received on
	// side (client || server)
	recvCompressedBytes int64

	startTime time.Time
	method    string
}

// statsTagRPC creates a recording object to derive measurements from in the
// context, scoping the recordings to per RPC Attempt client side (scope of the
// context). It also populates the gRPC Metadata within the context with any
// opencensus specific tags set by the application in the context, binary
// encoded to send across the wire.
func (csh *clientStatsHandler) statsTagRPC(ctx context.Context, info *stats.RPCTagInfo) (context.Context, *metricsInfo) {
	mi := &metricsInfo{
		startTime: time.Now(),
		method:    info.FullMethodName,
	}

	// Populate gRPC Metadata with OpenCensus tag map if set by application.
	if tm := tag.FromContext(ctx); tm != nil {
		ctx = metadata.AppendToOutgoingContext(ctx, "grpc-tags-bin", string(tag.Encode(tm)))
	}
	return ctx, mi
}

// statsTagRPC creates a recording object to derive measurements from in the
// context, scoping the recordings to per RPC server side (scope of the
// context). It also deserializes the opencensus tags set in the context's gRPC
// Metadata, and adds a server method tag to the opencensus tags. If multiple
// tags exist, it adds the last one.
func (ssh *serverStatsHandler) statsTagRPC(ctx context.Context, info *stats.RPCTagInfo) (context.Context, *metricsInfo) {
	mi := &metricsInfo{
		startTime: time.Now(),
		method:    info.FullMethodName,
	}

	if tgValues := metadata.ValueFromIncomingContext(ctx, "grpc-tags-bin"); len(tgValues) > 0 {
		tagsBin := []byte(tgValues[len(tgValues)-1])
		if tags, err := tag.Decode(tagsBin); err == nil {
			ctx = tag.NewContext(ctx, tags)
		}
	}

	// We can ignore the error here because in the error case, the context
	// passed in is returned. If the call errors, the server side application
	// layer won't get this key server method information in the tag map, but
	// this instrumentation code will function as normal.
	ctx, _ = tag.New(ctx, tag.Upsert(keyServerMethod, removeLeadingSlash(info.FullMethodName)))
	return ctx, mi
}

func recordRPCData(ctx context.Context, s stats.RPCStats, mi *metricsInfo) {
	if mi == nil {
		// Shouldn't happen, as gRPC calls TagRPC which populates the metricsInfo in
		// context.
		logger.Error("ctx passed into stats handler metrics event handling has no metrics data present")
		return
	}
	switch st := s.(type) {
	case *stats.InHeader, *stats.OutHeader, *stats.InTrailer, *stats.OutTrailer, *stats.PickerUpdated:
		// Headers, Trailers, and picker updates are not relevant to the measures,
		// as the measures concern number of messages and bytes for messages. This
		// aligns with flow control.
	case *stats.Begin:
		recordDataBegin(ctx, mi, st)
	case *stats.OutPayload:
		recordDataOutPayload(mi, st)
	case *stats.InPayload:
		recordDataInPayload(mi, st)
	case *stats.End:
		recordDataEnd(ctx, mi, st)
	default:
		// Shouldn't happen. gRPC calls into stats handler, and will never not
		// be one of the types above.
		logger.Errorf("Received unexpected stats type (%T) with data: %v", s, s)
	}
}

// recordDataBegin takes a measurement related to the RPC beginning,
// client/server started RPCs dependent on the caller.
func recordDataBegin(ctx context.Context, mi *metricsInfo, b *stats.Begin) {
	if b.Client {
		ocstats.RecordWithOptions(ctx,
			ocstats.WithTags(tag.Upsert(keyClientMethod, removeLeadingSlash(mi.method))),
			ocstats.WithMeasurements(clientStartedRPCs.M(1)))
		return
	}
	ocstats.RecordWithOptions(ctx,
		ocstats.WithTags(tag.Upsert(keyServerMethod, removeLeadingSlash(mi.method))),
		ocstats.WithMeasurements(serverStartedRPCs.M(1)))
}

// recordDataOutPayload records the length in bytes of outgoing messages and
// increases total count of sent messages both stored in the RPCs (attempt on
// client side) context for use in taking measurements at RPC end.
func recordDataOutPayload(mi *metricsInfo, op *stats.OutPayload) {
	atomic.AddInt64(&mi.sentMsgs, 1)
	atomic.AddInt64(&mi.sentBytes, int64(op.Length))
	atomic.AddInt64(&mi.sentCompressedBytes, int64(op.CompressedLength))
}

// recordDataInPayload records the length in bytes of incoming messages and
// increases total count of sent messages both stored in the RPCs (attempt on
// client side) context for use in taking measurements at RPC end.
func recordDataInPayload(mi *metricsInfo, ip *stats.InPayload) {
	atomic.AddInt64(&mi.recvMsgs, 1)
	atomic.AddInt64(&mi.recvBytes, int64(ip.Length))
	atomic.AddInt64(&mi.recvCompressedBytes, int64(ip.CompressedLength))
}

// recordDataEnd takes per RPC measurements derived from information derived
// from the lifetime of the RPC (RPC attempt client side).
func recordDataEnd(ctx context.Context, mi *metricsInfo, e *stats.End) {
	// latency bounds for distribution data (speced millisecond bounds) have
	// fractions, thus need a float.
	latency := float64(time.Since(mi.startTime)) / float64(time.Millisecond)
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
				tag.Upsert(keyClientMethod, removeLeadingSlash(mi.method)),
				tag.Upsert(keyClientStatus, st)),
			ocstats.WithMeasurements(
				clientSentBytesPerRPC.M(atomic.LoadInt64(&mi.sentBytes)),
				clientSentCompressedBytesPerRPC.M(atomic.LoadInt64(&mi.sentCompressedBytes)),
				clientSentMessagesPerRPC.M(atomic.LoadInt64(&mi.sentMsgs)),
				clientReceivedMessagesPerRPC.M(atomic.LoadInt64(&mi.recvMsgs)),
				clientReceivedBytesPerRPC.M(atomic.LoadInt64(&mi.recvBytes)),
				clientReceivedCompressedBytesPerRPC.M(atomic.LoadInt64(&mi.recvCompressedBytes)),
				clientRoundtripLatency.M(latency),
				clientServerLatency.M(latency),
			))
		return
	}
	ocstats.RecordWithOptions(ctx,
		ocstats.WithTags(
			tag.Upsert(keyServerMethod, removeLeadingSlash(mi.method)),
			tag.Upsert(keyServerStatus, st),
		),
		ocstats.WithMeasurements(
			serverSentBytesPerRPC.M(atomic.LoadInt64(&mi.sentBytes)),
			serverSentCompressedBytesPerRPC.M(atomic.LoadInt64(&mi.sentCompressedBytes)),
			serverSentMessagesPerRPC.M(atomic.LoadInt64(&mi.sentMsgs)),
			serverReceivedMessagesPerRPC.M(atomic.LoadInt64(&mi.recvMsgs)),
			serverReceivedBytesPerRPC.M(atomic.LoadInt64(&mi.recvBytes)),
			serverReceivedCompressedBytesPerRPC.M(atomic.LoadInt64(&mi.recvCompressedBytes)),
			serverLatency.M(latency)))
}
