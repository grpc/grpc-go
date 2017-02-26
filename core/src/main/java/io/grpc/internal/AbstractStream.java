/*
 * Copyright 2014, Google Inc. All rights reserved.
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
import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.MoreObjects;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.Decompressor;
import java.io.InputStream;
import javax.annotation.concurrent.GuardedBy;

/**
 * Abstract base class for {@link Stream} implementations.
 */
public abstract class AbstractStream implements Stream {
  /**
   * The default number of queued bytes for a given stream, below which
   * {@link StreamListener#onReady()} will be called.
   */
  public static final int DEFAULT_ONREADY_THRESHOLD = 32 * 1024;

  public static final int ABSENT_ID = -1;

  /**
   * Indicates the phase of the GRPC stream in one direction.
   */
  protected enum Phase {
    HEADERS, MESSAGE, STATUS
  }

  private final MessageFramer framer;
  private final MessageDeframer deframer;

  /**
   * Inbound phase is exclusively written to by the transport thread.
   */
  private Phase inboundPhase = Phase.HEADERS;

  /**
   * Outbound phase is exclusively written to by the application thread.
   */
  private Phase outboundPhase = Phase.HEADERS;

  /**
   * The number of queued bytes for a given stream, below which {@link StreamListener#onReady()}
   * will be called.
   */
  private int onReadyThreshold = DEFAULT_ONREADY_THRESHOLD;

  /**
   * The number of bytes currently queued, waiting to be sent. When this falls below
   * onReadyThreshold, {@link StreamListener#onReady()} will be called.
   */
  private int numSentBytesQueued;

  /**
   * Indicates the stream has been created on the connection. This implies that the stream is no
   * longer limited by MAX_CONCURRENT_STREAMS.
   */
  @GuardedBy("onReadyLock")
  private boolean allocated;

  private final Object onReadyLock = new Object();

