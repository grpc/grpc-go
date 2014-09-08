package com.google.net.stubby.newtransport;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;
import static com.google.net.stubby.newtransport.StreamState.OPEN;
import static com.google.net.stubby.newtransport.StreamState.WRITE_ONLY;

import com.google.common.base.Preconditions;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Transport;

/**
 * Abstract base class for {@link ServerStream} implementations.
 */
public abstract class AbstractServerStream extends AbstractStream implements ServerStream {

  private StreamListener listener;

  private final Object stateLock = new Object();
  private volatile StreamState state = StreamState.OPEN;

  @Override
  protected final StreamListener listener() {
    return listener;
  }

  public final void setListener(StreamListener listener) {
    this.listener = Preconditions.checkNotNull(listener, "listener");
  }

  @Override
  public final void close(Status status, Metadata.Trailers trailers) {
    synchronized (stateLock) {
      Preconditions.checkState(!status.isOk() || state == WRITE_ONLY,
          "Cannot close with OK before client half-closes");
      state = CLOSED;
    }
    outboundPhase(Phase.STATUS);
    closeFramer(status);
    dispose();
  }

  @Override
  public StreamState state() {
    return state;
  }

  /**
   * Called when the remote end half-closes the stream.
   */
  public final void remoteEndClosed() {
    StreamState previousState;
    synchronized (stateLock) {
      previousState = state;
      if (previousState == OPEN) {
        state = WRITE_ONLY;
      }
    }
    if (previousState == OPEN) {
      inboundPhase(Phase.STATUS);
      listener.closed(Status.OK, new Metadata.Trailers());
    } else {
      abortStream(
          new Status(Transport.Code.FAILED_PRECONDITION, "Client-end of the stream already closed"),
          true);
    }
  }

  /**
   * Aborts the stream with an error status, cleans up resources and notifies the listener if
   * necessary.
   *
   * <p>Unlike {@link #close(Status, Metadata.Trailers)}, this method is only called from the
   * gRPC framework, so that we need to call closed() on the listener if it has not been called.
   *
   * @param status the error status. Must not be Status.OK.
   * @param notifyClient true if the stream is still writable and you want to notify the client
   *                     about stream closure and send the status
   */
  public final void abortStream(Status status, boolean notifyClient) {
    Preconditions.checkArgument(!status.isOk(), "status must not be OK");
    StreamState previousState;
    synchronized (stateLock) {
      previousState = state;
      if (state == CLOSED) {
        return;
      }
      state = CLOSED;
    }

    if (previousState == OPEN) {
      listener.closed(status, new Metadata.Trailers());
    }  // Otherwise, previousState is WRITE_ONLY thus closed() has already been called.

    outboundPhase(Phase.STATUS);
    if (notifyClient) {
      closeFramer(status);
    }

    dispose();
  }
}
