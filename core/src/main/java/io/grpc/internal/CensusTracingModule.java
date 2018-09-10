/*
 * Copyright 2016 The gRPC Authors
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
import static io.opencensus.trace.unsafe.ContextUtils.CONTEXT_SPAN_KEY;

import com.google.common.annotations.VisibleForTesting;
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
import io.grpc.StreamTracer;
import io.opencensus.trace.BlankSpan;
import io.opencensus.trace.EndSpanOptions;
import io.opencensus.trace.MessageEvent;
import io.opencensus.trace.MessageEvent.Type;
import io.opencensus.trace.Span;
import io.opencensus.trace.SpanContext;
import io.opencensus.trace.Status;
import io.opencensus.trace.Tracer;
import io.opencensus.trace.propagation.BinaryFormat;
import java.util.concurrent.atomic.AtomicIntegerFieldUpdater;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Provides factories for {@link StreamTracer} that records traces to Census.
 *
 * <p>On the client-side, a factory is created for each call, because ClientCall starts earlier than
 * the ClientStream, and in some cases may even not create a ClientStream at all.  Therefore, it's
 * the factory that reports the summary to Census.
 *
 * <p>On the server-side, there is only one ServerStream per each ServerCall, and ServerStream
 * starts earlier than the ServerCall.  Therefore, only one tracer is created per stream/call and
 * it's the tracer that reports the summary to Census.
 */
final class CensusTracingModule {
  private static final Logger logger = Logger.getLogger(CensusTracingModule.class.getName());

  @Nullable private static final AtomicIntegerFieldUpdater<ClientCallTracer> callEndedUpdater;

  @Nullable private static final AtomicIntegerFieldUpdater<ServerTracer> streamClosedUpdater;

  /**
   * When using Atomic*FieldUpdater, some Samsung Android 5.0.x devices encounter a bug in their JDK
   * reflection API that triggers a NoSuchFieldException. When this occurs, we fallback to
   * (potentially racy) direct updates of the volatile variables.
   */
  static {
    AtomicIntegerFieldUpdater<ClientCallTracer> tmpCallEndedUpdater;
    AtomicIntegerFieldUpdater<ServerTracer> tmpStreamClosedUpdater;
    try {
      tmpCallEndedUpdater =
          AtomicIntegerFieldUpdater.newUpdater(ClientCallTracer.class, "callEnded");
      tmpStreamClosedUpdater =
          AtomicIntegerFieldUpdater.newUpdater(ServerTracer.class, "streamClosed");
    } catch (Throwable t) {
      logger.log(Level.SEVERE, "Creating atomic field updaters failed", t);
      tmpCallEndedUpdater = null;
      tmpStreamClosedUpdater = null;
    }
    callEndedUpdater = tmpCallEndedUpdater;
    streamClosedUpdater = tmpStreamClosedUpdater;
  }

  private final Tracer censusTracer;
  @VisibleForTesting
  final Metadata.Key<SpanContext> tracingHeader;
  private final TracingClientInterceptor clientInterceptor = new TracingClientInterceptor();
  private final ServerTracerFactory serverTracerFactory = new ServerTracerFactory();

  CensusTracingModule(
      Tracer censusTracer, final BinaryFormat censusPropagationBinaryFormat) {
    this.censusTracer = checkNotNull(censusTracer, "censusTracer");
    checkNotNull(censusPropagationBinaryFormat, "censusPropagationBinaryFormat");
    this.tracingHeader =
        Metadata.Key.of("grpc-trace-bin", new Metadata.BinaryMarshaller<SpanContext>() {
            @Override
            public byte[] toBytes(SpanContext context) {
              return censusPropagationBinaryFormat.toByteArray(context);
            }

            @Override
            public SpanContext parseBytes(byte[] serialized) {
              try {
                return censusPropagationBinaryFormat.fromByteArray(serialized);
              } catch (Exception e) {
                logger.log(Level.FINE, "Failed to parse tracing header", e);
                return SpanContext.INVALID;
              }
            }
          });
  }

