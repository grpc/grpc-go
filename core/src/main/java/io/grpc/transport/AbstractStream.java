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

package io.grpc.transport;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Objects;
import com.google.common.base.Preconditions;

import java.io.InputStream;

import javax.annotation.Nullable;

/**
 * Abstract base class for {@link Stream} implementations.
 */
public abstract class AbstractStream<IdT> implements Stream {
  /**
   * Indicates the phase of the GRPC stream in one direction.
   */
  protected enum Phase {
    HEADERS, MESSAGE, STATUS
  }

  private volatile IdT id;
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

  AbstractStream(WritableBufferAllocator bufferAllocator) {
    MessageDeframer.Listener inboundMessageHandler = new MessageDeframer.Listener() {
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
    };
    MessageFramer.Sink outboundFrameHandler = new MessageFramer.Sink() {
      @Override
      public void deliverFrame(WritableBuffer frame, boolean endOfStream) {
        internalSendFrame(frame, endOfStream);
      }
    };

    framer = new MessageFramer(outboundFrameHandler, bufferAllocator, 4096);
    this.deframer = new MessageDeframer(inboundMessageHandler);
  }

  /**
   * Returns the internal ID for this stream. Note that ID can be {@code null} for client streams
   * as the transport may defer creating the stream to the remote side until it has a payload or
   * metadata to send.
   */
  @Nullable
  public IdT id() {
    return id;
  }

  /**
   * Set the internal ID for this stream
   */
  public void id(IdT id) {
    Preconditions.checkState(id != null, "Can only set id once");
    this.id = id;
  }

  @Override
  public void writeMessage(InputStream message, int length, @Nullable Runnable accepted) {
    Preconditions.checkNotNull(message, "message");
    Preconditions.checkArgument(length >= 0, "length must be >= 0");
    outboundPhase(Phase.MESSAGE);
    if (!framer.isClosed()) {
      framer.writePayload(message, length);
    }

    // TODO(nmittler): add flow control.
    if (accepted != null) {
      accepted.run();
    }
  }

  @Override
  public final void flush() {
    if (!framer.isClosed()) {
      framer.flush();
    }
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
   * <p> NOTE: Can be called by both the transport thread and the application thread. Transport
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
   */
  protected abstract void internalSendFrame(WritableBuffer frame, boolean endOfStream);

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

  private Phase verifyNextPhase(Phase currentPhase, Phase nextPhase) {
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

  // We support Guava 14
  @SuppressWarnings("deprecation")
  protected Objects.ToStringHelper toStringHelper() {
    return Objects.toStringHelper(this)
        .add("id", id())
        .add("inboundPhase", inboundPhase().name())
        .add("outboundPhase", outboundPhase().name());

  }
}
