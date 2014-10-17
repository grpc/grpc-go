package com.google.net.stubby.newtransport;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;
import static com.google.net.stubby.newtransport.StreamState.OPEN;
import static com.google.net.stubby.newtransport.StreamState.READ_ONLY;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

import java.io.InputStream;
import java.nio.ByteBuffer;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * The abstract base class for {@link ClientStream} implementations.
 */
public abstract class AbstractClientStream extends AbstractStream implements ClientStream {

  private final ClientStreamListener listener;

  @GuardedBy("stateLock")
  private Status status;

  private final Object stateLock = new Object();
  private volatile StreamState state = StreamState.OPEN;

  private Status stashedStatus;
  private Metadata.Trailers stashedTrailers;

  protected AbstractClientStream(ClientStreamListener listener) {
    this.listener = Preconditions.checkNotNull(listener);
  }

  protected ListenableFuture<Void> receiveHeaders(Metadata.Headers headers) {
    return listener.headersRead(headers);
  }

  @Override
  protected ListenableFuture<Void> receiveMessage(InputStream is, int length) {
    return listener.messageRead(is, length);
  }

  @Override
  public final void writeMessage(InputStream message, int length, @Nullable Runnable accepted) {
    super.writeMessage(message, length, accepted);
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
    // TODO(user): Delete this hack when trailers are supported by GFE with v2. Currently GFE
    // doesn't support trailers, so when using gRPC v2 protocol GFE will not send any status. We
    // paper over this for now by just assuming OK. For all properly functioning servers (both v1
    // and v2), stashedStatus should not be null here.
    if (stashedStatus == null) {
      stashedStatus = Status.OK;
      stashedTrailers = new Metadata.Trailers();
    }
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
}