  /**
   * Creates a {@link ClientCallTracer} for a new call.
   */
  @VisibleForTesting
  ClientCallTracer newClientCallTracer(@Nullable Span parentSpan, MethodDescriptor<?, ?> method) {
    return new ClientCallTracer(parentSpan, method);
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

  @VisibleForTesting
  static Status convertStatus(io.grpc.Status grpcStatus) {
    Status status;
    switch (grpcStatus.getCode()) {
      case OK:
        status = Status.OK;
        break;
      case CANCELLED:
        status = Status.CANCELLED;
        break;
      case UNKNOWN:
        status = Status.UNKNOWN;
        break;
      case INVALID_ARGUMENT:
        status = Status.INVALID_ARGUMENT;
        break;
      case DEADLINE_EXCEEDED:
        status = Status.DEADLINE_EXCEEDED;
        break;
      case NOT_FOUND:
        status = Status.NOT_FOUND;
        break;
      case ALREADY_EXISTS:
        status = Status.ALREADY_EXISTS;
        break;
      case PERMISSION_DENIED:
        status = Status.PERMISSION_DENIED;
        break;
      case RESOURCE_EXHAUSTED:
        status = Status.RESOURCE_EXHAUSTED;
        break;
      case FAILED_PRECONDITION:
        status = Status.FAILED_PRECONDITION;
        break;
      case ABORTED:
        status = Status.ABORTED;
        break;
      case OUT_OF_RANGE:
        status = Status.OUT_OF_RANGE;
        break;
      case UNIMPLEMENTED:
        status = Status.UNIMPLEMENTED;
        break;
      case INTERNAL:
        status = Status.INTERNAL;
        break;
      case UNAVAILABLE:
        status = Status.UNAVAILABLE;
        break;
      case DATA_LOSS:
        status = Status.DATA_LOSS;
        break;
      case UNAUTHENTICATED:
        status = Status.UNAUTHENTICATED;
        break;
      default:
        throw new AssertionError("Unhandled status code " + grpcStatus.getCode());
    }
    if (grpcStatus.getDescription() != null) {
      status = status.withDescription(grpcStatus.getDescription());
    }
    return status;
  }

  private static EndSpanOptions createEndSpanOptions(
      io.grpc.Status status, boolean sampledToLocalTracing) {
    return EndSpanOptions.builder()
        .setStatus(convertStatus(status))
        .setSampleToLocalSpanStore(sampledToLocalTracing)
        .build();
  }

  private static void recordMessageEvent(
      Span span, MessageEvent.Type type,
      int seqNo, long optionalWireSize, long optionalUncompressedSize) {
    MessageEvent.Builder eventBuilder = MessageEvent.builder(type, seqNo);
    if (optionalUncompressedSize != -1) {
      eventBuilder.setUncompressedMessageSize(optionalUncompressedSize);
    }
    if (optionalWireSize != -1) {
      eventBuilder.setCompressedMessageSize(optionalWireSize);
    }
    span.addMessageEvent(eventBuilder.build());
  }

  @VisibleForTesting
  final class ClientCallTracer extends ClientStreamTracer.Factory {
    volatile int callEnded;

    private final boolean isSampledToLocalTracing;
    private final Span span;

    ClientCallTracer(@Nullable Span parentSpan, MethodDescriptor<?, ?> method) {
      checkNotNull(method, "method");
      this.isSampledToLocalTracing = method.isSampledToLocalTracing();
      this.span =
          censusTracer
              .spanBuilderWithExplicitParent(
                  generateTraceSpanName(false, method.getFullMethodName()),
                  parentSpan)
              .setRecordEvents(true)
              .startSpan();
    }

    @Override
    public ClientStreamTracer newClientStreamTracer(CallOptions callOptions, Metadata headers) {
      if (span != BlankSpan.INSTANCE) {
        headers.discardAll(tracingHeader);
        headers.put(tracingHeader, span.getContext());
      }
      return new ClientTracer(span);
    }

    /**
     * Record a finished call and mark the current time as the end time.
     *
     * <p>Can be called from any thread without synchronization.  Calling it the second time or more
     * is a no-op.
     */
    void callEnded(io.grpc.Status status) {
      if (callEndedUpdater != null) {
        if (callEndedUpdater.getAndSet(this, 1) != 0) {
          return;
        }
      } else {
        if (callEnded != 0) {
          return;
        }
        callEnded = 1;
      }
      span.end(createEndSpanOptions(status, isSampledToLocalTracing));
    }
  }

  private static final class ClientTracer extends ClientStreamTracer {
    private final Span span;

    ClientTracer(Span span) {
      this.span = checkNotNull(span, "span");
    }

    @Override
    public void outboundMessageSent(
        int seqNo, long optionalWireSize, long optionalUncompressedSize) {
      recordMessageEvent(
          span, Type.SENT, seqNo, optionalWireSize, optionalUncompressedSize);
    }

    @Override
    public void inboundMessageRead(
        int seqNo, long optionalWireSize, long optionalUncompressedSize) {
      recordMessageEvent(
          span, Type.RECEIVED, seqNo, optionalWireSize, optionalUncompressedSize);
    }
  }


  private final class ServerTracer extends ServerStreamTracer {
    private final Span span;
    volatile boolean isSampledToLocalTracing;
    volatile int streamClosed;

    ServerTracer(String fullMethodName, @Nullable SpanContext remoteSpan) {
      checkNotNull(fullMethodName, "fullMethodName");
      this.span =
          censusTracer
              .spanBuilderWithRemoteParent(
                  generateTraceSpanName(true, fullMethodName),
                  remoteSpan)
              .setRecordEvents(true)
              .startSpan();
    }

    @Override
    public void serverCallStarted(ServerCallInfo<?, ?> callInfo) {
      isSampledToLocalTracing = callInfo.getMethodDescriptor().isSampledToLocalTracing();
    }

    /**
     * Record a finished stream and mark the current time as the end time.
     *
     * <p>Can be called from any thread without synchronization.  Calling it the second time or more
     * is a no-op.
     */
    @Override
    public void streamClosed(io.grpc.Status status) {
      if (streamClosedUpdater != null) {
        if (streamClosedUpdater.getAndSet(this, 1) != 0) {
          return;
        }
      } else {
        if (streamClosed != 0) {
          return;
        }
        streamClosed = 1;
      }
      span.end(createEndSpanOptions(status, isSampledToLocalTracing));
    }

    @Override
    public Context filterContext(Context context) {
      // Access directly the unsafe trace API to create the new Context. This is a safe usage
      // because gRPC always creates a new Context for each of the server calls and does not
      // inherit from the parent Context.
      return context.withValue(CONTEXT_SPAN_KEY, span);
    }

    @Override
    public void outboundMessageSent(
        int seqNo, long optionalWireSize, long optionalUncompressedSize) {
      recordMessageEvent(
          span, Type.SENT, seqNo, optionalWireSize, optionalUncompressedSize);
    }

    @Override
    public void inboundMessageRead(
        int seqNo, long optionalWireSize, long optionalUncompressedSize) {
      recordMessageEvent(
          span, Type.RECEIVED, seqNo, optionalWireSize, optionalUncompressedSize);
    }
  }

  @VisibleForTesting
  final class ServerTracerFactory extends ServerStreamTracer.Factory {
    @SuppressWarnings("ReferenceEquality")
    @Override
    public ServerStreamTracer newServerStreamTracer(String fullMethodName, Metadata headers) {
      SpanContext remoteSpan = headers.get(tracingHeader);
      if (remoteSpan == SpanContext.INVALID) {
        remoteSpan = null;
      }
      return new ServerTracer(fullMethodName, remoteSpan);
    }
  }

  @VisibleForTesting
  final class TracingClientInterceptor implements ClientInterceptor {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      // New RPCs on client-side inherit the tracing context from the current Context.
      // Safe usage of the unsafe trace API because CONTEXT_SPAN_KEY.get() returns the same value
      // as Tracer.getCurrentSpan() except when no value available when the return value is null
      // for the direct access and BlankSpan when Tracer API is used.
      final ClientCallTracer tracerFactory = newClientCallTracer(CONTEXT_SPAN_KEY.get(), method);
      ClientCall<ReqT, RespT> call =
          next.newCall(
              method,
              callOptions.withStreamTracerFactory(tracerFactory));
      return new SimpleForwardingClientCall<ReqT, RespT>(call) {
        @Override
        public void start(Listener<RespT> responseListener, Metadata headers) {
          delegate().start(
              new SimpleForwardingClientCallListener<RespT>(responseListener) {
                @Override
                public void onClose(io.grpc.Status status, Metadata trailers) {
                  tracerFactory.callEnded(status);
                  super.onClose(status, trailers);
                }
              },
              headers);
        }
      };
    }
  }

  /**
   * Convert a full method name to a tracing span name.
   *
   * @param isServer {@code false} if the span is on the client-side, {@code true} if on the
   *                 server-side
   * @param fullMethodName the method name as returned by
   *        {@link MethodDescriptor#getFullMethodName}.
   */
  @VisibleForTesting
  static String generateTraceSpanName(boolean isServer, String fullMethodName) {
    String prefix = isServer ? "Recv" : "Sent";
    return prefix + "." + fullMethodName.replace('/', '.');
  }

}
