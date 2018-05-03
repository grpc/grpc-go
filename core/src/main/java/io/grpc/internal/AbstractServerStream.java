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

import com.google.common.base.Preconditions;
import io.grpc.Attributes;
import io.grpc.Decompressor;
import io.grpc.InternalStatus;
import io.grpc.Metadata;
import io.grpc.Status;
import javax.annotation.Nullable;

/**
 * Abstract base class for {@link ServerStream} implementations. Extending classes only need to
 * implement {@link #transportState()} and {@link #abstractServerStreamSink()}. Must only be called
 * from the sending application thread.
 */
public abstract class AbstractServerStream extends AbstractStream
    implements ServerStream, MessageFramer.Sink {
  /**
   * A sink for outbound operations, separated from the stream simply to avoid name
   * collisions/confusion. Only called from application thread.
   */
  protected interface Sink {
    /**
     * Sends response headers to the remote end point.
     *
     * @param headers the headers to be sent to client.
     */
    void writeHeaders(Metadata headers);

    /**
     * Sends an outbound frame to the remote end point.
     *
     * @param frame a buffer containing the chunk of data to be sent.
     * @param flush {@code true} if more data may not be arriving soon
     * @param numMessages the number of messages this frame represents
     */
    void writeFrame(@Nullable WritableBuffer frame, boolean flush, int numMessages);

    /**
     * Sends trailers to the remote end point. This call implies end of stream.
     *
     * @param trailers metadata to be sent to the end point
     * @param headersSent {@code true} if response headers have already been sent.
     * @param status the status that the call ended with
     */
    void writeTrailers(Metadata trailers, boolean headersSent, Status status);

    /**
     * Requests up to the given number of messages from the call to be delivered. This should end up
     * triggering {@link TransportState#requestMessagesFromDeframer(int)} on the transport thread.
     */
    void request(int numMessages);

    /**
     * Tears down the stream, typically in the event of a timeout. This method may be called
     * multiple times and from any thread.
     *
     * <p>This is a clone of {@link ServerStream#cancel(Status)}.
     */
    void cancel(Status status);
  }

  private final MessageFramer framer;
  private final StatsTraceContext statsTraceCtx;
  private boolean outboundClosed;
  private boolean headersSent;

  protected AbstractServerStream(
      WritableBufferAllocator bufferAllocator, StatsTraceContext statsTraceCtx) {
    this.statsTraceCtx = Preconditions.checkNotNull(statsTraceCtx, "statsTraceCtx");
    framer = new MessageFramer(this, bufferAllocator, statsTraceCtx);
  }

  @Override
  protected abstract TransportState transportState();

  /**
   * Sink for transport to be called to perform outbound operations. Each stream must have its own
   * unique sink.
   */
  protected abstract Sink abstractServerStreamSink();

  @Override
  protected final MessageFramer framer() {
    return framer;
  }

  @Override
  public final void request(int numMessages) {
    abstractServerStreamSink().request(numMessages);
  }

  @Override
  public final void writeHeaders(Metadata headers) {
    Preconditions.checkNotNull(headers, "headers");

    headersSent = true;
    abstractServerStreamSink().writeHeaders(headers);
  }

  @Override
  public final void deliverFrame(
      WritableBuffer frame, boolean endOfStream, boolean flush, int numMessages) {
    // Since endOfStream is triggered by the sending of trailers, avoid flush here and just flush
    // after the trailers.
    abstractServerStreamSink().writeFrame(frame, endOfStream ? false : flush, numMessages);
  }

  @Override
  public final void close(Status status, Metadata trailers) {
    Preconditions.checkNotNull(status, "status");
    Preconditions.checkNotNull(trailers, "trailers");
    if (!outboundClosed) {
      outboundClosed = true;
      endOfMessages();
      addStatusToTrailers(trailers, status);
      // Safe to set without synchronization because access is tightly controlled.
      // closedStatus is only set from here, and is read from a place that has happen-after
      // guarantees with respect to here.
      transportState().setClosedStatus(status);
      abstractServerStreamSink().writeTrailers(trailers, headersSent, status);
    }
  }

  private void addStatusToTrailers(Metadata trailers, Status status) {
    trailers.discardAll(InternalStatus.CODE_KEY);
    trailers.discardAll(InternalStatus.MESSAGE_KEY);
    trailers.put(InternalStatus.CODE_KEY, status);
    if (status.getDescription() != null) {
      trailers.put(InternalStatus.MESSAGE_KEY, status.getDescription());
    }
  }

  @Override
  public final void cancel(Status status) {
    abstractServerStreamSink().cancel(status);
  }

  @Override
  public final boolean isReady() {
    return super.isReady();
  }

  @Override
  public final void setDecompressor(Decompressor decompressor) {
    transportState().setDecompressor(Preconditions.checkNotNull(decompressor, "decompressor"));
  }

  @Override public Attributes getAttributes() {
    return Attributes.EMPTY;
  }

  @Override
  public String getAuthority() {
    return null;
  }

  @Override
  public final void setListener(ServerStreamListener serverStreamListener) {
    transportState().setListener(serverStreamListener);
  }

  @Override
  public StatsTraceContext statsTraceContext() {
    return statsTraceCtx;
  }

  /**
   * This should only called from the transport thread (except for private interactions with
   * {@code AbstractServerStream}).
   */
  protected abstract static class TransportState extends AbstractStream.TransportState {
    /** Whether listener.closed() has been called. */
    private boolean listenerClosed;
    private ServerStreamListener listener;
    private final StatsTraceContext statsTraceCtx;

    private boolean endOfStream = false;
    private boolean deframerClosed = false;
    private boolean immediateCloseRequested = false;
    private Runnable deframerClosedTask;
    /** The status that the application used to close this stream. */
    @Nullable
    private Status closedStatus;

    protected TransportState(
        int maxMessageSize,
        StatsTraceContext statsTraceCtx,
        TransportTracer transportTracer) {
      super(
          maxMessageSize,
          statsTraceCtx,
          Preconditions.checkNotNull(transportTracer, "transportTracer"));
      this.statsTraceCtx = Preconditions.checkNotNull(statsTraceCtx, "statsTraceCtx");
    }

    /**
     * Sets the listener to receive notifications. Must be called in the context of the transport
     * thread.
     */
    public final void setListener(ServerStreamListener listener) {
      Preconditions.checkState(this.listener == null, "setListener should be called only once");
      this.listener = Preconditions.checkNotNull(listener, "listener");
    }

    @Override
    public final void onStreamAllocated() {
      super.onStreamAllocated();
      getTransportTracer().reportRemoteStreamStarted();
    }

    @Override
    public void deframerClosed(boolean hasPartialMessage) {
      deframerClosed = true;
      if (endOfStream) {
        if (!immediateCloseRequested && hasPartialMessage) {
          // We've received the entire stream and have data available but we don't have
          // enough to read the next frame ... this is bad.
          deframeFailed(
              Status.INTERNAL
                  .withDescription("Encountered end-of-stream mid-frame")
                  .asRuntimeException());
          deframerClosedTask = null;
          return;
        }
        listener.halfClosed();
      }
      if (deframerClosedTask != null) {
        deframerClosedTask.run();
        deframerClosedTask = null;
      }
    }

    @Override
    protected ServerStreamListener listener() {
      return listener;
    }

    /**
     * Called in the transport thread to process the content of an inbound DATA frame from the
     * client.
     *
     * @param frame the inbound HTTP/2 DATA frame. If this buffer is not used immediately, it must
     *              be retained.
     * @param endOfStream {@code true} if no more data will be received on the stream.
     */
    public void inboundDataReceived(ReadableBuffer frame, boolean endOfStream) {
      Preconditions.checkState(!this.endOfStream, "Past end of stream");
      // Deframe the message. If a failure occurs, deframeFailed will be called.
      deframe(frame);
      if (endOfStream) {
        this.endOfStream = true;
        closeDeframer(false);
      }
    }

    /**
     * Notifies failure to the listener of the stream. The transport is responsible for notifying
     * the client of the failure independent of this method.
     *
     * <p>Unlike {@link #close(Status, Metadata)}, this method is only called from the
     * transport. The transport should use this method instead of {@code close(Status)} for internal
     * errors to prevent exposing unexpected states and exceptions to the application.
     *
     * @param status the error status. Must not be {@link Status#OK}.
     */
    public final void transportReportStatus(final Status status) {
      Preconditions.checkArgument(!status.isOk(), "status must not be OK");
      if (deframerClosed) {
        deframerClosedTask = null;
        closeListener(status);
      } else {
        deframerClosedTask =
            new Runnable() {
              @Override
              public void run() {
                closeListener(status);
              }
            };
        immediateCloseRequested = true;
        closeDeframer(true);
      }
    }

    /**
     * Indicates the stream is considered completely closed and there is no further opportunity for
     * error. It calls the listener's {@code closed()} if it was not already done by {@link
     * #transportReportStatus}.
     */
    public void complete() {
      if (deframerClosed) {
        deframerClosedTask = null;
        closeListener(Status.OK);
      } else {
        deframerClosedTask =
            new Runnable() {
              @Override
              public void run() {
                closeListener(Status.OK);
              }
            };
        immediateCloseRequested = true;
        closeDeframer(true);
      }
    }

    /**
     * Closes the listener if not previously closed and frees resources. {@code newStatus} is a
     * status generated by gRPC. It is <b>not</b> the status the stream closed with.
     */
    private void closeListener(Status newStatus) {
      // If newStatus is OK, the application must have already called AbstractServerStream.close()
      // and the status passed in there was the actual status of the RPC.
      // If newStatus non-OK, then the RPC ended some other way and the server application did
      // not initiate the termination.
      Preconditions.checkState(!newStatus.isOk() || closedStatus != null);
      if (!listenerClosed) {
        if (!newStatus.isOk()) {
          statsTraceCtx.streamClosed(newStatus);
          getTransportTracer().reportStreamClosed(false);
        } else {
          statsTraceCtx.streamClosed(closedStatus);
          getTransportTracer().reportStreamClosed(closedStatus.isOk());
        }
        listenerClosed = true;
        onStreamDeallocated();
        listener().closed(newStatus);
      }
    }

    /**
     * Stores the {@code Status} that the application used to close this stream.
     */
    private void setClosedStatus(Status closeStatus) {
      Preconditions.checkState(closedStatus == null, "closedStatus can only be set once");
      closedStatus = closeStatus;
    }
  }
}