  @VisibleForTesting
  class FramerSink implements MessageFramer.Sink {
    @Override
    public void deliverFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
      internalSendFrame(frame, endOfStream, flush);
    }
  }

  @VisibleForTesting
  class DeframerListener implements MessageDeframer.Listener {
    @Override
    public void bytesRead(int numBytes) {
      returnProcessedBytes(numBytes);
    }

    @Override
    public void messageRead(InputStream input) {
      receiveMessage(input);
    }

    @Override
    public void deliveryStalled() {
      inboundDeliveryPaused();
    }

    @Override
    public void endOfStream() {
      remoteEndClosed();
    }
  }

  AbstractStream(WritableBufferAllocator bufferAllocator, int maxMessageSize,
      StatsTraceContext statsTraceCtx) {
    framer = new MessageFramer(new FramerSink(), bufferAllocator, statsTraceCtx);
    deframer = new MessageDeframer(new DeframerListener(), Codec.Identity.NONE, maxMessageSize,
        statsTraceCtx, getClass().getName());
  }

  protected final void setMaxInboundMessageSizeProtected(int maxSize) {
    deframer.setMaxInboundMessageSize(maxSize);
  }

  protected final void setMaxOutboundMessageSizeProtected(int maxSize) {
    framer.setMaxOutboundMessageSize(maxSize);
  }

  @VisibleForTesting
  AbstractStream(MessageFramer framer, MessageDeframer deframer) {
    this.framer = framer;
    this.deframer = deframer;
  }

  /**
   * Override this method to provide a stream listener.
   */
  protected abstract StreamListener listener();

  /**
   * Returns the internal ID for this stream. Note that ID can be {@link #ABSENT_ID} for client
   * streams as the transport may defer creating the stream to the remote side until it has a
   * payload or metadata to send.
   */
  public abstract int id();

  /**
   * The number of queued bytes for a given stream, below which {@link StreamListener#onReady()}
   * will be called. Defaults to {@link #DEFAULT_ONREADY_THRESHOLD}.
   */
  public int getOnReadyThreshold() {
    synchronized (onReadyLock) {
      return onReadyThreshold;
    }
  }

  @Override
  public void writeMessage(InputStream message) {
    checkNotNull(message, "message");
    outboundPhase(Phase.MESSAGE);
    if (!framer.isClosed()) {
      framer.writePayload(message);
    }
  }

  @Override
  public final void setMessageCompression(boolean enable) {
    framer.setMessageCompression(enable);
  }

  @Override
  public final void flush() {
    if (!framer.isClosed()) {
      framer.flush();
    }
  }

  @Override
  public boolean isReady() {
    if (listener() != null && outboundPhase() != Phase.STATUS) {
      synchronized (onReadyLock) {
        return allocated && numSentBytesQueued < onReadyThreshold;
      }
    }
    return false;
  }

  /**
   * Closes the underlying framer.
   *
   * <p>No-op if the framer has already been closed.
   */
  final void closeFramer() {
    if (!framer.isClosed()) {
      framer.close();
    }
  }

  /**
   * Frees any resources associated with this stream. Subclass implementations must call this
   * version.
   *
   * <p>NOTE: Can be called by both the transport thread and the application thread. Transport
   * threads need to dispose when the remote side has terminated the stream. Application threads
   * will dispose when the application decides to close the stream as part of normal processing.
   */
  public void dispose() {
    framer.dispose();
  }

  /**
   * Sends an outbound frame to the remote end point.
   *
   * @param frame a buffer containing the chunk of data to be sent.
   * @param endOfStream if {@code true} indicates that no more data will be sent on the stream by
   *        this endpoint.
   * @param flush {@code true} if more data may not be arriving soon
   */
  protected abstract void internalSendFrame(WritableBuffer frame, boolean endOfStream,
      boolean flush);

  /**
   * Handles a message that was just deframed.
   *
   * @param is the stream containing the message
   */
  protected abstract void receiveMessage(InputStream is);

  /**
   * Handles the event that the deframer has no pending deliveries.
   */
  protected abstract void inboundDeliveryPaused();

  /**
   * Handles the event that the deframer has reached end of stream.
   */
  protected abstract void remoteEndClosed();

  /**
   * Returns the given number of processed bytes back to inbound flow control to enable receipt of
   * more data.
   */
  protected abstract void returnProcessedBytes(int processedBytes);

  /**
   * Called when a {@link #deframe(ReadableBuffer, boolean)} operation failed.
   *
   * @param cause the actual failure
   */
  protected abstract void deframeFailed(Throwable cause);

  /**
   * Closes this deframer and frees any resources. After this method is called, additional calls
   * will have no effect.
   */
  protected final void closeDeframer() {
    deframer.close();
  }

  /**
   * Called to parse a received frame and attempt delivery of any completed
   * messages. Must be called from the transport thread.
   */
  protected final void deframe(ReadableBuffer frame, boolean endOfStream) {
    try {
      deframer.deframe(frame, endOfStream);
    } catch (Throwable t) {
      deframeFailed(t);
    }
  }

  /**
   * Indicates whether delivery is currently stalled, pending receipt of more data.
   */
  protected final boolean isDeframerStalled() {
    return deframer.isStalled();
  }

  /**
   * Called to request the given number of messages from the deframer. Must be called
   * from the transport thread.
   */
  protected final void requestMessagesFromDeframer(int numMessages) {
    try {
      deframer.request(numMessages);
    } catch (Throwable t) {
      deframeFailed(t);
    }
  }

  @Override
  public final void setCompressor(Compressor compressor) {
    framer.setCompressor(checkNotNull(compressor, "compressor"));
  }

  @Override
  public final void setDecompressor(Decompressor decompressor) {
    deframer.setDecompressor(checkNotNull(decompressor, "decompressor"));
  }

  /**
   * Event handler to be called by the subclass when the stream's headers have passed any connection
   * flow control (i.e., MAX_CONCURRENT_STREAMS). It may call the listener's {@link
   * StreamListener#onReady()} handler if appropriate. This must be called from the transport
   * thread, since the listener may be called back directly.
   */
  protected final void onStreamAllocated() {
    checkState(listener() != null);
    synchronized (onReadyLock) {
      checkState(!allocated, "Already allocated");
      allocated = true;
    }
    notifyIfReady();
  }

  /**
   * Event handler to be called by the subclass when a number of bytes are being queued for sending
   * to the remote endpoint.
   *
   * @param numBytes the number of bytes being sent.
   */
  protected final void onSendingBytes(int numBytes) {
    synchronized (onReadyLock) {
      numSentBytesQueued += numBytes;
    }
  }

  /**
   * Event handler to be called by the subclass when a number of bytes has been sent to the remote
   * endpoint. May call back the listener's {@link StreamListener#onReady()} handler if appropriate.
   * This must be called from the transport thread, since the listener may be called back directly.
   *
   * @param numBytes the number of bytes that were sent.
   */
  protected final void onSentBytes(int numBytes) {
    boolean doNotify;
    synchronized (onReadyLock) {
      boolean belowThresholdBefore = numSentBytesQueued < onReadyThreshold;
      numSentBytesQueued -= numBytes;
      boolean belowThresholdAfter = numSentBytesQueued < onReadyThreshold;
      doNotify = !belowThresholdBefore && belowThresholdAfter;
    }
    if (doNotify) {
      notifyIfReady();
    }
  }

  @VisibleForTesting
  final void notifyIfReady() {
    boolean doNotify = false;
    synchronized (onReadyLock) {
      doNotify = isReady();
    }
    if (doNotify) {
      listener().onReady();
    }
  }

  final Phase inboundPhase() {
    return inboundPhase;
  }

  /**
   * Transitions the inbound phase to the given phase and returns the previous phase.
   *
   * @throws IllegalStateException if the transition is disallowed
   */
  final Phase inboundPhase(Phase nextPhase) {
    Phase tmp = inboundPhase;
    inboundPhase = verifyNextPhase(inboundPhase, nextPhase);
    return tmp;
  }

  final Phase outboundPhase() {
    return outboundPhase;
  }

  /**
   * Transitions the outbound phase to the given phase and returns the previous phase.
   *
   * @throws IllegalStateException if the transition is disallowed
   */
  final Phase outboundPhase(Phase nextPhase) {
    Phase tmp = outboundPhase;
    outboundPhase = verifyNextPhase(outboundPhase, nextPhase);
    return tmp;
  }

  @VisibleForTesting
  Phase verifyNextPhase(Phase currentPhase, Phase nextPhase) {
    if (nextPhase.ordinal() < currentPhase.ordinal()) {
      throw new IllegalStateException(
          String.format("Cannot transition phase from %s to %s", currentPhase, nextPhase));
    }
    return nextPhase;
  }

  /**
   * Returns {@code true} if the stream can receive data from its remote peer.
   */
  public boolean canReceive() {
    return inboundPhase() != Phase.STATUS;
  }

  /**
   * Returns {@code true} if the stream can send data to its remote peer.
   */
  public boolean canSend() {
    return outboundPhase() != Phase.STATUS;
  }

  /**
   * Whether the stream is fully closed. Note that this method is not thread-safe as {@code
   * inboundPhase} and {@code outboundPhase} are mutated in different threads. Tests must account
   * for thread coordination when calling.
   */
  @VisibleForTesting
  public boolean isClosed() {
    return inboundPhase() == Phase.STATUS && outboundPhase() == Phase.STATUS;
  }

  @Override
  public String toString() {
    return toStringHelper().toString();
  }

  protected MoreObjects.ToStringHelper toStringHelper() {
    return MoreObjects.toStringHelper(this)
        .add("id", id())
        .add("inboundPhase", inboundPhase().name())
        .add("outboundPhase", outboundPhase().name());
  }
}
