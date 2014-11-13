package com.google.net.stubby.transport;

import static com.google.net.stubby.transport.StreamState.CLOSED;
import static com.google.net.stubby.transport.StreamState.OPEN;
import static com.google.net.stubby.transport.StreamState.WRITE_ONLY;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.FutureCallback;
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
import javax.annotation.concurrent.GuardedBy;

/**
 * Abstract base class for {@link ServerStream} implementations.
 */
public abstract class AbstractServerStream<IdT> extends AbstractStream<IdT>
    implements ServerStream {
  private static final Logger log = Logger.getLogger(AbstractServerStream.class.getName());

  private ServerStreamListener listener;

  private final Object stateLock = new Object();
  private volatile StreamState state = StreamState.OPEN;
  private boolean headersSent = false;
  /** Whether listener.closed() has been called. */
  @GuardedBy("stateLock")
  private boolean listenerClosed;
  /**
   * Whether the stream was closed gracefully by the application (vs. a transport-level failure).
   */
  private boolean gracefulClose;
  /** Saved trailers from close() that need to be sent once the framer has sent all messages. */
  private Metadata.Trailers stashedTrailers;
  private final FutureCallback<Object> failureCallback = new FutureCallback<Object>() {
    @Override
    public void onFailure(Throwable t) {
      log.log(Level.WARNING, "Exception processing message", t);
      abortStream(Status.fromThrowable(t), true);
    }

    @Override
    public void onSuccess(Object result) {}
  };

  protected AbstractServerStream(IdT id, @Nullable Decompressor decompressor,
                                 Executor deframerExecutor) {
    super(decompressor, deframerExecutor);
    id(id);
  }

  public final void setListener(ServerStreamListener listener) {
    this.listener = Preconditions.checkNotNull(listener, "listener");
  }

  @Override
  protected ListenableFuture<Void> receiveMessage(InputStream is, int length) {
    inboundPhase(Phase.MESSAGE);
    return listener.messageRead(is, length);
  }

  /** gRPC protocol v1 support */
  @Override
  protected void receiveStatus(Status status) {
    Preconditions.checkState(status == Status.OK, "Received status can only be OK on server");
  }

  @Override
  public void writeHeaders(Metadata.Headers headers) {
    Preconditions.checkNotNull(headers, "headers");
    outboundPhase(Phase.HEADERS);
    headersSent = true;
    internalSendHeaders(headers);
    outboundPhase(Phase.MESSAGE);
  }

  @Override
  public final void writeMessage(InputStream message, int length, @Nullable Runnable accepted) {
    if (!headersSent) {
      writeHeaders(new Metadata.Headers());
      headersSent = true;
    }
    super.writeMessage(message, length, accepted);
  }

  @Override
  public final void close(Status status, Metadata.Trailers trailers) {
    Preconditions.checkNotNull(status, "status");
    Preconditions.checkNotNull(trailers, "trailers");
    outboundPhase(Phase.STATUS);
    synchronized (stateLock) {
      state = CLOSED;
    }
    gracefulClose = true;
    this.stashedTrailers = trailers;
    writeStatusToTrailers(status);
    closeFramer(status);
    dispose();
  }

  private void writeStatusToTrailers(Status status) {
    stashedTrailers.removeAll(Status.CODE_KEY);
    stashedTrailers.removeAll(Status.MESSAGE_KEY);
    stashedTrailers.put(Status.CODE_KEY, status);
    if (status.getDescription() != null) {
      stashedTrailers.put(Status.MESSAGE_KEY, status.getDescription());
    }
  }

  /**
   * Called in the network thread to process the content of an inbound DATA frame from the client.
   *
   * @param frame the inbound HTTP/2 DATA frame. If this buffer is not used immediately, it must
   *              be retained.
   */
  public void inboundDataReceived(Buffer frame, boolean endOfStream) {
    if (state() == StreamState.CLOSED) {
      frame.close();
      return;
    }
    // TODO(user): It sounds sub-optimal to deframe in the network thread. That means
    // decompression is serialized.
    if (!GRPC_V2_PROTOCOL) {
      deframer.deframe(frame, endOfStream);
    } else {
      ListenableFuture<?> future = deframer2.deframe(frame, endOfStream);
      if (future != null) {
        Futures.addCallback(future, failureCallback);
      }
    }
  }

  @Override
  protected final void internalSendFrame(ByteBuffer frame, boolean endOfStream) {
    if (!GRPC_V2_PROTOCOL) {
      sendFrame(frame, endOfStream);
    } else {
      if (frame.hasRemaining()) {
        sendFrame(frame, false);
      }
      if (endOfStream) {
        sendTrailers(stashedTrailers, headersSent);
        headersSent = true;
        stashedTrailers = null;
      }
    }
  }

  /**
   * Sends response headers to the remote end points.
   * @param headers to be sent to client.
   */
  protected abstract void internalSendHeaders(Metadata.Headers headers);

  /**
   * Sends an outbound frame to the remote end point.
   *
   * @param frame a buffer containing the chunk of data to be sent.
   * @param endOfStream if {@code true} indicates that no more data will be sent on the stream by
   *        this endpoint.
   */
  protected abstract void sendFrame(ByteBuffer frame, boolean endOfStream);

  /**
   * Sends trailers to the remote end point. This call implies end of stream.
   *
   * @param trailers metadata to be sent to end point
   * @param headersSent true if response headers have already been sent.
   */
  protected abstract void sendTrailers(Metadata.Trailers trailers, boolean headersSent);

  /**
   * The Stream is considered completely closed and there is no further opportunity for error. It
   * calls the listener's {@code closed()} if it was not already done by {@link #abortStream}. Note
   * that it is expected that either {@code closed()} or {@code abortStream()} was previously
   * called, since {@code closed()} is required for a normal stream closure and {@code
   * abortStream()} for abnormal.
   */
  public void complete() {
    synchronized (stateLock) {
      if (listenerClosed) {
        return;
      }
      listenerClosed = true;
    }
    if (!gracefulClose) {
      listener.closed(Status.INTERNAL.withDescription("successful complete() without close()"));
      throw new IllegalStateException("successful complete() without close()");
    }
    listener.closed(Status.OK);
  }

  @Override
  public StreamState state() {
    return state;
  }

  /**
   * Called when the remote end half-closes the stream.
   */
  @Override
  protected final void remoteEndClosed() {
    synchronized (stateLock) {
      Preconditions.checkState(state == OPEN, "Stream not OPEN");
      state = WRITE_ONLY;
    }
    inboundPhase(Phase.STATUS);
    listener.halfClosed();
  }

  /**
   * Aborts the stream with an error status, cleans up resources and notifies the listener if
   * necessary.
   *
   * <p>Unlike {@link #close(Status, Metadata.Trailers)}, this method is only called from the
   * transport. The transport should use this method instead of {@code close(Status)} for internal
   * errors to prevent exposing unexpected states and exceptions to the application.
   *
   * @param status the error status. Must not be Status.OK.
   * @param notifyClient true if the stream is still writable and you want to notify the client
   *                     about stream closure and send the status
   */
  public final void abortStream(Status status, boolean notifyClient) {
    Preconditions.checkArgument(!status.isOk(), "status must not be OK");
    boolean closeListener;
    synchronized (stateLock) {
      if (state == CLOSED) {
        // Can't actually notify client.
        notifyClient = false;
      }
      state = CLOSED;
      closeListener = !listenerClosed;
      listenerClosed = true;
    }

    try {
      if (notifyClient) {
        if (stashedTrailers == null) {
          stashedTrailers = new Metadata.Trailers();
        }
        writeStatusToTrailers(status);
        closeFramer(status);
      }
      dispose();
    } finally {
      if (closeListener) {
        listener.closed(status);
      }
    }
  }
}
