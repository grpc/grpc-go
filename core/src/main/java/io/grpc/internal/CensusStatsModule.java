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

import static com.google.common.base.MoreObjects.firstNonNull;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import com.google.instrumentation.stats.MeasurementMap;
import com.google.instrumentation.stats.RpcConstants;
import com.google.instrumentation.stats.StatsContext;
import com.google.instrumentation.stats.StatsContextFactory;
import com.google.instrumentation.stats.TagValue;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientStreamTracer;
import io.grpc.Context;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import io.grpc.ForwardingClientCallListener.SimpleForwardingClientCallListener;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerStreamTracer;
import io.grpc.Status;
import io.grpc.StreamTracer;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Provides factories for {@link StreamTracer} that records stats to Census.
 *
 * <p>On the client-side, a factory is created for each call, because ClientCall starts earlier than
 * the ClientStream, and in some cases may even not create a ClientStream at all.  Therefore, it's
 * the factory that reports the summary to Census.
 *
 * <p>On the server-side, there is only one ServerStream per each ServerCall, and ServerStream
 * starts earlier than the ServerCall.  Therefore, only one tracer is created per stream/call and
 * it's the tracer that reports the summary to Census.
 */
final class CensusStatsModule {
  private static final Logger logger = Logger.getLogger(CensusStatsModule.class.getName());
  private static final double NANOS_PER_MILLI = TimeUnit.MILLISECONDS.toNanos(1);
  private static final ClientTracer BLANK_CLIENT_TRACER = new ClientTracer();

  // TODO(zhangkun): point to Census's StatsContext key once they've made it public
  @VisibleForTesting
  static final Context.Key<StatsContext> STATS_CONTEXT_KEY =
      Context.key("io.grpc.internal.StatsContext"); 

  private final StatsContextFactory statsCtxFactory;
  private final Supplier<Stopwatch> stopwatchSupplier;
  @VisibleForTesting
  final Metadata.Key<StatsContext> statsHeader;
  private final StatsClientInterceptor clientInterceptor = new StatsClientInterceptor();
  private final ServerTracerFactory serverTracerFactory = new ServerTracerFactory();
  private final boolean propagateTags;

