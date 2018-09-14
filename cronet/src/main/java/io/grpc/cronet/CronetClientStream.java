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

package io.grpc.cronet;

import static io.grpc.internal.GrpcUtil.CONTENT_TYPE_KEY;
import static io.grpc.internal.GrpcUtil.TE_HEADER;
import static io.grpc.internal.GrpcUtil.USER_AGENT_KEY;

// TODO(ericgribkoff): Consider changing from android.util.Log to java logging.
import android.util.Log;
import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.io.BaseEncoding;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.cronet.CronetChannelBuilder.StreamBuilderFactory;
import io.grpc.internal.AbstractClientStream;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.Http2ClientStreamTransportState;
import io.grpc.internal.ReadableBuffers;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.TransportFrameUtil;
import io.grpc.internal.TransportTracer;
import io.grpc.internal.WritableBuffer;
import java.nio.ByteBuffer;
import java.nio.charset.Charset;
import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedList;
import java.util.List;
import java.util.Map;
import java.util.Queue;
import java.util.concurrent.Executor;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import org.chromium.net.BidirectionalStream;
import org.chromium.net.CronetException;
import org.chromium.net.ExperimentalBidirectionalStream;
import org.chromium.net.UrlResponseInfo;

/**
 * Client stream for the cronet transport.
 */
class CronetClientStream extends AbstractClientStream {
  private static final int READ_BUFFER_CAPACITY = 4 * 1024;
  private static final ByteBuffer EMPTY_BUFFER = ByteBuffer.allocateDirect(0);
  private static final String LOG_TAG = "grpc-java-cronet";
  private final String url;
  private final String userAgent;
  private final StatsTraceContext statsTraceCtx;
  private final Executor executor;
  private final Metadata headers;
  private final CronetClientTransport transport;
  private final Runnable startCallback;
  @VisibleForTesting
  final boolean idempotent;
  private BidirectionalStream stream;
  private final boolean delayRequestHeader;
  private final Object annotation;
  private final Collection<Object> annotations;
  private final TransportState state;
  private final Sink sink = new Sink();
  private StreamBuilderFactory streamFactory;

  CronetClientStream(
      final String url,
      @Nullable String userAgent,
      Executor executor,
      final Metadata headers,
      CronetClientTransport transport,
      Runnable startCallback,
      Object lock,
      int maxMessageSize,
      boolean alwaysUsePut,
      MethodDescriptor<?, ?> method,
      StatsTraceContext statsTraceCtx,
      CallOptions callOptions,
      TransportTracer transportTracer) {
    super(
        new CronetWritableBufferAllocator(), statsTraceCtx, transportTracer, headers,
        method.isSafe());
    this.url = Preconditions.checkNotNull(url, "url");
    this.userAgent = Preconditions.checkNotNull(userAgent, "userAgent");
    this.statsTraceCtx = Preconditions.checkNotNull(statsTraceCtx, "statsTraceCtx");
    this.executor = Preconditions.checkNotNull(executor, "executor");
    this.headers = Preconditions.checkNotNull(headers, "headers");
    this.transport = Preconditions.checkNotNull(transport, "transport");
    this.startCallback = Preconditions.checkNotNull(startCallback, "startCallback");
    this.idempotent = method.isIdempotent() || alwaysUsePut;
    // Only delay flushing header for unary rpcs.
    this.delayRequestHeader = (method.getType() == MethodDescriptor.MethodType.UNARY);
    this.annotation = callOptions.getOption(CronetCallOptions.CRONET_ANNOTATION_KEY);
    this.annotations = callOptions.getOption(CronetCallOptions.CRONET_ANNOTATIONS_KEY);
    this.state = new TransportState(maxMessageSize, statsTraceCtx, lock, transportTracer);
  }

  @Override
  protected TransportState transportState() {
    return state;
  }

  @Override
  protected Sink abstractClientStreamSink() {
    return sink;
  }

  @Override
  public void setAuthority(String authority) {
    throw new UnsupportedOperationException("Cronet does not support overriding authority");
  }

