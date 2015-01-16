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

package com.google.net.stubby.transport;

import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

import java.io.InputStream;
import java.nio.ByteBuffer;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;

/**
 * The abstract base class for {@link ClientStream} implementations.
 */
public abstract class AbstractClientStream<IdT> extends AbstractStream<IdT>
    implements ClientStream {

  private static final Logger log = Logger.getLogger(AbstractClientStream.class.getName());
  private static final ListenableFuture<Void> COMPLETED_FUTURE = Futures.immediateFuture(null);

  private final ClientStreamListener listener;
  private boolean listenerClosed;

  // Stored status & trailers to report when deframer completes or
  // transportReportStatus is directly called.
  private Status status;
  private Metadata.Trailers trailers;
  private Runnable closeListenerTask;


  protected AbstractClientStream(ClientStreamListener listener, Executor deframerExecutor) {
    super(deframerExecutor);
    this.listener = Preconditions.checkNotNull(listener);
  }

  @Override
  protected ListenableFuture<Void> receiveMessage(InputStream is, int length) {
    if (listenerClosed) {
      return COMPLETED_FUTURE;
    }
    return listener.messageRead(is, length);
  }

  @Override
  public final void writeMessage(InputStream message, int length, @Nullable Runnable accepted) {
    super.writeMessage(message, length, accepted);
  }

  /**
   * The transport implementation has detected a protocol error on the stream. Transports are
   * responsible for properly closing streams when protocol errors occur.
   *
   * @param errorStatus the error to report
   */
  protected void inboundTransportError(Status errorStatus) {
    if (inboundPhase() == Phase.STATUS) {
      log.log(Level.INFO, "Received transport error on closed stream {0} {1}",
          new Object[]{id(), errorStatus});
      return;
    }
    // For transport errors we immediately report status to the application layer
    // and do not wait for additional payloads.
    transportReportStatus(errorStatus, false, new Metadata.Trailers());
  }

  /**
   * Called by transport implementations when they receive headers. When receiving headers
   * a transport may determine that there is an error in the protocol at this phase which is
   * why this method takes an error {@link Status}. If a transport reports an
   * {@link com.google.net.stubby.Status.Code#INTERNAL} error
   *
   * @param headers the parsed headers
   */
  protected void inboundHeadersReceived(Metadata.Headers headers) {
    if (inboundPhase() == Phase.STATUS) {
      log.log(Level.INFO, "Received headers on closed stream {0} {1}",
          new Object[]{id(), headers});
    }
    inboundPhase(Phase.MESSAGE);
    delayDeframer(listener.headersRead(headers));
  }

  /**
   * Process the contents of a received data frame from the server.
   */
  protected void inboundDataReceived(Buffer frame) {
    Preconditions.checkNotNull(frame, "frame");
    if (inboundPhase() == Phase.STATUS) {
      frame.close();
      return;
    }
    if (inboundPhase() == Phase.HEADERS) {
      // Have not received headers yet so error
      inboundTransportError(Status.INTERNAL.withDescription("headers not received before payload"));
      frame.close();
      return;
    }
    inboundPhase(Phase.MESSAGE);

    deframe(frame, false);
  }

  @Override
  protected void inboundDeliveryPaused() {
    runCloseListenerTask();
  }

  @Override
  protected final void deframeFailed(Throwable cause) {
    log.log(Level.WARNING, "Exception processing message", cause);
    cancel();
  }

  /**
   * Called by transport implementations when they receive trailers.
   */
  protected void inboundTrailersReceived(Metadata.Trailers trailers, Status status) {
    Preconditions.checkNotNull(trailers, "trailers");
    if (inboundPhase() == Phase.STATUS) {
      log.log(Level.INFO, "Received trailers on closed stream {0}\n {1}\n {3}",
          new Object[]{id(), status, trailers});
    }
    // Stash the status & trailers so they can be delivered by the deframer calls
    // remoteEndClosed
    this.status = status;
    this.trailers = trailers;
    deframe(Buffers.empty(), true);
  }

  @Override
  protected void remoteEndClosed() {
    transportReportStatus(status, true, trailers);
  }

  @Override
  protected final void internalSendFrame(ByteBuffer frame, boolean endOfStream) {
    sendFrame(frame, endOfStream);
  }

  /**
   * Sends an outbound frame to the remote end point.
   *
   * @param frame a buffer containing the chunk of data to be sent.
   * @param endOfStream if {@code true} indicates that no more data will be sent on the stream by
   *        this endpoint.
   */
  protected abstract void sendFrame(ByteBuffer frame, boolean endOfStream);

  /**
   * Report stream closure with status to the application layer if not already reported. This method
   * must be called from the transport thread.
   *
   * @param newStatus the new status to set
   * @param stopDelivery if {@code true}, interrupts any further delivery of inbound messages that
   *        may already be queued up in the deframer. If {@code false}, the listener will be
   *        notified immediately after all currently completed messages in the deframer have been
   *        delivered to the application.
   */
  public void transportReportStatus(final Status newStatus, boolean stopDelivery,
      final Metadata.Trailers trailers) {
    Preconditions.checkNotNull(newStatus, "newStatus");

    boolean closingLater = closeListenerTask != null && !stopDelivery;
    if (listenerClosed || closingLater) {
      // We already closed (or are about to close) the listener.
      return;
    }

    inboundPhase(Phase.STATUS);
    status = newStatus;
    closeListenerTask = null;

    // Determine if the deframer is stalled (i.e. currently has no complete messages to deliver).
    boolean deliveryStalled = !deframer.isDeliveryOutstanding();

    if (stopDelivery || deliveryStalled) {
      // Close the listener immediately.
      listenerClosed = true;
      listener.closed(newStatus, trailers);
    } else {
      // Delay close until inboundDeliveryStalled()
      closeListenerTask = newCloseListenerTask(newStatus, trailers);
    }
  }

  /**
   * Creates a new {@link Runnable} to close the listener with the given status/trailers.
   */
  private Runnable newCloseListenerTask(final Status status, final Metadata.Trailers trailers) {
    return new Runnable() {
      @Override
      public void run() {
        if (!listenerClosed) {
          // Status has not been reported to the application layer
          listenerClosed = true;
          listener.closed(status, trailers);
        }
      }
    };
  }

  /**
   * Executes the pending listener close task, if one exists.
   */
  private void runCloseListenerTask() {
    if (closeListenerTask != null) {
      closeListenerTask.run();
      closeListenerTask = null;
    }
  }

  @Override
  public final void halfClose() {
    if (outboundPhase(Phase.STATUS) != Phase.STATUS) {
      closeFramer();
    }
  }

  /**
   * Cancel the stream. Called by the application layer, never called by the transport.
   */
  @Override
  public void cancel() {
    outboundPhase(Phase.STATUS);
    if (id() != null) {
      // Only send a cancellation to remote side if we have actually been allocated
      // a stream id and we are not already closed. i.e. the server side is aware of the stream.
      sendCancel();
    }
    dispose();
  }

  /**
   * Send a stream cancellation message to the remote server. Can be called by either the
   * application or transport layers.
   */
  protected abstract void sendCancel();

  @Override
  protected MoreObjects.ToStringHelper toStringHelper() {
    MoreObjects.ToStringHelper toStringHelper = super.toStringHelper();
    if (status != null) {
      toStringHelper.add("status", status);
    }
    return toStringHelper;
  }

  @Override
  public boolean isClosed() {
    return super.isClosed() || listenerClosed;
  }
}
