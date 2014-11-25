package com.google.net.stubby.transport;

import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;
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

  private final ClientStreamListener listener;
  private boolean listenerClosed;

  // Stored status & trailers to report when deframer completes or
  // transportReportStatus is directly called.
  private Status status;
  private Metadata.Trailers trailers;


  protected AbstractClientStream(ClientStreamListener listener,
                                 @Nullable Decompressor decompressor,
                                 Executor deframerExecutor) {
    super(decompressor, deframerExecutor);
    this.listener = Preconditions.checkNotNull(listener);
  }

  @Override
  protected ListenableFuture<Void> receiveMessage(InputStream is, int length) {
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
    transportReportStatus(errorStatus, new Metadata.Trailers());
  }

  /**
   * Called by transport implementations when they receive headers. When receiving headers
   * a transport may determine that there is an error in the protocol at this phase which is
   * why this method takes an error {@link Status}. If a transport reports an
   * {@link Status.Code#INTERNAL} error
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
    if (GRPC_V2_PROTOCOL) {
      this.trailers = trailers;
    }
    deframe(Buffers.empty(), true);
  }

  /** gRPC protocol v1 support */
  @Override
  protected void receiveStatus(Status status) {
    Preconditions.checkNotNull(status, "status");
    this.status = status;
    trailers = new Metadata.Trailers();
  }

  @Override
  protected void remoteEndClosed() {
    transportReportStatus(status, trailers);
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
   * Report stream closure with status to the application layer if not already reported.
   * This method must be called from the transport thread.
   *
   * @param newStatus the new status to set
   * @return {@code} true if the status was not already set.
   */
  public boolean transportReportStatus(final Status newStatus, Metadata.Trailers trailers) {
    Preconditions.checkNotNull(newStatus, "newStatus");
    inboundPhase(Phase.STATUS);
    status = newStatus;
    // Invoke the observer callback which will schedule work onto an application thread
    if (!listenerClosed) {
      // Status has not been reported to the application layer
      listenerClosed = true;
      listener.closed(newStatus, trailers);
    }
    return true;
  }

  @Override
  public final void halfClose() {
    if (outboundPhase(Phase.STATUS) != Phase.STATUS) {
      closeFramer(null);
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
