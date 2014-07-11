package com.google.net.stubby.newtransport;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;
import static com.google.net.stubby.newtransport.StreamState.OPEN;
import static com.google.net.stubby.newtransport.StreamState.READ_ONLY;

import com.google.common.base.Preconditions;
import com.google.common.io.Closeables;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.net.stubby.Status;

import java.io.InputStream;
import java.nio.ByteBuffer;

import javax.annotation.Nullable;

/**
 * Abstract base class for {@link Stream} implementations.
 */
public abstract class AbstractStream implements Stream {

  /**
   * Indicates the phase of the GRPC stream in one direction.
   */
  protected enum Phase {
    CONTEXT, MESSAGE, STATUS
  }

  private volatile StreamState state = StreamState.OPEN;
  private Status status;
  private final Object stateLock = new Object();
  private final Object writeLock = new Object();
  private final MessageFramer framer;
  private final StreamListener listener;
  protected Phase inboundPhase = Phase.CONTEXT;
  protected Phase outboundPhase = Phase.CONTEXT;

  /**
   * Handler for Framer output.
   */
  private final Framer.Sink<ByteBuffer> outboundFrameHandler = new Framer.Sink<ByteBuffer>() {
    @Override
    public void deliverFrame(ByteBuffer frame, boolean endOfStream) {
      sendFrame(frame, endOfStream);
    }
  };

  /**
   * Handler for Deframer output.
   */
  private final Framer inboundMessageHandler = new Framer() {
    @Override
    public void writeContext(String name, InputStream value, int length) {
      ListenableFuture<Void> future = null;
      try {
        inboundPhase(Phase.CONTEXT);
        future = listener.contextRead(name, value, length);
      } finally {
        closeWhenDone(future, value);
      }
    }

    @Override
    public void writePayload(InputStream input, int length) {
      ListenableFuture<Void> future = null;
      try {
        inboundPhase(Phase.MESSAGE);
        future = listener.messageRead(input, length);
      } finally {
        closeWhenDone(future, input);
      }
    }

    @Override
    public void writeStatus(Status status) {
      inboundPhase(Phase.STATUS);
      setStatus(status);
    }

    @Override
    public void flush() {}

    @Override
    public boolean isClosed() {
      return false;
    }

    @Override
    public void close() {}

    @Override
    public void dispose() {}
  };

  protected AbstractStream(StreamListener listener) {
    this.listener = Preconditions.checkNotNull(listener, "listener");

    framer = new MessageFramer(outboundFrameHandler, 4096);
    // No compression at the moment.
    framer.setAllowCompression(false);
  }

  @Override
  public StreamState state() {
    return state;
  }

  @Override
  public final void close() {
    outboundPhase(Phase.STATUS);
    synchronized (stateLock) {
      state = state == OPEN ? READ_ONLY : CLOSED;
    }
    synchronized (writeLock) {
      framer.close();
    }
  }

  /**
   * Free any resources associated with this stream. Subclass implementations must call this
   * version.
   */
  public void dispose() {
    synchronized (writeLock) {
      framer.dispose();
    }
  }

  @Override
  public final void writeContext(String name, InputStream value, int length,
      @Nullable Runnable accepted) {
    Preconditions.checkNotNull(name, "name");
    Preconditions.checkNotNull(value, "value");
    Preconditions.checkArgument(length >= 0, "length must be >= 0");
    outboundPhase(Phase.CONTEXT);
    synchronized (writeLock) {
      if (!framer.isClosed()) {
        framer.writeContext(name, value, length);
      }
    }

    // TODO(user): add flow control.
    if (accepted != null) {
      accepted.run();
    }
  }

  @Override
  public final void writeMessage(InputStream message, int length, @Nullable Runnable accepted) {
    Preconditions.checkNotNull(message, "message");
    Preconditions.checkArgument(length >= 0, "length must be >= 0");
    outboundPhase(Phase.MESSAGE);
    synchronized (writeLock) {
      if (!framer.isClosed()) {
        framer.writePayload(message, length);
      }
    }

    // TODO(user): add flow control.
    if (accepted != null) {
      accepted.run();
    }
  }

  @Override
  public final void flush() {
    synchronized (writeLock) {
      if (!framer.isClosed()) {
        framer.flush();
      }
    }
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

  /**
   * Sends an outbound frame to the server.
   *
   * @param frame a buffer containing the chunk of data to be sent.
   * @param endOfStream if {@code true} indicates that no more data will be sent on the stream by
   *        this endpoint.
   */
  protected abstract void sendFrame(ByteBuffer frame, boolean endOfStream);

  /**
   * Gets the handler for inbound messages. Subclasses must use this as the target for a
   * {@link com.google.net.stubby.newtransport.Deframer}.
   */
  protected final Framer inboundMessageHandler() {
    return inboundMessageHandler;
  }

  /**
   * Transitions the inbound phase. If the transition is disallowed, throws a
   * {@link IllegalStateException}.
   */
  protected final void inboundPhase(Phase nextPhase) {
    inboundPhase = verifyNextPhase(inboundPhase, nextPhase);
  }

  /**
   * Transitions the outbound phase. If the transition is disallowed, throws a
   * {@link IllegalStateException}.
   */
  protected final void outboundPhase(Phase nextPhase) {
    outboundPhase = verifyNextPhase(outboundPhase, nextPhase);
  }

  private Phase verifyNextPhase(Phase currentPhase, Phase nextPhase) {
    if (nextPhase.ordinal() < currentPhase.ordinal() || currentPhase == Phase.STATUS) {
      throw new IllegalStateException(
          String.format("Cannot transition phase from %s to %s", currentPhase, nextPhase));
    }
    return nextPhase;
  }

  /**
   * If the given future is provided, closes the {@link InputStream} when it completes. Otherwise
   * the {@link InputStream} is closed immediately.
   */
  private static void closeWhenDone(@Nullable ListenableFuture<Void> future,
      final InputStream input) {
    if (future == null) {
      Closeables.closeQuietly(input);
      return;
    }

    // Close the buffer when the future completes.
    future.addListener(new Runnable() {
      @Override
      public void run() {
        Closeables.closeQuietly(input);
      }
    }, MoreExecutors.sameThreadExecutor());
  }
}
