/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc.okhttp;

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.internal.ClientStreamListener.RpcProgress.PROCESSED;

import com.google.common.io.BaseEncoding;
import io.grpc.Attributes;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.AbstractClientStream;
import io.grpc.internal.Http2ClientStreamTransportState;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.TransportTracer;
import io.grpc.internal.WritableBuffer;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.Header;
import java.util.ArrayDeque;
import java.util.List;
import java.util.Queue;
import javax.annotation.concurrent.GuardedBy;
import okio.Buffer;

/**
 * Client stream for the okhttp transport.
 */
class OkHttpClientStream extends AbstractClientStream {

  private static final int WINDOW_UPDATE_THRESHOLD = Utils.DEFAULT_WINDOW_SIZE / 2;

  private static final Buffer EMPTY_BUFFER = new Buffer();

  public static final int ABSENT_ID = -1;

  private final MethodDescriptor<?, ?> method;

  private final String userAgent;
  private final StatsTraceContext statsTraceCtx;
  private String authority;
  private Object outboundFlowState;
  private volatile int id = ABSENT_ID;
  private final TransportState state;
  private final Sink sink = new Sink();
  private final Attributes attributes;

  private boolean useGet = false;

  OkHttpClientStream(
      MethodDescriptor<?, ?> method,
      Metadata headers,
      AsyncFrameWriter frameWriter,
      OkHttpClientTransport transport,
      OutboundFlowController outboundFlow,
      Object lock,
      int maxMessageSize,
      String authority,
      String userAgent,
      StatsTraceContext statsTraceCtx,
      TransportTracer transportTracer) {
    super(
        new OkHttpWritableBufferAllocator(),
        statsTraceCtx,
        transportTracer,
        headers,
        method.isSafe());
    this.statsTraceCtx = checkNotNull(statsTraceCtx, "statsTraceCtx");
    this.method = method;
    this.authority = authority;
    this.userAgent = userAgent;
    // OkHttpClientStream is only created after the transport has finished connecting,
    // so it is safe to read the transport attributes.
    // We make a copy here for convenience, even though we can ask the transport.
    this.attributes = transport.getAttributes();
    this.state = new TransportState(maxMessageSize, statsTraceCtx, lock, frameWriter, outboundFlow,
        transport);
  }

  @Override
  protected TransportState transportState() {
    return state;
  }

  @Override
  protected Sink abstractClientStreamSink() {
    return sink;
  }

  /**
   * Returns the type of this stream.
   */
  public MethodDescriptor.MethodType getType() {
    return method.getType();
  }

  public int id() {
    return id;
  }

  /**
   * Returns whether the stream uses GET. This is not known until after {@link Sink#writeHeaders} is
   * invoked.
   */
  boolean useGet() {
    return useGet;
  }

  @Override
  public void setAuthority(String authority) {
    this.authority = checkNotNull(authority, "authority");
  }

  @Override
  public Attributes getAttributes() {
    return attributes;
  }

  class Sink implements AbstractClientStream.Sink {
    @SuppressWarnings("BetaApi") // BaseEncoding is stable in Guava 20.0
    @Override
    public void writeHeaders(Metadata metadata, byte[] payload) {
      String defaultPath = "/" + method.getFullMethodName();
      if (payload != null) {
        useGet = true;
        defaultPath += "?" + BaseEncoding.base64().encode(payload);
      }
      synchronized (state.lock) {
        state.streamReady(metadata, defaultPath);
      }
    }

    @Override
    public void writeFrame(
        WritableBuffer frame, boolean endOfStream, boolean flush, int numMessages) {
      Buffer buffer;
      if (frame == null) {
        buffer = EMPTY_BUFFER;
      } else {
        buffer = ((OkHttpWritableBuffer) frame).buffer();
        int size = (int) buffer.size();
        if (size > 0) {
          onSendingBytes(size);
        }
      }

      synchronized (state.lock) {
        state.sendBuffer(buffer, endOfStream, flush);
        getTransportTracer().reportMessageSent(numMessages);
      }
    }

    @Override
    public void request(final int numMessages) {
      synchronized (state.lock) {
        state.requestMessagesFromDeframer(numMessages);
      }
    }

    @Override
    public void cancel(Status reason) {
      synchronized (state.lock) {
        state.cancel(reason, true, null);
      }
    }
  }

  class TransportState extends Http2ClientStreamTransportState {
    private final Object lock;
    @GuardedBy("lock")
    private List<Header> requestHeaders;
    /**
     * Null iff {@link #requestHeaders} is null.  Non-null iff neither {@link #cancel} nor
     * {@link #start(int)} have been called.
     */
    @GuardedBy("lock")
    private Queue<PendingData> pendingData = new ArrayDeque<PendingData>();
    @GuardedBy("lock")
    private boolean cancelSent = false;
    @GuardedBy("lock")
    private int window = Utils.DEFAULT_WINDOW_SIZE;
    @GuardedBy("lock")
    private int processedWindow = Utils.DEFAULT_WINDOW_SIZE;
    @GuardedBy("lock")
    private final AsyncFrameWriter frameWriter;
    @GuardedBy("lock")
    private final OutboundFlowController outboundFlow;
    @GuardedBy("lock")
    private final OkHttpClientTransport transport;

    public TransportState(
        int maxMessageSize,
        StatsTraceContext statsTraceCtx,
        Object lock,
        AsyncFrameWriter frameWriter,
        OutboundFlowController outboundFlow,
        OkHttpClientTransport transport) {
      super(maxMessageSize, statsTraceCtx, OkHttpClientStream.this.getTransportTracer());
      this.lock = checkNotNull(lock, "lock");
      this.frameWriter = frameWriter;
      this.outboundFlow = outboundFlow;
      this.transport = transport;
    }

