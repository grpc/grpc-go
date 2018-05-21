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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.internal.GrpcUtil.CONTENT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.TIMEOUT_KEY;
import static java.lang.Math.max;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.Deadline;
import io.grpc.Decompressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import java.io.InputStream;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * The abstract base class for {@link ClientStream} implementations. Extending classes only need to
 * implement {@link #transportState()} and {@link #abstractClientStreamSink()}. Must only be called
 * from the sending application thread.
 */
public abstract class AbstractClientStream extends AbstractStream
    implements ClientStream, MessageFramer.Sink {

  private static final Logger log = Logger.getLogger(AbstractClientStream.class.getName());

  /**
   * A sink for outbound operations, separated from the stream simply to avoid name
   * collisions/confusion. Only called from application thread.
   */
  protected interface Sink {
    /** 
     * Sends the request headers to the remote end point.
     *
     * @param metadata the metadata to be sent
     * @param payload the payload needs to be sent in the headers if not null. Should only be used
     *     when sending an unary GET request
     */
    void writeHeaders(Metadata metadata, @Nullable byte[] payload);

    /**
     * Sends an outbound frame to the remote end point.
     *
     * @param frame a buffer containing the chunk of data to be sent, or {@code null} if {@code
     *     endOfStream} with no data to send
     * @param endOfStream {@code true} if this is the last frame; {@code flush} is guaranteed to be
     *     {@code true} if this is {@code true}
     * @param flush {@code true} if more data may not be arriving soon
     * @Param numMessages the number of messages this series of frames represents, must be >= 0.
     */
    void writeFrame(
        @Nullable WritableBuffer frame, boolean endOfStream, boolean flush, int numMessages);

    /**
     * Requests up to the given number of messages from the call to be delivered to the client. This
     * should end up triggering {@link TransportState#requestMessagesFromDeframer(int)} on the
     * transport thread.
     */
    void request(int numMessages);

    /**
     * Tears down the stream, typically in the event of a timeout. This method may be called
     * multiple times and from any thread.
     *
     * <p>This is a clone of {@link ClientStream#cancel(Status)};
     * {@link AbstractClientStream#cancel} delegates to this method.
     */
    void cancel(Status status);
  }

  private final TransportTracer transportTracer;
  private final Framer framer;
  private boolean useGet;
  private Metadata headers;
  /**
   * Whether cancel() has been called. This is not strictly necessary, but removes the delay between
   * cancel() being called and isReady() beginning to return false, since cancel is commonly
   * processed asynchronously.
   */
  private volatile boolean cancelled;

  protected AbstractClientStream(
      WritableBufferAllocator bufferAllocator,
      StatsTraceContext statsTraceCtx,
      TransportTracer transportTracer,
      Metadata headers,
      boolean useGet) {
    checkNotNull(headers, "headers");
    this.transportTracer = checkNotNull(transportTracer, "transportTracer");
    this.useGet = useGet;
    if (!useGet) {
      framer = new MessageFramer(this, bufferAllocator, statsTraceCtx);
      this.headers = headers;
    } else {
      framer = new GetFramer(headers, statsTraceCtx);
    }
  }

  @Override
  public void setDeadline(Deadline deadline) {
    headers.discardAll(TIMEOUT_KEY);
    long effectiveTimeout = max(0, deadline.timeRemaining(TimeUnit.NANOSECONDS));
    headers.put(TIMEOUT_KEY, effectiveTimeout);
  }

  @Override
  public void setMaxOutboundMessageSize(int maxSize) {
    framer.setMaxOutboundMessageSize(maxSize);
  }

  @Override
  public void setMaxInboundMessageSize(int maxSize) {
    transportState().setMaxInboundMessageSize(maxSize);
  }

  @Override
  public final void setFullStreamDecompression(boolean fullStreamDecompression) {
    transportState().setFullStreamDecompression(fullStreamDecompression);
  }

  @Override
  public final void setDecompressorRegistry(DecompressorRegistry decompressorRegistry) {
    transportState().setDecompressorRegistry(decompressorRegistry);
  }

  /** {@inheritDoc} */
  @Override
  protected abstract TransportState transportState();

  @Override
  public final void start(ClientStreamListener listener) {
    transportState().setListener(listener);
    if (!useGet) {
      abstractClientStreamSink().writeHeaders(headers, null);
      headers = null;
    }
  }

  /**
   * Sink for transport to be called to perform outbound operations. Each stream must have its own
   * unique sink.
   */
  protected abstract Sink abstractClientStreamSink();

  @Override
  protected final Framer framer() {
    return framer;
  }

  @Override
  public final void request(int numMessages) {
    abstractClientStreamSink().request(numMessages);
  }

  @Override
  public final void deliverFrame(
      WritableBuffer frame, boolean endOfStream, boolean flush, int numMessages) {
    Preconditions.checkArgument(frame != null || endOfStream, "null frame before EOS");
    abstractClientStreamSink().writeFrame(frame, endOfStream, flush, numMessages);
  }

  @Override
  public final void halfClose() {
    if (!transportState().isOutboundClosed()) {
      transportState().setOutboundClosed();
      endOfMessages();
    }
  }

  @Override
  public final void cancel(Status reason) {
    Preconditions.checkArgument(!reason.isOk(), "Should not cancel with OK status");
    cancelled = true;
    abstractClientStreamSink().cancel(reason);
  }

  @Override
  public final boolean isReady() {
    return super.isReady() && !cancelled;
  }

  protected TransportTracer getTransportTracer() {
    return transportTracer;
  }

  /** This should only called from the transport thread. */
  protected abstract static class TransportState extends AbstractStream.TransportState {
    /** Whether listener.closed() has been called. */
    private final StatsTraceContext statsTraceCtx;
    private boolean listenerClosed;
    private ClientStreamListener listener;
    private boolean fullStreamDecompression;
    private DecompressorRegistry decompressorRegistry = DecompressorRegistry.getDefaultInstance();

    private boolean deframerClosed = false;
    private Runnable deframerClosedTask;

    /** Whether the client has half-closed the stream. */
    private volatile boolean outboundClosed;

    /**
     * Whether the stream is closed from the transport's perspective. This can differ from {@link
     * #listenerClosed} because there may still be messages buffered to deliver to the application.
     */
    private boolean statusReported;
    private Metadata trailers;
    private Status trailerStatus;

    protected TransportState(
        int maxMessageSize,
        StatsTraceContext statsTraceCtx,
        TransportTracer transportTracer) {
      super(maxMessageSize, statsTraceCtx, transportTracer);
      this.statsTraceCtx = checkNotNull(statsTraceCtx, "statsTraceCtx");
    }

    private void setFullStreamDecompression(boolean fullStreamDecompression) {
      this.fullStreamDecompression = fullStreamDecompression;
    }

    private void setDecompressorRegistry(DecompressorRegistry decompressorRegistry) {
      checkState(this.listener == null, "Already called start");
      this.decompressorRegistry =
          checkNotNull(decompressorRegistry, "decompressorRegistry");
    }

    @VisibleForTesting
    public final void setListener(ClientStreamListener listener) {
      checkState(this.listener == null, "Already called setListener");
      this.listener = checkNotNull(listener, "listener");
    }

    @Override
    public void deframerClosed(boolean hasPartialMessage) {
      deframerClosed = true;

      if (trailerStatus != null) {
        if (trailerStatus.isOk() && hasPartialMessage) {
          trailerStatus = Status.INTERNAL.withDescription("Encountered end-of-stream mid-frame");
          trailers = new Metadata();
        }
        transportReportStatus(trailerStatus, false, trailers);
      } else {
        checkState(statusReported, "status should have been reported on deframer closed");
      }

      if (deframerClosedTask != null) {
        deframerClosedTask.run();
        deframerClosedTask = null;
      }
    }

    @Override
    protected final ClientStreamListener listener() {
      return listener;
    }

    private final void setOutboundClosed() {
      outboundClosed = true;
    }

    protected final boolean isOutboundClosed() {
      return outboundClosed;
    }

    /**
     * Called by transport implementations when they receive headers.
     *
     * @param headers the parsed headers
     */
    protected void inboundHeadersReceived(Metadata headers) {
      checkState(!statusReported, "Received headers on closed stream");
      statsTraceCtx.clientInboundHeaders();

      boolean compressedStream = false;
      String streamEncoding = headers.get(CONTENT_ENCODING_KEY);
      if (fullStreamDecompression && streamEncoding != null) {
        if (streamEncoding.equalsIgnoreCase("gzip")) {
          setFullStreamDecompressor(new GzipInflatingBuffer());
          compressedStream = true;
        } else if (!streamEncoding.equalsIgnoreCase("identity")) {
          deframeFailed(
              Status.INTERNAL
                  .withDescription(
                      String.format("Can't find full stream decompressor for %s", streamEncoding))
                  .asRuntimeException());
          return;
        }
      }

      String messageEncoding = headers.get(MESSAGE_ENCODING_KEY);
      if (messageEncoding != null) {
        Decompressor decompressor = decompressorRegistry.lookupDecompressor(messageEncoding);
        if (decompressor == null) {
          deframeFailed(
              Status.INTERNAL
                  .withDescription(String.format("Can't find decompressor for %s", messageEncoding))
                  .asRuntimeException());
          return;
        } else if (decompressor != Codec.Identity.NONE) {
          if (compressedStream) {
            deframeFailed(
                Status.INTERNAL
                    .withDescription(
                        String.format("Full stream and gRPC message encoding cannot both be set"))
                    .asRuntimeException());
            return;
          }
          setDecompressor(decompressor);
        }
      }

      listener().headersRead(headers);
    }

    /**
     * Processes the contents of a received data frame from the server.
     *
     * @param frame the received data frame. Its ownership is transferred to this method.
     */
    protected void inboundDataReceived(ReadableBuffer frame) {
      checkNotNull(frame, "frame");
      boolean needToCloseFrame = true;
      try {
        if (statusReported) {
          log.log(Level.INFO, "Received data on closed stream");
          return;
        }

        needToCloseFrame = false;
        deframe(frame);
      } finally {
        if (needToCloseFrame) {
          frame.close();
        }
      }
    }

    /**
     * Processes the trailers and status from the server.
     *
     * @param trailers the received trailers
     * @param status the status extracted from the trailers
     */
    protected void inboundTrailersReceived(Metadata trailers, Status status) {
      checkNotNull(status, "status");
      checkNotNull(trailers, "trailers");
      if (statusReported) {
        log.log(Level.INFO, "Received trailers on closed stream:\n {1}\n {2}",
            new Object[]{status, trailers});
        return;
      }
      this.trailers = trailers;
      trailerStatus = status;
      closeDeframer(false);
    }

    /**
     * Report stream closure with status to the application layer if not already reported. This
     * method must be called from the transport thread.
     *
     * @param status the new status to set
     * @param stopDelivery if {@code true}, interrupts any further delivery of inbound messages that
     *        may already be queued up in the deframer. If {@code false}, the listener will be
     *        notified immediately after all currently completed messages in the deframer have been
     *        delivered to the application.
     * @param trailers new instance of {@code Trailers}, either empty or those returned by the
     *        server
     */
    public final void transportReportStatus(final Status status, boolean stopDelivery,
        final Metadata trailers) {
      transportReportStatus(status, RpcProgress.PROCESSED, stopDelivery, trailers);
    }

    /**
     * Report stream closure with status to the application layer if not already reported. This
     * method must be called from the transport thread.
     *
     * @param status the new status to set
     * @param rpcProgress RPC progress that the
     *        {@link ClientStreamListener#closed(Status, RpcProgress, Metadata)}
     *        will receive
     * @param stopDelivery if {@code true}, interrupts any further delivery of inbound messages that
     *        may already be queued up in the deframer. If {@code false}, the listener will be
     *        notified immediately after all currently completed messages in the deframer have been
     *        delivered to the application.
     * @param trailers new instance of {@code Trailers}, either empty or those returned by the
     *        server
     */
    public final void transportReportStatus(
        final Status status, final RpcProgress rpcProgress, boolean stopDelivery,
        final Metadata trailers) {
      checkNotNull(status, "status");
      checkNotNull(trailers, "trailers");
      // If stopDelivery, we continue in case previous invocation is waiting for stall
      if (statusReported && !stopDelivery) {
        return;
      }
      statusReported = true;
      onStreamDeallocated();

      if (deframerClosed) {
        deframerClosedTask = null;
        closeListener(status, rpcProgress, trailers);
      } else {
        deframerClosedTask =
            new Runnable() {
              @Override
              public void run() {
                closeListener(status, rpcProgress, trailers);
              }
            };
        closeDeframer(stopDelivery);
      }
    }

    /**
     * Closes the listener if not previously closed.
     *
     * @throws IllegalStateException if the call has not yet been started.
     */
    private void closeListener(
        Status status, RpcProgress rpcProgress, Metadata trailers) {
      if (!listenerClosed) {
        listenerClosed = true;
        statsTraceCtx.streamClosed(status);
        listener().closed(status, rpcProgress, trailers);
        if (getTransportTracer() != null) {
          getTransportTracer().reportStreamClosed(status.isOk());
        }
      }
    }
  }

  private class GetFramer implements Framer {
    private Metadata headers;
    private boolean closed;
    private final StatsTraceContext statsTraceCtx;
    private byte[] payload;

    public GetFramer(Metadata headers, StatsTraceContext statsTraceCtx) {
      this.headers = checkNotNull(headers, "headers");
      this.statsTraceCtx = checkNotNull(statsTraceCtx, "statsTraceCtx");
    }

    @Override
    public void writePayload(InputStream message) {
      checkState(payload == null, "writePayload should not be called multiple times");
      try {
        payload = IoUtils.toByteArray(message);
      } catch (java.io.IOException ex) {
        throw new RuntimeException(ex);
      }
      statsTraceCtx.outboundMessage(0);
      statsTraceCtx.outboundMessageSent(0, payload.length, payload.length);
      statsTraceCtx.outboundUncompressedSize(payload.length);
      // NB(zhangkun83): this is not accurate, because the underlying transport will probably encode
      // it using e.g., base64.  However, we are not supposed to know such detail here.
      //
      // We don't want to move this line to where the encoding happens either, because we'd better
      // contain the message stats reporting in Framer as suggested in StatsTraceContext.
      // Scattering the reporting sites increases the risk of mis-counting or double-counting.
      //
      // Because the payload is usually very small, people shouldn't care about the size difference
      // caused by encoding.
      statsTraceCtx.outboundWireSize(payload.length);
    }

    @Override
    public void flush() {}

    @Override
    public boolean isClosed() {
      return closed;
    }

    /** Closes, with flush. */
    @Override
    public void close() {
      closed = true;
      checkState(payload != null,
          "Lack of request message. GET request is only supported for unary requests");
      abstractClientStreamSink().writeHeaders(headers, payload);
      payload = null;
      headers = null;
    }

    /** Closes, without flush. */
    @Override
    public void dispose() {
      closed = true;
      payload = null;
      headers = null;
    }

    // Compression is not supported for GET encoding.
    @Override
    public Framer setMessageCompression(boolean enable) {
      return this;
    }

    @Override
    public Framer setCompressor(Compressor compressor) {
      return this;
    }

    // TODO(zsurocking): support this
    @Override
    public void setMaxOutboundMessageSize(int maxSize) {}
  }
}
