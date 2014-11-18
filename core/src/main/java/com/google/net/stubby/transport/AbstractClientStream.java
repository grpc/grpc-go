package com.google.net.stubby.transport;

import static com.google.net.stubby.transport.StreamState.CLOSED;
import static com.google.net.stubby.transport.StreamState.OPEN;
import static com.google.net.stubby.transport.StreamState.READ_ONLY;

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
import javax.annotation.concurrent.GuardedBy;

/**
 * The abstract base class for {@link ClientStream} implementations.
 */
public abstract class AbstractClientStream<IdT> extends AbstractStream<IdT>
    implements ClientStream {

  private static final Logger log = Logger.getLogger(AbstractClientStream.class.getName());

  private final ClientStreamListener listener;

  @GuardedBy("stateLock")
  private Status status;

  private final Object stateLock = new Object();
  private volatile StreamState state = StreamState.OPEN;

  // Stored status & trailers to report when deframer completes.
  private Status stashedStatus;
  private Metadata.Trailers stashedTrailers;

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
    if (state() == CLOSED) {
      log.log(Level.INFO, "Received transport error on closed stream {0} {1}",
          new Object[]{id(), errorStatus});
      return;
    }
    inboundPhase(Phase.STATUS);
    // For transport errors we immediately report status to the application layer
    // and do not wait for additional payloads.
    setStatus(errorStatus, new Metadata.Trailers());
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
    if (state() == CLOSED) {
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
    if (state() == CLOSED) {
      frame.close();
      return;
    }
    if (inboundPhase == Phase.HEADERS) {
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
    if (state() == CLOSED) {
      log.log(Level.INFO, "Received trailers on closed stream {0}\n {1}\n {3}",
          new Object[]{id(), status, trailers});
    }
    inboundPhase(Phase.STATUS);
    // Stash the status & trailers so they can be delivered by the deframer calls
    // remoteEndClosed
    stashedStatus = status;
    if (GRPC_V2_PROTOCOL) {
      stashTrailers(trailers);
    }
    deframe(Buffers.empty(), true);
  }

  /** gRPC protocol v1 support */
  @Override
  protected void receiveStatus(Status status) {
    Preconditions.checkNotNull(status, "status");
    stashedStatus = status;
    stashedTrailers = new Metadata.Trailers();
  }

  /**
   * If using gRPC v2 protocol, this method must be called with received trailers before notifying
   * deframer of end of stream.
   */
  protected void stashTrailers(Metadata.Trailers trailers) {
    Preconditions.checkNotNull(trailers, "trailers");
    stashedStatus = trailers.get(Status.CODE_KEY)
        .withDescription(trailers.get(Status.MESSAGE_KEY));
    trailers.removeAll(Status.CODE_KEY);
    trailers.removeAll(Status.MESSAGE_KEY);
    stashedTrailers = trailers;
  }

  @Override
  protected void remoteEndClosed() {
    Preconditions.checkState(stashedStatus != null, "Status and trailers should have been set");
    setStatus(stashedStatus, stashedTrailers);
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
   * Sets the status if not already set and notifies the stream listener that the stream was closed.
   * This method must be called from the transport thread.
   *
   * @param newStatus the new status to set
   * @return {@code} true if the status was not already set.
   */
  public boolean setStatus(final Status newStatus, Metadata.Trailers trailers) {
    Preconditions.checkNotNull(newStatus, "newStatus");
    synchronized (stateLock) {
      if (status != null) {
        // Disallow override of current status.
        return false;
      }
      status = newStatus;
      state = CLOSED;
    }

    // Invoke the observer callback.
    listener.closed(newStatus, trailers);

    // Free any resources.
    dispose();

    return true;
  }

  @Override
  public final void halfClose() {
    outboundPhase(Phase.STATUS);
    synchronized (stateLock) {
      state = state == OPEN ? READ_ONLY : CLOSED;
    }
    closeFramer(null);
  }

  @Override
  public StreamState state() {
    return state;
  }

  @Override
  public void cancel() {
    // Allow phase to go to cancelled regardless of prior phase.
    outboundPhase = Phase.STATUS;
    if (id() != null) {
      // Only send a cancellation to remote side if we have actually been allocated
      // a stream id. i.e. the server side is aware of the stream.
      sendCancel();
    }
  }

  /**
   * Send a stream cancellation message to the remote server.
   */
  protected abstract void sendCancel();
}
