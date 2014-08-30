package com.google.net.stubby.newtransport;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;
import static com.google.net.stubby.newtransport.StreamState.OPEN;
import static com.google.net.stubby.newtransport.StreamState.READ_ONLY;

import com.google.common.base.Preconditions;
import com.google.net.stubby.Status;

/**
 * The abstract base class for {@link ClientStream} implementations.
 */
public abstract class AbstractClientStream extends AbstractStream implements ClientStream {

  private final StreamListener listener;

  private Status status;

  private final Object stateLock = new Object();
  private volatile StreamState state = StreamState.OPEN;

  protected AbstractClientStream(StreamListener listener) {
    this.listener = Preconditions.checkNotNull(listener);
  }

  @Override
  protected final StreamListener listener() {
    return listener;
  }

  /**
   * Overrides the behavior of the {@link StreamListener#closed(Status)} method to call
   * {@link #setStatus(Status)}, rather than notifying the {@link #listener()} directly.
   */
  @Override
  protected final StreamListener inboundMessageHandler() {
    // Wraps the base handler to get status update.
    return new ForwardingStreamListener(super.inboundMessageHandler()) {
      @Override
      public void closed(Status status) {
        inboundPhase(Phase.STATUS);
        setStatus(status);
      }
    };
  }

  /**
   * Sets the status if not already set and notifies the stream listener that the stream was closed.
   * This method must be called from the transport thread.
   *
   * @param newStatus the new status to set
   * @return {@code} true if the status was not already set.
   */
  public boolean setStatus(final Status newStatus) {
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
    listener.closed(newStatus);

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