  class Sink implements AbstractClientStream.Sink {
    @Override
    public void writeHeaders(Metadata metadata, byte[] payload) {
      startCallback.run();

      BidirectionalStreamCallback callback = new BidirectionalStreamCallback();
      String path = url;
      if (payload != null) {
        path += "?" + BaseEncoding.base64().encode(payload);
      }
      BidirectionalStream.Builder builder =
          streamFactory.newBidirectionalStreamBuilder(path, callback, executor);
      if (payload != null) {
        builder.setHttpMethod("GET");
      } else if (idempotent) {
        builder.setHttpMethod("PUT");
      }
      if (delayRequestHeader) {
        builder.delayRequestHeadersUntilFirstFlush(true);
      }
      if (annotation != null) {
        ((ExperimentalBidirectionalStream.Builder) builder).addRequestAnnotation(annotation);
      }
      if (annotations != null) {
        for (Object o : annotations) {
          ((ExperimentalBidirectionalStream.Builder) builder).addRequestAnnotation(o);
        }
      }
      setGrpcHeaders(builder);
      stream = builder.build();
      stream.start();
    }

    @Override
    public void writeFrame(
        WritableBuffer buffer, boolean endOfStream, boolean flush, int numMessages) {
      synchronized (state.lock) {
        if (state.cancelSent) {
          return;
        }
        ByteBuffer byteBuffer;
        if (buffer != null) {
          byteBuffer = ((CronetWritableBuffer) buffer).buffer();
          byteBuffer.flip();
        } else {
          byteBuffer = EMPTY_BUFFER;
        }
        onSendingBytes(byteBuffer.remaining());
        if (!state.streamReady) {
          state.enqueuePendingData(new PendingData(byteBuffer, endOfStream, flush));
        } else {
          streamWrite(byteBuffer, endOfStream, flush);
        }
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
        if (state.cancelSent) {
          return;
        }
        state.cancelSent = true;
        state.cancelReason = reason;
        state.clearPendingData();
        if (stream != null) {
          // Will report stream finish when BidirectionalStreamCallback.onCanceled is called.
          stream.cancel();
        } else {
          transport.finishStream(CronetClientStream.this, reason);
        }
      }
    }
  }

  class TransportState extends Http2ClientStreamTransportState {
    private final Object lock;
    @GuardedBy("lock")
    private Queue<PendingData> pendingData = new LinkedList<PendingData>();
    @GuardedBy("lock")
    private boolean streamReady;
    @GuardedBy("lock")
    private boolean cancelSent = false;
    @GuardedBy("lock")
    private int bytesPendingProcess;
    @GuardedBy("lock")
    private Status cancelReason;
    @GuardedBy("lock")
    private boolean readClosed;
    @GuardedBy("lock")
    private boolean firstWriteComplete;

    public TransportState(
        int maxMessageSize, StatsTraceContext statsTraceCtx, Object lock,
        TransportTracer transportTracer) {
      super(maxMessageSize, statsTraceCtx, transportTracer);
      this.lock = Preconditions.checkNotNull(lock, "lock");
    }

    @GuardedBy("lock")
    public void start(StreamBuilderFactory factory) {
      streamFactory = factory;
    }

    @GuardedBy("lock")
    @Override
    protected void onStreamAllocated() {
      super.onStreamAllocated();
    }

    @GuardedBy("lock")
    @Override
    protected void http2ProcessingFailed(Status status, boolean stopDelivery, Metadata trailers) {
      stream.cancel();
      transportReportStatus(status, stopDelivery, trailers);
    }

    @GuardedBy("lock")
    @Override
    public void deframeFailed(Throwable cause) {
      http2ProcessingFailed(Status.fromThrowable(cause), true, new Metadata());
    }

    @Override
    public void runOnTransportThread(final Runnable r) {
      synchronized (lock) {
        r.run();
      }
    }

    @GuardedBy("lock")
    @Override
    public void bytesRead(int processedBytes) {
      bytesPendingProcess -= processedBytes;
      if (bytesPendingProcess == 0 && !readClosed) {
        if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
          Log.v(LOG_TAG, "BidirectionalStream.read");
        }
        stream.read(ByteBuffer.allocateDirect(READ_BUFFER_CAPACITY));
      }
    }

    @GuardedBy("lock")
    private void transportHeadersReceived(Metadata metadata, boolean endOfStream) {
      if (endOfStream) {
        transportTrailersReceived(metadata);
      } else {
        transportHeadersReceived(metadata);
      }
    }

    @GuardedBy("lock")
    private void transportDataReceived(ByteBuffer buffer, boolean endOfStream) {
      bytesPendingProcess += buffer.remaining();
      super.transportDataReceived(ReadableBuffers.wrap(buffer), endOfStream);
    }

