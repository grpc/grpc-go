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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.CallOptions;
import io.grpc.ClientStreamTracer;
import io.grpc.Context;
import io.grpc.Metadata;
import io.grpc.ServerCall;
import io.grpc.ServerStreamTracer;
import io.grpc.Status;
import io.grpc.StreamTracer;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;
import javax.annotation.concurrent.ThreadSafe;

/**
 * The stats and tracing information for a stream.
 */
@ThreadSafe
public final class StatsTraceContext {
  public static final StatsTraceContext NOOP = new StatsTraceContext(new StreamTracer[0]);

  private final StreamTracer[] tracers;
  private final AtomicBoolean closed = new AtomicBoolean(false);

  /**
   * Factory method for the client-side.
   */
  public static StatsTraceContext newClientContext(CallOptions callOptions, Metadata headers) {
    List<ClientStreamTracer.Factory> factories = callOptions.getStreamTracerFactories();
    if (factories.isEmpty()) {
      return NOOP;
    }
    // This array will be iterated multiple times per RPC. Use primitive array instead of Collection
    // so that for-each doesn't create an Iterator every time.
    StreamTracer[] tracers = new StreamTracer[factories.size()];
    for (int i = 0; i < tracers.length; i++) {
      tracers[i] = factories.get(i).newClientStreamTracer(headers);
    }
    return new StatsTraceContext(tracers);
  }

  /**
   * Factory method for the server-side.
   */
  public static StatsTraceContext newServerContext(
      List<ServerStreamTracer.Factory> factories, String fullMethodName, Metadata headers) {
    if (factories.isEmpty()) {
      return NOOP;
    }
    StreamTracer[] tracers = new StreamTracer[factories.size()];
    for (int i = 0; i < tracers.length; i++) {
      tracers[i] = factories.get(i).newServerStreamTracer(fullMethodName, headers);
    }
    return new StatsTraceContext(tracers);
  }

  @VisibleForTesting
  StatsTraceContext(StreamTracer[] tracers) {
    this.tracers = tracers;
  }

  /**
   * Returns a copy of the tracer list.
   */
  @VisibleForTesting
  public List<StreamTracer> getTracersForTest() {
    return new ArrayList<StreamTracer>(Arrays.asList(tracers));
  }

  /**
   * See {@link ClientStreamTracer#outboundHeaders}.  For client-side only.
   *
   * <p>Transport-specific, thus should be called by transport implementations.
   */
  public void clientOutboundHeaders() {
    for (StreamTracer tracer : tracers) {
      ((ClientStreamTracer) tracer).outboundHeaders();
    }
  }

  /**
   * See {@link ClientStreamTracer#inboundHeaders}.  For client-side only.
   *
   * <p>Called from abstract stream implementations.
   */
  public void clientInboundHeaders() {
    for (StreamTracer tracer : tracers) {
      ((ClientStreamTracer) tracer).inboundHeaders();
    }
  }

  /**
   * See {@link ServerStreamTracer#filterContext}.  For server-side only.
   *
   * <p>Called from {@link io.grpc.internal.ServerImpl}.
   */
  public <ReqT, RespT> Context serverFilterContext(Context context) {
    Context ctx = checkNotNull(context, "context");
    for (StreamTracer tracer : tracers) {
      ctx = ((ServerStreamTracer) tracer).filterContext(ctx);
      checkNotNull(ctx, "%s returns null context", tracer);
    }
    return ctx;
  }

  /**
   * See {@link ServerStreamTracer#serverCallStarted}.  For server-side only.
   *
   * <p>Called from {@link io.grpc.internal.ServerImpl}.
   */
  public void serverCallStarted(ServerCall<?, ?> call) {
    for (StreamTracer tracer : tracers) {
      ((ServerStreamTracer) tracer).serverCallStarted(call);
    }
  }

  /**
   * See {@link StreamTracer#streamClosed}. This may be called multiple times, and only the first
   * value will be taken.
   *
   * <p>Called from abstract stream implementations.
   */
  public void streamClosed(Status status) {
    if (closed.compareAndSet(false, true)) {
      for (StreamTracer tracer : tracers) {
        tracer.streamClosed(status);
      }
    }
  }

  /**
   * See {@link StreamTracer#outboundMessage}.
   *
   * <p>Called from {@link io.grpc.internal.Framer}.
   */
  public void outboundMessage() {
    for (StreamTracer tracer : tracers) {
      tracer.outboundMessage();
    }
  }

  /**
   * See {@link StreamTracer#inboundMessage}.
   *
   * <p>Called from {@link io.grpc.internal.MessageDeframer}.
   */
  public void inboundMessage() {
    for (StreamTracer tracer : tracers) {
      tracer.inboundMessage();
    }
  }

  /**
   * See {@link StreamTracer#outboundUncompressedSize}.
   *
   * <p>Called from {@link io.grpc.internal.Framer}.
   */
  public void outboundUncompressedSize(long bytes) {
    for (StreamTracer tracer : tracers) {
      tracer.outboundUncompressedSize(bytes);
    }
  }

  /**
   * See {@link StreamTracer#outboundWireSize}.
   *
   * <p>Called from {@link io.grpc.internal.Framer}.
   */
  public void outboundWireSize(long bytes) {
    for (StreamTracer tracer : tracers) {
      tracer.outboundWireSize(bytes);
    }
  }

  /**
   * See {@link StreamTracer#inboundUncompressedSize}.
   *
   * <p>Called from {@link io.grpc.internal.MessageDeframer}.
   */
  public void inboundUncompressedSize(long bytes) {
    for (StreamTracer tracer : tracers) {
      tracer.inboundUncompressedSize(bytes);
    }
  }

  /**
   * See {@link StreamTracer#inboundWireSize}.
   *
   * <p>Called from {@link io.grpc.internal.MessageDeframer}.
   */
  public void inboundWireSize(long bytes) {
    for (StreamTracer tracer : tracers) {
      tracer.inboundWireSize(bytes);
    }
  }
}
