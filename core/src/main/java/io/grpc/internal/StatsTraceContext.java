/*
 * Copyright 2016, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.internal;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import com.google.instrumentation.stats.MeasurementDescriptor;
import com.google.instrumentation.stats.MeasurementMap;
import com.google.instrumentation.stats.RpcConstants;
import com.google.instrumentation.stats.StatsContext;
import com.google.instrumentation.stats.StatsContextFactory;
import com.google.instrumentation.stats.TagKey;
import com.google.instrumentation.stats.TagValue;
import io.grpc.Metadata;
import io.grpc.Status;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

/**
 * The stats and tracing information for a call.
 *
 * <p>This class is not thread-safe, in the sense that the updates to each individual metric must be
 * serialized, while multiple threads can update different metrics without any sort of
 * synchronization.  For example, calls to {@link #wireBytesSent} must be synchronized, while {@link
 * #wireBytesReceived} and {@link #wireBytesSent} can be called concurrently.  {@link #callEnded}
 * can be called concurrently with itself and the other methods.
 */
public final class StatsTraceContext {
  public static final StatsTraceContext NOOP = StatsTraceContext.newClientContext(
      "noopservice/noopmethod", NoopStatsContextFactory.INSTANCE,
      GrpcUtil.STOPWATCH_SUPPLIER);

  private static final double NANOS_PER_MILLI = TimeUnit.MILLISECONDS.toNanos(1);
  private static final long UNSET_CLIENT_PENDING_NANOS = -1;

  private enum Side {
    CLIENT, SERVER
  }

  private final StatsContext statsCtx;
  private final Stopwatch stopwatch;
  private final Side side;
  private final Metadata.Key<StatsContext> statsHeader;
  private final AtomicLong clientPendingNanos = new AtomicLong(UNSET_CLIENT_PENDING_NANOS);
  private final AtomicLong wireBytesSent = new AtomicLong();
  private final AtomicLong wireBytesReceived = new AtomicLong();
  private final AtomicLong uncompressedBytesSent = new AtomicLong();
  private final AtomicLong uncompressedBytesReceived = new AtomicLong();
  private final AtomicBoolean callEnded = new AtomicBoolean(false);

  private StatsTraceContext(Side side, String fullMethodName, StatsContext parentCtx,
      Supplier<Stopwatch> stopwatchSupplier, Metadata.Key<StatsContext> statsHeader) {
    this.side = side;
    TagKey methodTagKey =
        side == Side.CLIENT ? RpcConstants.RPC_CLIENT_METHOD : RpcConstants.RPC_SERVER_METHOD;
    // TODO(carl-mastrangelo): maybe cache TagValue in MethodDescriptor
    this.statsCtx = parentCtx.with(methodTagKey, TagValue.create(fullMethodName));
    this.stopwatch = stopwatchSupplier.get().start();
    this.statsHeader = statsHeader;
  }

  /**
   * Creates a {@code StatsTraceContext} for an outgoing RPC, using the current StatsContext.
   *
   * <p>The current time is used as the start time of the RPC.
   */
  public static StatsTraceContext newClientContext(String methodName,
      StatsContextFactory statsFactory, Supplier<Stopwatch> stopwatchSupplier) {
    return new StatsTraceContext(Side.CLIENT, methodName,
        // TODO(zhangkun83): use the StatsContext out of the current Context
        statsFactory.getDefault(),
        stopwatchSupplier, createStatsHeader(statsFactory));
  }

  @VisibleForTesting
  static StatsTraceContext newClientContextForTesting(String methodName,
      StatsContextFactory statsFactory, StatsContext parent,
      Supplier<Stopwatch> stopwatchSupplier) {
    return new StatsTraceContext(Side.CLIENT, methodName, parent, stopwatchSupplier,
        createStatsHeader(statsFactory));
  }

  /**
   * Creates a {@code StatsTraceContext} for an incoming RPC, using the StatsContext deserialized
   * from the headers.
   *
   * <p>The current time is used as the start time of the RPC.
   */
  public static StatsTraceContext newServerContext(String methodName,
      StatsContextFactory statsFactory, Metadata headers,
      Supplier<Stopwatch> stopwatchSupplier) {
    Metadata.Key<StatsContext> statsHeader = createStatsHeader(statsFactory);
    StatsContext parentCtx = headers.get(statsHeader);
    if (parentCtx == null) {
      parentCtx = statsFactory.getDefault();
    }
    return new StatsTraceContext(Side.SERVER, methodName, parentCtx, stopwatchSupplier,
        statsHeader);
  }

  /**
   * Propagate the context to the outgoing headers.
   */
  void propagateToHeaders(Metadata headers) {
    headers.discardAll(statsHeader);
    headers.put(statsHeader, statsCtx);
  }

  Metadata.Key<StatsContext> getStatsHeader() {
    return statsHeader;
  }

  @VisibleForTesting
  StatsContext getStatsContext() {
    return statsCtx;
  }

