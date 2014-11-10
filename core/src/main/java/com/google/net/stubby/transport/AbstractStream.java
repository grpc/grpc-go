package com.google.net.stubby.transport;

import com.google.common.base.Preconditions;
import com.google.common.io.Closeables;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.net.stubby.Status;

import java.io.InputStream;
import java.nio.ByteBuffer;
import java.util.concurrent.Executor;

import javax.annotation.Nullable;

/**
 * Abstract base class for {@link Stream} implementations.
 */
public abstract class AbstractStream<IdT> implements Stream {
  /**
   * Global to enable gRPC v2 protocol support, which may be incomplete. This is a complete hack
   * and should please, please, please be temporary to ease migration.
   */
  // TODO(user): remove this once v1 support is dropped.
  public static boolean GRPC_V2_PROTOCOL = false;

  /**
   * Indicates the phase of the GRPC stream in one direction.
   */
  protected enum Phase {
    HEADERS, MESSAGE, STATUS
  }

  private volatile IdT id;
  private final Object writeLock = new Object();
  private final Framer framer;
  final GrpcDeframer deframer;
  final MessageDeframer2 deframer2;
  Phase inboundPhase = Phase.HEADERS;
  Phase outboundPhase = Phase.HEADERS;

  AbstractStream(@Nullable Decompressor decompressor,
                           Executor deframerExecutor) {
    GrpcDeframer.Sink inboundMessageHandler = new GrpcDeframer.Sink() {
      @Override
      public ListenableFuture<Void> messageRead(InputStream input, int length) {
        ListenableFuture<Void> future = null;
        try {
          future = receiveMessage(input, length);
          disableWindowUpdate(future);
          return future;
        } finally {
          closeWhenDone(future, input);
        }
      }

      @Override
      public void statusRead(Status status) {
        receiveStatus(status);
      }

      @Override
      public void endOfStream() {
        remoteEndClosed();
      }
    };
    Framer.Sink<ByteBuffer> outboundFrameHandler = new Framer.Sink<ByteBuffer>() {
      @Override
      public void deliverFrame(ByteBuffer frame, boolean endOfStream) {
        internalSendFrame(frame, endOfStream);
      }
    };
    if (!GRPC_V2_PROTOCOL) {
      framer = new MessageFramer(outboundFrameHandler, 4096);
      this.deframer = new GrpcDeframer(decompressor, inboundMessageHandler, deframerExecutor);
      this.deframer2 = null;
    } else {
      framer = new MessageFramer2(outboundFrameHandler, 4096);
      this.deframer = null;
      this.deframer2 = new MessageDeframer2(inboundMessageHandler, deframerExecutor);
    }
  }

  /**
   * Returns the internal id for this stream. Note that Id can be {@code null} for client streams
   * as the transport may defer creating the stream to the remote side until is has payload or
   * metadata to send.
   */
  @Nullable
  public IdT id() {
    return id;
  }

  /**
   * Set the internal id for this stream
   */
  public void id(IdT id) {
    Preconditions.checkState(id != null, "Can only set id once");
    this.id = id;
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
  public void writeMessage(InputStream message, int length, @Nullable Runnable accepted) {
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
   * Sends an outbound frame to the remote end point.
   *
   * @param frame a buffer containing the chunk of data to be sent.
   * @param endOfStream if {@code true} indicates that no more data will be sent on the stream by
   *        this endpoint.
   */
  protected abstract void internalSendFrame(ByteBuffer frame, boolean endOfStream);

  /** A message was deframed. */
  protected abstract ListenableFuture<Void> receiveMessage(InputStream is, int length);

  /** A status was deframed. */
  protected abstract void receiveStatus(Status status);

  /** Deframer reached end of stream. */
  protected abstract void remoteEndClosed();

  /**
   * If the given future is non-{@code null}, temporarily disables window updates for inbound flow
   * control for this stream until the future completes. If the given future is {@code null}, does
   * nothing.
   */
  protected abstract void disableWindowUpdate(@Nullable ListenableFuture<Void> processingFuture);

  /**
   * Transitions the inbound phase. If the transition is disallowed, throws a
   * {@link IllegalStateException}.
   */
  final void inboundPhase(Phase nextPhase) {
    inboundPhase = verifyNextPhase(inboundPhase, nextPhase);
  }

  /**
   * Transitions the outbound phase. If the transition is disallowed, throws a
   * {@link IllegalStateException}.
   */
  final void outboundPhase(Phase nextPhase) {
    outboundPhase = verifyNextPhase(outboundPhase, nextPhase);
  }

  /**
   * Closes the underlying framer.
   *
   * <p>No-op if the framer has already been closed.
   *
   * @param status if not null, will write the status to the framer before closing it
   */
  final void closeFramer(@Nullable Status status) {
    synchronized (writeLock) {
      if (!framer.isClosed()) {
        if (status != null) {
          framer.writeStatus(status);
        }
        framer.close();
      }
    }
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
    }, MoreExecutors.directExecutor());
  }
}