  CensusStatsModule(
      final StatsContextFactory statsCtxFactory, Supplier<Stopwatch> stopwatchSupplier,
      boolean propagateTags) {
    this.statsCtxFactory = checkNotNull(statsCtxFactory, "statsCtxFactory");
    this.stopwatchSupplier = checkNotNull(stopwatchSupplier, "stopwatchSupplier");
    this.propagateTags = propagateTags;
    this.statsHeader =
        Metadata.Key.of("grpc-tags-bin", new Metadata.BinaryMarshaller<StatsContext>() {
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
              } catch (Exception e) {
                logger.log(Level.FINE, "Failed to parse stats header", e);
                return statsCtxFactory.getDefault();
              }
            }
          });
  }

  /**
   * Creates a {@link ClientCallTracer} for a new call.
   */
  @VisibleForTesting
  ClientCallTracer newClientCallTracer(StatsContext parentCtx, String fullMethodName) {
    return new ClientCallTracer(parentCtx, fullMethodName);
  }

  /**
   * Returns the server tracer factory.
   */
  ServerStreamTracer.Factory getServerTracerFactory() {
    return serverTracerFactory;
  }

  /**
   * Returns the client interceptor that facilitates Census-based stats reporting.
   */
  ClientInterceptor getClientInterceptor() {
    return clientInterceptor;
  }

  private static final class ClientTracer extends ClientStreamTracer {
    final AtomicLong outboundWireSize = new AtomicLong();
    final AtomicLong inboundWireSize = new AtomicLong();
    final AtomicLong outboundUncompressedSize = new AtomicLong();
    final AtomicLong inboundUncompressedSize = new AtomicLong();

    @Override
    public void outboundWireSize(long bytes) {
      outboundWireSize.addAndGet(bytes);
    }

    @Override
    public void inboundWireSize(long bytes) {
      inboundWireSize.addAndGet(bytes);
    }

    @Override
    public void outboundUncompressedSize(long bytes) {
      outboundUncompressedSize.addAndGet(bytes);
    }

    @Override
    public void inboundUncompressedSize(long bytes) {
      inboundUncompressedSize.addAndGet(bytes);
    }
  }

  @VisibleForTesting
  final class ClientCallTracer extends ClientStreamTracer.Factory {

    private final String fullMethodName;
    private final Stopwatch stopwatch;
    private final AtomicReference<ClientTracer> streamTracer = new AtomicReference<ClientTracer>();
    private final AtomicBoolean callEnded = new AtomicBoolean(false);
    private final StatsContext parentCtx;

    ClientCallTracer(StatsContext parentCtx, String fullMethodName) {
      this.parentCtx = checkNotNull(parentCtx, "parentCtx");
      this.fullMethodName = checkNotNull(fullMethodName, "fullMethodName");
      this.stopwatch = stopwatchSupplier.get().start();
    }

    @Override
    public ClientStreamTracer newClientStreamTracer(Metadata headers) {
      ClientTracer tracer = new ClientTracer();
      // TODO(zhangkun83): Once retry or hedging is implemented, a ClientCall may start more than
      // one streams.  We will need to update this file to support them.
      checkState(streamTracer.compareAndSet(null, tracer),
          "Are you creating multiple streams per call? This class doesn't yet support this case.");
      if (propagateTags) {
        headers.discardAll(statsHeader);
        if (parentCtx != statsCtxFactory.getDefault()) {
          headers.put(statsHeader, parentCtx);
        }
      }
      return tracer;
    }

    /**
     * Record a finished call and mark the current time as the end time.
     *
     * <p>Can be called from any thread without synchronization.  Calling it the second time or more
     * is a no-op.
     */
    void callEnded(Status status) {
      if (!callEnded.compareAndSet(false, true)) {
        return;
      }
      stopwatch.stop();
      long roundtripNanos = stopwatch.elapsed(TimeUnit.NANOSECONDS);
      ClientTracer tracer = streamTracer.get();
      if (tracer == null) {
        tracer = BLANK_CLIENT_TRACER;
      }
      MeasurementMap.Builder builder = MeasurementMap.builder()
          // The metrics are in double
          .put(RpcConstants.RPC_CLIENT_ROUNDTRIP_LATENCY, roundtripNanos / NANOS_PER_MILLI)
          .put(RpcConstants.RPC_CLIENT_REQUEST_BYTES, tracer.outboundWireSize.get())
          .put(RpcConstants.RPC_CLIENT_RESPONSE_BYTES, tracer.inboundWireSize.get())
          .put(
              RpcConstants.RPC_CLIENT_UNCOMPRESSED_REQUEST_BYTES,
              tracer.outboundUncompressedSize.get())
          .put(
              RpcConstants.RPC_CLIENT_UNCOMPRESSED_RESPONSE_BYTES,
              tracer.inboundUncompressedSize.get());
      if (!status.isOk()) {
        builder.put(RpcConstants.RPC_CLIENT_ERROR_COUNT, 1.0);
      }
      parentCtx
          .with(
              RpcConstants.RPC_CLIENT_METHOD, TagValue.create(fullMethodName),
              RpcConstants.RPC_STATUS, TagValue.create(status.getCode().toString()))
          .record(builder.build());
    }
  }

  private final class ServerTracer extends ServerStreamTracer {
    private final String fullMethodName;
    @Nullable
    private final StatsContext parentCtx;
    private final AtomicBoolean streamClosed = new AtomicBoolean(false);
    private final Stopwatch stopwatch;
    private final AtomicLong outboundWireSize = new AtomicLong();
    private final AtomicLong inboundWireSize = new AtomicLong();
    private final AtomicLong outboundUncompressedSize = new AtomicLong();
    private final AtomicLong inboundUncompressedSize = new AtomicLong();

    ServerTracer(String fullMethodName, StatsContext parentCtx) {
      this.fullMethodName = checkNotNull(fullMethodName, "fullMethodName");
      this.parentCtx = checkNotNull(parentCtx, "parentCtx");
      this.stopwatch = stopwatchSupplier.get().start();
    }

    @Override
    public void outboundWireSize(long bytes) {
      outboundWireSize.addAndGet(bytes);
    }

    @Override
    public void inboundWireSize(long bytes) {
      inboundWireSize.addAndGet(bytes);
    }

    @Override
    public void outboundUncompressedSize(long bytes) {
      outboundUncompressedSize.addAndGet(bytes);
    }

    @Override
    public void inboundUncompressedSize(long bytes) {
      inboundUncompressedSize.addAndGet(bytes);
    }

    /**
     * Record a finished stream and mark the current time as the end time.
     *
     * <p>Can be called from any thread without synchronization.  Calling it the second time or more
     * is a no-op.
     */
    @Override
    public void streamClosed(Status status) {
      if (!streamClosed.compareAndSet(false, true)) {
        return;
      }
      stopwatch.stop();
      long elapsedTimeNanos = stopwatch.elapsed(TimeUnit.NANOSECONDS);
      MeasurementMap.Builder builder = MeasurementMap.builder()
          // The metrics are in double
          .put(RpcConstants.RPC_SERVER_SERVER_LATENCY, elapsedTimeNanos / NANOS_PER_MILLI)
          .put(RpcConstants.RPC_SERVER_RESPONSE_BYTES, outboundWireSize.get())
          .put(RpcConstants.RPC_SERVER_REQUEST_BYTES, inboundWireSize.get())
          .put(
              RpcConstants.RPC_SERVER_UNCOMPRESSED_RESPONSE_BYTES,
              outboundUncompressedSize.get())
          .put(
              RpcConstants.RPC_SERVER_UNCOMPRESSED_REQUEST_BYTES,
              inboundUncompressedSize.get());
      if (!status.isOk()) {
        builder.put(RpcConstants.RPC_SERVER_ERROR_COUNT, 1.0);
      }
      StatsContext ctx = firstNonNull(parentCtx, statsCtxFactory.getDefault());
      ctx
          .with(
              RpcConstants.RPC_SERVER_METHOD, TagValue.create(fullMethodName),
              RpcConstants.RPC_STATUS, TagValue.create(status.getCode().toString()))
          .record(builder.build());
    }

    @Override
    public <ReqT, RespT> Context filterContext(Context context) {
      if (parentCtx != statsCtxFactory.getDefault()) {
        return context.withValue(STATS_CONTEXT_KEY, parentCtx);
      }
      return context;
    }
  }

  private final class ServerTracerFactory extends ServerStreamTracer.Factory {
    @Override
    public ServerStreamTracer newServerStreamTracer(String fullMethodName, Metadata headers) {
      StatsContext parentCtx = headers.get(statsHeader);
      if (parentCtx == null) {
        parentCtx = statsCtxFactory.getDefault();
      }
      return new ServerTracer(fullMethodName, parentCtx);
    }
  }

  private class StatsClientInterceptor implements ClientInterceptor {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      // New RPCs on client-side inherit the stats context from the current Context.
      StatsContext parentCtx = STATS_CONTEXT_KEY.get();
      if (parentCtx == null) {
        parentCtx = statsCtxFactory.getDefault();
      }
      final ClientCallTracer tracerFactory =
          newClientCallTracer(parentCtx, method.getFullMethodName());
      ClientCall<ReqT, RespT> call =
          next.newCall(method, callOptions.withStreamTracerFactory(tracerFactory));
      return new SimpleForwardingClientCall<ReqT, RespT>(call) {
        @Override
        public void start(Listener<RespT> responseListener, Metadata headers) {
          delegate().start(
              new SimpleForwardingClientCallListener<RespT>(responseListener) {
                @Override
                public void onClose(Status status, Metadata trailers) {
                  tracerFactory.callEnded(status);
                  super.onClose(status, trailers);
                }
              },
              headers);
        }
      };
    }
  }
}
