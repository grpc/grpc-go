/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.opencensus.tags.unsafe.ContextUtils.TAG_CONTEXT_KEY;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
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
import io.opencensus.contrib.grpc.metrics.RpcMeasureConstants;
import io.opencensus.stats.MeasureMap;
import io.opencensus.stats.Stats;
import io.opencensus.stats.StatsRecorder;
import io.opencensus.tags.TagContext;
import io.opencensus.tags.TagValue;
import io.opencensus.tags.Tagger;
import io.opencensus.tags.Tags;
import io.opencensus.tags.propagation.TagContextBinarySerializer;
import io.opencensus.tags.propagation.TagContextSerializationException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicIntegerFieldUpdater;
import java.util.concurrent.atomic.AtomicLongFieldUpdater;
import java.util.concurrent.atomic.AtomicReferenceFieldUpdater;
import java.util.logging.Level;
import java.util.logging.Logger;

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
public final class CensusStatsModule {
  private static final Logger logger = Logger.getLogger(CensusStatsModule.class.getName());
  private static final double NANOS_PER_MILLI = TimeUnit.MILLISECONDS.toNanos(1);
  private static final ClientTracer BLANK_CLIENT_TRACER = new ClientTracer();

  private final Tagger tagger;
  private final StatsRecorder statsRecorder;
  private final Supplier<Stopwatch> stopwatchSupplier;
  @VisibleForTesting
  final Metadata.Key<TagContext> statsHeader;
  private final boolean propagateTags;

  /**
   * Creates a {@link CensusStatsModule} with the default OpenCensus implementation.
   */
  CensusStatsModule(Supplier<Stopwatch> stopwatchSupplier, boolean propagateTags) {
    this(
        Tags.getTagger(),
        Tags.getTagPropagationComponent().getBinarySerializer(),
        Stats.getStatsRecorder(),
        stopwatchSupplier,
        propagateTags);
  }

  /**
   * Creates a {@link CensusStatsModule} with the given OpenCensus implementation.
   */
  public CensusStatsModule(
      final Tagger tagger,
      final TagContextBinarySerializer tagCtxSerializer,
      StatsRecorder statsRecorder, Supplier<Stopwatch> stopwatchSupplier,
      boolean propagateTags) {
    this.tagger = checkNotNull(tagger, "tagger");
    this.statsRecorder = checkNotNull(statsRecorder, "statsRecorder");
    checkNotNull(tagCtxSerializer, "tagCtxSerializer");
    this.stopwatchSupplier = checkNotNull(stopwatchSupplier, "stopwatchSupplier");
    this.propagateTags = propagateTags;
    this.statsHeader =
        Metadata.Key.of("grpc-tags-bin", new Metadata.BinaryMarshaller<TagContext>() {
            @Override
            public byte[] toBytes(TagContext context) {
              // TODO(carl-mastrangelo): currently we only make sure the correctness. We may need to
              // optimize out the allocation and copy in the future.
              try {
                return tagCtxSerializer.toByteArray(context);
              } catch (TagContextSerializationException e) {
                throw new RuntimeException(e);
              }
            }

            @Override
            public TagContext parseBytes(byte[] serialized) {
              try {
                return tagCtxSerializer.fromByteArray(serialized);
              } catch (Exception e) {
                logger.log(Level.FINE, "Failed to parse stats header", e);
                return tagger.empty();
              }
            }
          });
  }

  /**
   * Creates a {@link ClientCallTracer} for a new call.
   */
  @VisibleForTesting
  ClientCallTracer newClientCallTracer(
      TagContext parentCtx, String fullMethodName,
      boolean recordStartedRpcs, boolean recordFinishedRpcs) {
    return new ClientCallTracer(
        this, parentCtx, fullMethodName, recordStartedRpcs, recordFinishedRpcs);
  }

  /**
   * Returns the server tracer factory.
   */
  ServerStreamTracer.Factory getServerTracerFactory(
      boolean recordStartedRpcs, boolean recordFinishedRpcs) {
    return new ServerTracerFactory(recordStartedRpcs, recordFinishedRpcs);
  }

  /**
   * Returns the client interceptor that facilitates Census-based stats reporting.
   */
  ClientInterceptor getClientInterceptor(boolean recordStartedRpcs, boolean recordFinishedRpcs) {
    return new StatsClientInterceptor(recordStartedRpcs, recordFinishedRpcs);
  }

  private static final class ClientTracer extends ClientStreamTracer {