    @GuardedBy("lock")
    private void clearPendingData() {
      for (PendingData data : pendingData) {
        data.buffer.clear();
      }
      pendingData.clear();
    }

    @GuardedBy("lock")
    private void enqueuePendingData(PendingData data) {
      pendingData.add(data);
    }

    @GuardedBy("lock")
    private void writeAllPendingData() {
      for (PendingData data : pendingData) {
        streamWrite(data.buffer, data.endOfStream, data.flush);
      }
      pendingData.clear();
    }
  }

  // TODO(ericgribkoff): move header related method to a common place like GrpcUtil.
  private static boolean isApplicationHeader(String key) {
    // Don't allow reserved non HTTP/2 pseudo headers to be added
    // HTTP/2 headers can not be created as keys because Header.Key disallows the ':' character.
    return !CONTENT_TYPE_KEY.name().equalsIgnoreCase(key)
        && !USER_AGENT_KEY.name().equalsIgnoreCase(key)
        && !TE_HEADER.name().equalsIgnoreCase(key);
  }

  private void setGrpcHeaders(BidirectionalStream.Builder builder) {
    // Psuedo-headers are set by cronet.
    // All non-pseudo headers must come after pseudo headers.
    // TODO(ericgribkoff): remove this and set it on CronetEngine after crbug.com/588204 gets fixed.
    builder.addHeader(USER_AGENT_KEY.name(), userAgent);
    builder.addHeader(CONTENT_TYPE_KEY.name(), GrpcUtil.CONTENT_TYPE_GRPC);
    builder.addHeader("te", GrpcUtil.TE_TRAILERS);

    // Now add any application-provided headers.
    // TODO(ericgribkoff): make a String-based version to avoid unnecessary conversion between
    // String and byte array.
    byte[][] serializedHeaders = TransportFrameUtil.toHttp2Headers(headers);
    for (int i = 0; i < serializedHeaders.length; i += 2) {
      String key = new String(serializedHeaders[i], Charset.forName("UTF-8"));
      // TODO(ericgribkoff): log an error or throw an exception
      if (isApplicationHeader(key)) {
        String value = new String(serializedHeaders[i + 1], Charset.forName("UTF-8"));
        builder.addHeader(key, value);
      }
    }
  }

  private void streamWrite(ByteBuffer buffer, boolean endOfStream, boolean flush) {
    if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
      Log.v(LOG_TAG, "BidirectionalStream.write");
    }
    stream.write(buffer, endOfStream);
    if (flush) {
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "BidirectionalStream.flush");
      }
      stream.flush();
    }
  }

  private void finishStream(Status status) {
    transport.finishStream(this, status);
  }

  @Override
  public Attributes getAttributes() {
    return Attributes.EMPTY;
  }

  class BidirectionalStreamCallback extends BidirectionalStream.Callback {
    private List<Map.Entry<String, String>> trailerList;

    @Override
    public void onStreamReady(BidirectionalStream stream) {
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "onStreamReady");
      }
      synchronized (state.lock) {
        // Now that the stream is ready, call the listener's onReady callback if
        // appropriate.
        state.onStreamAllocated();
        state.streamReady = true;
        state.writeAllPendingData();
      }
    }

    @Override
    public void onResponseHeadersReceived(BidirectionalStream stream, UrlResponseInfo info) {
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "onResponseHeadersReceived. Header=" + info.getAllHeadersAsList());
        Log.v(LOG_TAG, "BidirectionalStream.read");
      }
      reportHeaders(info.getAllHeadersAsList(), false);
      stream.read(ByteBuffer.allocateDirect(READ_BUFFER_CAPACITY));
    }

    @Override
    public void onReadCompleted(BidirectionalStream stream, UrlResponseInfo info,
        ByteBuffer buffer, boolean endOfStream) {
      buffer.flip();
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "onReadCompleted. Size=" + buffer.remaining());
      }

      synchronized (state.lock) {
        state.readClosed = endOfStream;
        // The endOfStream in gRPC has a different meaning so we always call transportDataReceived
        // with endOfStream=false.
        if (buffer.remaining() != 0) {
          state.transportDataReceived(buffer, false);
        }
      }
      if (endOfStream && trailerList != null) {
        // Process trailers if we have already received any.
        reportHeaders(trailerList, true);
      }
    }

    @Override
    public void onWriteCompleted(BidirectionalStream stream, UrlResponseInfo info,
        ByteBuffer buffer, boolean endOfStream) {
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "onWriteCompleted");
      }
      synchronized (state.lock) {
        if (!state.firstWriteComplete) {
          // Cronet API doesn't notify when headers are written to wire, but it occurs before first
          // onWriteCompleted callback.
          state.firstWriteComplete = true;
          statsTraceCtx.clientOutboundHeaders();
        }
        state.onSentBytes(buffer.position());
      }
    }

    @Override
    public void onResponseTrailersReceived(BidirectionalStream stream, UrlResponseInfo info,
        UrlResponseInfo.HeaderBlock trailers) {
      processTrailers(trailers.getAsList());
    }

    // We need this method because UrlResponseInfo.HeaderBlock is a final class and cannot be
    // mocked.
    @VisibleForTesting
    void processTrailers(List<Map.Entry<String, String>> trailerList) {
      this.trailerList = trailerList;
      boolean readClosed;
      synchronized (state.lock) {
        readClosed = state.readClosed;
      }
      if (readClosed) {
        // There's no pending onReadCompleted callback so we can report trailers now.
        reportHeaders(trailerList, true);
      }
      // Otherwise report trailers in onReadCompleted, or onSucceeded.
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "onResponseTrailersReceived. Trailer=" + trailerList.toString());
      }
    }

    @Override
    public void onSucceeded(BidirectionalStream stream, UrlResponseInfo info) {
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "onSucceeded");
      }

      if (!haveTrailersBeenReported()) {
        if (trailerList != null) {
          reportHeaders(trailerList, true);
        } else if (info != null) {
          reportHeaders(info.getAllHeadersAsList(), true);
        } else {
          throw new AssertionError("No response header or trailer");
        }
      }
      finishStream(toGrpcStatus(info));
    }

    @Override
    public void onFailed(BidirectionalStream stream, UrlResponseInfo info,
        CronetException error) {
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "onFailed");
      }
      finishStream(Status.UNAVAILABLE.withCause(error));
    }

    @Override
    public void onCanceled(BidirectionalStream stream, UrlResponseInfo info) {
      if (Log.isLoggable(LOG_TAG, Log.VERBOSE)) {
        Log.v(LOG_TAG, "onCanceled");
      }
      Status status;
      synchronized (state.lock) {
        if (state.cancelReason != null) {
          status = state.cancelReason;
        } else if (info != null) {
          status = toGrpcStatus(info);
        } else {
          status = Status.CANCELLED.withDescription("stream cancelled without reason");
        }
      }
      finishStream(status);
    }

    private void reportHeaders(List<Map.Entry<String, String>> headers, boolean endOfStream) {
      // TODO(ericgribkoff): create new utility methods to eliminate all these conversions
      List<String> headerList = new ArrayList<>();
      for (Map.Entry<String, String> entry : headers) {
        headerList.add(entry.getKey());
        headerList.add(entry.getValue());
      }

      byte[][] headerValues = new byte[headerList.size()][];
      for (int i = 0; i < headerList.size(); i += 2) {
        headerValues[i] = headerList.get(i).getBytes(Charset.forName("UTF-8"));
        headerValues[i + 1] = headerList.get(i + 1).getBytes(Charset.forName("UTF-8"));
      }
      Metadata metadata =
          InternalMetadata.newMetadata(TransportFrameUtil.toRawSerializedHeaders(headerValues));
      synchronized (state.lock) {
        // There's no pending onReadCompleted callback so we can report trailers now.
        state.transportHeadersReceived(metadata, endOfStream);
      }
    }

    private boolean haveTrailersBeenReported() {
      synchronized (state.lock) {
        return trailerList != null && state.readClosed;
      }
    }

    private Status toGrpcStatus(UrlResponseInfo info) {
      return GrpcUtil.httpStatusToGrpcStatus(info.getHttpStatusCode());
    }
  }

  private static class PendingData {
    ByteBuffer buffer;
    boolean endOfStream;
    boolean flush;

    PendingData(ByteBuffer buffer, boolean endOfStream, boolean flush) {
      this.buffer = buffer;
      this.endOfStream = endOfStream;
      this.flush = flush;
    }
  }
}