  @VisibleForTesting
  static Metadata.Key<StatsContext> createStatsHeader(final StatsContextFactory statsCtxFactory) {
    return Metadata.Key.of("grpc-census-bin", new Metadata.BinaryMarshaller<StatsContext>() {
        @Override
        public byte[] toBytes(StatsContext context) {
          // TODO(carl-mastrangelo): currently we only make sure the correctness. We may need to
          // optimize out the allocation and copy in the future.
          ByteArrayOutputStream buffer = new ByteArrayOutputStream();
          try {
            context.serialize(buffer);
          } catch (IOException e) {
            throw new RuntimeException(e);
          }
          return buffer.toByteArray();
        }

        @Override
        public StatsContext parseBytes(byte[] serialized) {
          try {
            return statsCtxFactory.deserialize(new ByteArrayInputStream(serialized));
          } catch (IOException e) {
            throw new RuntimeException(e);
          }
        }
      });
  }

  /**
   * Record the outgoing number of payload bytes as on the wire.
   */
  void wireBytesSent(long bytes) {
    wireBytesSent.addAndGet(bytes);
  }

  /**
   * Record the incoming number of payload bytes as on the wire.
   */
  void wireBytesReceived(long bytes) {
    wireBytesReceived.addAndGet(bytes);
  }

  /**
   * Record the outgoing number of payload bytes in uncompressed form.
   *
   * <p>The time this method is called is unrelated to the actual time when those byte are sent.
   */
  void uncompressedBytesSent(long bytes) {
    uncompressedBytesSent.addAndGet(bytes);
  }

  /**
   * Record the incoming number of payload bytes in uncompressed form.
   *
   * <p>The time this method is called is unrelated to the actual time when those byte are received.
   */
  void uncompressedBytesReceived(long bytes) {
    uncompressedBytesReceived.addAndGet(bytes);
  }

  /**
   * Mark the time when the headers, which are the first bytes of the RPC, are sent from the client.
   * This is specific to transport implementation, thus should be called from transports.  Calling
   * it the second time or more is a no-op.
   */
  public void clientHeadersSent() {
    Preconditions.checkState(side == Side.CLIENT, "Must be called on client-side");
    if (clientPendingNanos.get() == UNSET_CLIENT_PENDING_NANOS) {
      clientPendingNanos.compareAndSet(
          UNSET_CLIENT_PENDING_NANOS, stopwatch.elapsed(TimeUnit.NANOSECONDS));
    }
  }

  /**
   * Record a finished all and mark the current time as the end time.
   *
   * <p>Can be called from any thread without synchronization.  Calling it the second time or more
   * is a no-op.
   */
  void callEnded(Status status) {
    if (!callEnded.compareAndSet(false, true)) {
      return;
    }
    stopwatch.stop();
    MeasurementDescriptor latencyMetric;
    MeasurementDescriptor wireBytesSentMetric;
    MeasurementDescriptor wireBytesReceivedMetric;
    MeasurementDescriptor uncompressedBytesSentMetric;
    MeasurementDescriptor uncompressedBytesReceivedMetric;
    MeasurementDescriptor errorCountMetric;
    if (side == Side.CLIENT) {
      latencyMetric = RpcConstants.RPC_CLIENT_ROUNDTRIP_LATENCY;
      wireBytesSentMetric = RpcConstants.RPC_CLIENT_REQUEST_BYTES;
      wireBytesReceivedMetric = RpcConstants.RPC_CLIENT_RESPONSE_BYTES;
      uncompressedBytesSentMetric = RpcConstants.RPC_CLIENT_UNCOMPRESSED_REQUEST_BYTES;
      uncompressedBytesReceivedMetric = RpcConstants.RPC_CLIENT_UNCOMPRESSED_RESPONSE_BYTES;
      errorCountMetric = RpcConstants.RPC_CLIENT_ERROR_COUNT;
    } else {
      latencyMetric = RpcConstants.RPC_SERVER_SERVER_LATENCY;
      wireBytesSentMetric = RpcConstants.RPC_SERVER_RESPONSE_BYTES;
      wireBytesReceivedMetric = RpcConstants.RPC_SERVER_REQUEST_BYTES;
      uncompressedBytesSentMetric = RpcConstants.RPC_SERVER_UNCOMPRESSED_RESPONSE_BYTES;
      uncompressedBytesReceivedMetric = RpcConstants.RPC_SERVER_UNCOMPRESSED_REQUEST_BYTES;
      errorCountMetric = RpcConstants.RPC_SERVER_ERROR_COUNT;
    }
    long roundtripNanos = stopwatch.elapsed(TimeUnit.NANOSECONDS);
    MeasurementMap.Builder builder = MeasurementMap.builder()
        .put(latencyMetric, roundtripNanos / NANOS_PER_MILLI)  // in double
        .put(wireBytesSentMetric, wireBytesSent.get())
        .put(wireBytesReceivedMetric, wireBytesReceived.get())
        .put(uncompressedBytesSentMetric, uncompressedBytesSent.get())
        .put(uncompressedBytesReceivedMetric, uncompressedBytesReceived.get());
    if (!status.isOk()) {
      builder.put(errorCountMetric, 1.0);
    }
    if (side == Side.CLIENT) {
      long localClientPendingNanos = clientPendingNanos.get();
      if (localClientPendingNanos != UNSET_CLIENT_PENDING_NANOS) {
        builder.put(
            RpcConstants.RPC_CLIENT_SERVER_ELAPSED_TIME,
            (roundtripNanos - localClientPendingNanos) / NANOS_PER_MILLI);  // in double
      }
    }
    statsCtx.with(RpcConstants.RPC_STATUS, TagValue.create(status.getCode().toString()))
        .record(builder.build());
  }
}