    private static final AtomicLongFieldUpdater<ClientTracer> outboundMessageCountUpdater =
        AtomicLongFieldUpdater.newUpdater(ClientTracer.class, "outboundMessageCount");
    private static final AtomicLongFieldUpdater<ClientTracer> inboundMessageCountUpdater =
        AtomicLongFieldUpdater.newUpdater(ClientTracer.class, "inboundMessageCount");
    private static final AtomicLongFieldUpdater<ClientTracer> outboundWireSizeUpdater =
        AtomicLongFieldUpdater.newUpdater(ClientTracer.class, "outboundWireSize");
    private static final AtomicLongFieldUpdater<ClientTracer> inboundWireSizeUpdater =
        AtomicLongFieldUpdater.newUpdater(ClientTracer.class, "inboundWireSize");
    private static final AtomicLongFieldUpdater<ClientTracer> outboundUncompressedSizeUpdater =
        AtomicLongFieldUpdater.newUpdater(ClientTracer.class, "outboundUncompressedSize");
    private static final AtomicLongFieldUpdater<ClientTracer> inboundUncompressedSizeUpdater =
        AtomicLongFieldUpdater.newUpdater(ClientTracer.class, "inboundUncompressedSize");

    volatile long outboundMessageCount;
    volatile long inboundMessageCount;
    volatile long outboundWireSize;
    volatile long inboundWireSize;
    volatile long outboundUncompressedSize;
    volatile long inboundUncompressedSize;

    @Override
    public void outboundWireSize(long bytes) {
      outboundWireSizeUpdater.getAndAdd(this, bytes);
    }

    @Override
    public void inboundWireSize(long bytes) {
      inboundWireSizeUpdater.getAndAdd(this, bytes);
    }

    @Override
    public void outboundUncompressedSize(long bytes) {
      outboundUncompressedSizeUpdater.getAndAdd(this, bytes);
    }

    @Override
    public void inboundUncompressedSize(long bytes) {
      inboundUncompressedSizeUpdater.getAndAdd(this, bytes);
    }

    @Override
    public void inboundMessage(int seqNo) {
      inboundMessageCountUpdater.getAndIncrement(this);
    }

    @Override
    public void outboundMessage(int seqNo) {
      outboundMessageCountUpdater.getAndIncrement(this);
    }
  }



  @VisibleForTesting
  static final class ClientCallTracer extends ClientStreamTracer.Factory {
    private static final AtomicReferenceFieldUpdater<ClientCallTracer, ClientTracer>
        streamTracerUpdater =
            AtomicReferenceFieldUpdater.newUpdater(
                ClientCallTracer.class, ClientTracer.class, "streamTracer");
    private static final AtomicIntegerFieldUpdater<ClientCallTracer> callEndedUpdater =
        AtomicIntegerFieldUpdater.newUpdater(ClientCallTracer.class, "callEnded");

    private final CensusStatsModule module;
    private final String fullMethodName;
    private final Stopwatch stopwatch;
    private volatile ClientTracer streamTracer;
    private volatile int callEnded;
    private final TagContext parentCtx;
    private final TagContext startCtx;
    private final boolean recordFinishedRpcs;

    ClientCallTracer(
        CensusStatsModule module,
        TagContext parentCtx,
        String fullMethodName,
        boolean recordStartedRpcs,
        boolean recordFinishedRpcs) {
      this.module = module;
      this.fullMethodName = checkNotNull(fullMethodName, "fullMethodName");
      this.parentCtx = checkNotNull(parentCtx);
      this.startCtx =
          module.tagger.toBuilder(parentCtx)
          .put(RpcMeasureConstants.RPC_METHOD, TagValue.create(fullMethodName)).build();
      this.stopwatch = module.stopwatchSupplier.get().start();
      this.recordFinishedRpcs = recordFinishedRpcs;
      if (recordStartedRpcs) {
        module.statsRecorder.newMeasureMap().put(RpcMeasureConstants.RPC_CLIENT_STARTED_COUNT, 1)
            .record(startCtx);
      }
    }