    @GuardedBy("lock")
    public void start(int streamId) {
      checkState(id == ABSENT_ID, "the stream has been started with id %s", streamId);
      id = streamId;
      state.onStreamAllocated();

      if (pendingData != null) {
        // Only happens when the stream has neither been started nor cancelled.
        frameWriter.synStream(useGet, false, id, 0, requestHeaders);
        statsTraceCtx.clientOutboundHeaders();
        requestHeaders = null;

        boolean flush = false;
        while (!pendingData.isEmpty()) {
          PendingData data = pendingData.poll();
          outboundFlow.data(data.endOfStream, id, data.buffer, false);
          if (data.flush) {
            flush = true;
          }
        }
        if (flush) {
          outboundFlow.flush();
        }
        pendingData = null;
      }
    }

    @GuardedBy("lock")
    @Override
    protected void onStreamAllocated() {
      super.onStreamAllocated();
      getTransportTracer().reportLocalStreamStarted();
    }

    @GuardedBy("lock")
    @Override
    protected void http2ProcessingFailed(Status status, boolean stopDelivery, Metadata trailers) {
      cancel(status, stopDelivery, trailers);
    }

    @Override
    @GuardedBy("lock")
    public void deframeFailed(Throwable cause) {
      http2ProcessingFailed(Status.fromThrowable(cause), true, new Metadata());
    }

    @Override
    @GuardedBy("lock")
    public void bytesRead(int processedBytes) {
      processedWindow -= processedBytes;
      if (processedWindow <= WINDOW_UPDATE_THRESHOLD) {
        int delta = Utils.DEFAULT_WINDOW_SIZE - processedWindow;
        window += delta;
        processedWindow += delta;
        frameWriter.windowUpdate(id(), delta);
      }
    }

    @Override
    @GuardedBy("lock")
    public void deframerClosed(boolean hasPartialMessage) {
      onEndOfStream();
      super.deframerClosed(hasPartialMessage);
    }

    @Override
    @GuardedBy("lock")
    public void runOnTransportThread(final Runnable r) {
      synchronized (lock) {
        r.run();
      }
    }

    /**
     * Must be called with holding the transport lock.
     */
    @GuardedBy("lock")
    public void transportHeadersReceived(List<Header> headers, boolean endOfStream) {
      if (endOfStream) {
        transportTrailersReceived(Utils.convertTrailers(headers));
      } else {
        transportHeadersReceived(Utils.convertHeaders(headers));
      }
    }

    /**
     * Must be called with holding the transport lock.
     */
    @GuardedBy("lock")
    public void transportDataReceived(okio.Buffer frame, boolean endOfStream) {
      // We only support 16 KiB frames, and the max permitted in HTTP/2 is 16 MiB. This is verified
      // in OkHttp's Http2 deframer. In addition, this code is after the data has been read.
      int length = (int) frame.size();
      window -= length;
      if (window < 0) {
        frameWriter.rstStream(id(), ErrorCode.FLOW_CONTROL_ERROR);
        transport.finishStream(
            id(),
            Status.INTERNAL.withDescription(
                "Received data size exceeded our receiving window size"),
            PROCESSED, false, null, null);
        return;
      }
      super.transportDataReceived(new OkHttpReadableBuffer(frame), endOfStream);
    }

    @GuardedBy("lock")
    private void onEndOfStream() {
      if (!isOutboundClosed()) {
        // If server's end-of-stream is received before client sends end-of-stream, we just send a
        // reset to server to fully close the server side stream.
        transport.finishStream(id(),null, PROCESSED, false, ErrorCode.CANCEL, null);
      } else {
        transport.finishStream(id(), null, PROCESSED, false, null, null);
      }
    }

    @GuardedBy("lock")
    private void cancel(Status reason, boolean stopDelivery, Metadata trailers) {
      if (cancelSent) {
        return;
      }
      cancelSent = true;
      if (pendingData != null) {
        // stream is pending.
        transport.removePendingStream(OkHttpClientStream.this);
        // release holding data, so they can be GCed or returned to pool earlier.
        requestHeaders = null;
        for (PendingData data : pendingData) {
          data.buffer.clear();
        }
        pendingData = null;
        transportReportStatus(reason, true, trailers != null ? trailers : new Metadata());
      } else {
        // If pendingData is null, start must have already been called, which means synStream has
        // been called as well.
        transport.finishStream(
            id(), reason, PROCESSED, stopDelivery, ErrorCode.CANCEL, trailers);
      }
    }

    @GuardedBy("lock")
    private void sendBuffer(Buffer buffer, boolean endOfStream, boolean flush) {
      if (cancelSent) {
        return;
      }
      if (pendingData != null) {
        // Stream is pending start, queue the data.
        pendingData.add(new PendingData(buffer, endOfStream, flush));
      } else {
        checkState(id() != ABSENT_ID, "streamId should be set");
        // If buffer > frameWriter.maxDataLength() the flow-controller will ensure that it is
        // properly chunked.
        outboundFlow.data(endOfStream, id(), buffer, flush);
      }
    }

    @GuardedBy("lock")
    private void streamReady(Metadata metadata, String path) {
      requestHeaders = Headers.createRequestHeaders(metadata, path, authority, userAgent, useGet);
      transport.streamReadyToStart(OkHttpClientStream.this);
    }
  }

  void setOutboundFlowState(Object outboundFlowState) {
    this.outboundFlowState = outboundFlowState;
  }

  Object getOutboundFlowState() {
    return outboundFlowState;
  }

  private static class PendingData {
    Buffer buffer;
    boolean endOfStream;
    boolean flush;

    PendingData(Buffer buffer, boolean endOfStream, boolean flush) {
      this.buffer = buffer;
      this.endOfStream = endOfStream;
      this.flush = flush;
    }
  }
}