    @Override
    public ClientStreamTracer newClientStreamTracer(CallOptions callOptions, Metadata headers) {
      ClientTracer tracer = new ClientTracer();
      // TODO(zhangkun83): Once retry or hedging is implemented, a ClientCall may start more than
      // one streams.  We will need to update this file to support them.
      checkState(
          streamTracerUpdater.compareAndSet(this, null, tracer),
          "Are you creating multiple streams per call? This class doesn't yet support this case.");
      if (module.propagateTags) {
        headers.discardAll(module.statsHeader);
        if (!module.tagger.empty().equals(parentCtx)) {
          headers.put(module.statsHeader, parentCtx);
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
      if (callEndedUpdater.getAndSet(this, 1) != 0) {
        return;
      }
      if (!recordFinishedRpcs) {
        return;
      }
      stopwatch.stop();
      long roundtripNanos = stopwatch.elapsed(TimeUnit.NANOSECONDS);
      ClientTracer tracer = streamTracer;
      if (tracer == null) {
        tracer = BLANK_CLIENT_TRACER;
      }
      MeasureMap measureMap = module.statsRecorder.newMeasureMap()
          .put(RpcMeasureConstants.RPC_CLIENT_FINISHED_COUNT, 1)
          // The latency is double value
          .put(RpcMeasureConstants.RPC_CLIENT_ROUNDTRIP_LATENCY, roundtripNanos / NANOS_PER_MILLI)
          .put(RpcMeasureConstants.RPC_CLIENT_REQUEST_COUNT, tracer.outboundMessageCount)
          .put(RpcMeasureConstants.RPC_CLIENT_RESPONSE_COUNT, tracer.inboundMessageCount)
          .put(RpcMeasureConstants.RPC_CLIENT_REQUEST_BYTES, tracer.outboundWireSize)
          .put(RpcMeasureConstants.RPC_CLIENT_RESPONSE_BYTES, tracer.inboundWireSize)
          .put(
              RpcMeasureConstants.RPC_CLIENT_UNCOMPRESSED_REQUEST_BYTES,
              tracer.outboundUncompressedSize)
          .put(
              RpcMeasureConstants.RPC_CLIENT_UNCOMPRESSED_RESPONSE_BYTES,
              tracer.inboundUncompressedSize);
      if (!status.isOk()) {
        measureMap.put(RpcMeasureConstants.RPC_CLIENT_ERROR_COUNT, 1);
      }
      measureMap.record(
          module
              .tagger
              .toBuilder(startCtx)
              .put(RpcMeasureConstants.RPC_STATUS, TagValue.create(status.getCode().toString()))
              .build());
    }
  }

  private static final class ServerTracer extends ServerStreamTracer {
    private static final AtomicIntegerFieldUpdater<ServerTracer> streamClosedUpdater =
        AtomicIntegerFieldUpdater.newUpdater(ServerTracer.class, "streamClosed");
    private static final AtomicLongFieldUpdater<ServerTracer> outboundMessageCountUpdater =
        AtomicLongFieldUpdater.newUpdater(ServerTracer.class, "outboundMessageCount");
    private static final AtomicLongFieldUpdater<ServerTracer> inboundMessageCountUpdater =
        AtomicLongFieldUpdater.newUpdater(ServerTracer.class, "inboundMessageCount");
    private static final AtomicLongFieldUpdater<ServerTracer> outboundWireSizeUpdater =
        AtomicLongFieldUpdater.newUpdater(ServerTracer.class, "outboundWireSize");
    private static final AtomicLongFieldUpdater<ServerTracer> inboundWireSizeUpdater =
        AtomicLongFieldUpdater.newUpdater(ServerTracer.class, "inboundWireSize");
    private static final AtomicLongFieldUpdater<ServerTracer> outboundUncompressedSizeUpdater =
        AtomicLongFieldUpdater.newUpdater(ServerTracer.class, "outboundUncompressedSize");
    private static final AtomicLongFieldUpdater<ServerTracer> inboundUncompressedSizeUpdater =
        AtomicLongFieldUpdater.newUpdater(ServerTracer.class, "inboundUncompressedSize");

    private final CensusStatsModule module;
    private final String fullMethodName;
    private final TagContext parentCtx;
    private volatile int streamClosed;
    private final Stopwatch stopwatch;
    private final Tagger tagger;
    private final boolean recordFinishedRpcs;
    private volatile long outboundMessageCount;
    private volatile long inboundMessageCount;
    private volatile long outboundWireSize;
    private volatile long inboundWireSize;
    private volatile long outboundUncompressedSize;
    private volatile long inboundUncompressedSize;

    ServerTracer(
        CensusStatsModule module,
        String fullMethodName,
        TagContext parentCtx,
        Supplier<Stopwatch> stopwatchSupplier,
        Tagger tagger,
        boolean recordStartedRpcs,
        boolean recordFinishedRpcs) {
      this.module = module;
      this.fullMethodName = checkNotNull(fullMethodName, "fullMethodName");
      this.parentCtx = checkNotNull(parentCtx, "parentCtx");
      this.stopwatch = stopwatchSupplier.get().start();
      this.tagger = tagger;
      this.recordFinishedRpcs = recordFinishedRpcs;
      if (recordStartedRpcs) {
        module.statsRecorder.newMeasureMap().put(RpcMeasureConstants.RPC_SERVER_STARTED_COUNT, 1)
            .record(parentCtx);
      }
    }

    @Override
    public void outboundWireSize(long bytes) {
      outboundWireSizeUpdater.getAndAdd(this, bytes);
    }

    @Override
    public void inboundWireSize(long bytes) {
      inboundWireSizeUpdater.getAndAdd(this, bytes);
    }

    @Override
    public void outboundUncompressedSize(long bytes) {
      outboundUncompressedSizeUpdater.getAndAdd(this, bytes);
    }

    @Override
    public void inboundUncompressedSize(long bytes) {
      inboundUncompressedSizeUpdater.getAndAdd(this, bytes);
    }

    @Override
    public void inboundMessage(int seqNo) {
      inboundMessageCountUpdater.getAndIncrement(this);
    }

    @Override
    public void outboundMessage(int seqNo) {
      outboundMessageCountUpdater.getAndIncrement(this);
    }

    /**
     * Record a finished stream and mark the current time as the end time.
     *
     * <p>Can be called from any thread without synchronization.  Calling it the second time or more
     * is a no-op.
     */
    @Override
    public void streamClosed(Status status) {
      if (streamClosedUpdater.getAndSet(this, 1) != 0) {
        return;
      }
      if (!recordFinishedRpcs) {
        return;
      }
      stopwatch.stop();
      long elapsedTimeNanos = stopwatch.elapsed(TimeUnit.NANOSECONDS);
      MeasureMap measureMap = module.statsRecorder.newMeasureMap()
          .put(RpcMeasureConstants.RPC_SERVER_FINISHED_COUNT, 1)
          // The latency is double value
          .put(RpcMeasureConstants.RPC_SERVER_SERVER_LATENCY, elapsedTimeNanos / NANOS_PER_MILLI)
          .put(RpcMeasureConstants.RPC_SERVER_RESPONSE_COUNT, outboundMessageCount)
          .put(RpcMeasureConstants.RPC_SERVER_REQUEST_COUNT, inboundMessageCount)
          .put(RpcMeasureConstants.RPC_SERVER_RESPONSE_BYTES, outboundWireSize)
          .put(RpcMeasureConstants.RPC_SERVER_REQUEST_BYTES, inboundWireSize)
          .put(RpcMeasureConstants.RPC_SERVER_UNCOMPRESSED_RESPONSE_BYTES, outboundUncompressedSize)
          .put(RpcMeasureConstants.RPC_SERVER_UNCOMPRESSED_REQUEST_BYTES, inboundUncompressedSize);
      if (!status.isOk()) {
        measureMap.put(RpcMeasureConstants.RPC_SERVER_ERROR_COUNT, 1);
      }
      measureMap.record(
          module
              .tagger
              .toBuilder(parentCtx)
              .put(RpcMeasureConstants.RPC_STATUS, TagValue.create(status.getCode().toString()))
              .build());
    }

    @Override
    public Context filterContext(Context context) {
      if (!tagger.empty().equals(parentCtx)) {
        return context.withValue(TAG_CONTEXT_KEY, parentCtx);
      }
      return context;
    }
  }

  @VisibleForTesting
  final class ServerTracerFactory extends ServerStreamTracer.Factory {
    private final boolean recordStartedRpcs;
    private final boolean recordFinishedRpcs;

    ServerTracerFactory(boolean recordStartedRpcs, boolean recordFinishedRpcs) {
      this.recordStartedRpcs = recordStartedRpcs;
      this.recordFinishedRpcs = recordFinishedRpcs;
    }

    @Override
    public ServerStreamTracer newServerStreamTracer(String fullMethodName, Metadata headers) {
      TagContext parentCtx = headers.get(statsHeader);
      if (parentCtx == null) {
        parentCtx = tagger.empty();
      }
      parentCtx =
          tagger
              .toBuilder(parentCtx)
              .put(RpcMeasureConstants.RPC_METHOD, TagValue.create(fullMethodName))
              .build();
      return new ServerTracer(
          CensusStatsModule.this,
          fullMethodName,
          parentCtx,
          stopwatchSupplier,
          tagger,
          recordStartedRpcs,
          recordFinishedRpcs);
    }
  }

  @VisibleForTesting
  final class StatsClientInterceptor implements ClientInterceptor {
    private final boolean recordStartedRpcs;
    private final boolean recordFinishedRpcs;

    StatsClientInterceptor(boolean recordStartedRpcs, boolean recordFinishedRpcs) {
      this.recordStartedRpcs = recordStartedRpcs;
      this.recordFinishedRpcs = recordFinishedRpcs;
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      // New RPCs on client-side inherit the tag context from the current Context.
      TagContext parentCtx = tagger.getCurrentTagContext();
      final ClientCallTracer tracerFactory =
          newClientCallTracer(parentCtx, method.getFullMethodName(),
              recordStartedRpcs, recordFinishedRpcs);
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
