package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;
import static com.google.net.stubby.newtransport.StreamState.OPEN;
import static com.google.net.stubby.newtransport.StreamState.READ_ONLY;

import com.google.common.base.Preconditions;
import com.google.common.io.Closeables;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.Deframer;
import com.google.net.stubby.newtransport.Framer;
import com.google.net.stubby.newtransport.MessageFramer;
import com.google.net.stubby.newtransport.StreamListener;
import com.google.net.stubby.newtransport.StreamState;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelPromise;

import java.io.InputStream;
import java.nio.ByteBuffer;

import javax.annotation.Nullable;

/**
 * Client stream for a Netty transport.
 */
class NettyClientStream implements ClientStream {
  public static final int PENDING_STREAM_ID = -1;

  /**
   * Indicates the phase of the GRPC stream in one direction.
   */
  private enum Phase {
    CONTEXT, MESSAGE, STATUS
  }

  /**
   * Guards transition of stream state.
   */
  private final Object stateLock = new Object();

  /**
   * Guards access to the frame writer.
   */
  private final Object writeLock = new Object();

  private volatile StreamState state = OPEN;
  private volatile int id = PENDING_STREAM_ID;
  private Status status;
  private Phase inboundPhase = Phase.CONTEXT;
  private Phase outboundPhase = Phase.CONTEXT;
  private final StreamListener listener;
  private final Channel channel;
  private final Framer framer;
  private final Deframer<ByteBuf> deframer;

  private final Framer.Sink<ByteBuffer> outboundFrameHandler = new Framer.Sink<ByteBuffer>() {
    @Override
    public void deliverFrame(ByteBuffer buffer, boolean endStream) {
      ByteBuf buf = toByteBuf(buffer);
      send(buf, endStream, endStream);
    }
  };

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

  NettyClientStream(StreamListener listener, Channel channel) {
    this.listener = Preconditions.checkNotNull(listener, "listener");
    this.channel = Preconditions.checkNotNull(channel, "channel");
    this.deframer = new ByteBufDeframer(channel.alloc(), inboundMessageHandler);
    this.framer = new MessageFramer(outboundFrameHandler, 4096);
  }

  /**
   * Returns the HTTP/2 ID for this stream.
   */
  public int id() {
    return id;
  }

  void id(int id) {
    this.id = id;
  }

  @Override
  public StreamState state() {
    return state;
  }

  @Override
  public void close() {
    outboundPhase(Phase.STATUS);
    // Transition the state to mark the close the local side of the stream.
    synchronized (stateLock) {
      state = state == OPEN ? READ_ONLY : CLOSED;
    }

    // Close the frame writer and send any buffered frames.
    synchronized (writeLock) {
      framer.close();
    }
  }

  @Override
  public void cancel() {
    outboundPhase = Phase.STATUS;

    // Send the cancel command to the handler.
    channel.writeAndFlush(new CancelStreamCommand(this));
  }

  /**
   * Free any resources associated with this stream.
   */
  public void dispose() {
    synchronized (writeLock) {
      framer.dispose();
    }
  }

  @Override
  public void writeContext(String name, InputStream value, int length,
      @Nullable final Runnable accepted) {
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
  public void writeMessage(InputStream message, int length, @Nullable final Runnable accepted) {
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
  public void flush() {
    synchronized (writeLock) {
      if (!framer.isClosed()) {
        framer.flush();
      }
    }
  }

  /**
   * Called in the channel thread to process the content of an inbound DATA frame.
   *
   * @param frame the inbound HTTP/2 DATA frame. If this buffer is not used immediately, it must be
   *        retained.
   * @param promise the promise to be set after the application has finished processing the frame.
   */
  public void inboundDataReceived(ByteBuf frame, boolean endOfStream, ChannelPromise promise) {
    Preconditions.checkNotNull(frame, "frame");
    Preconditions.checkNotNull(promise, "promise");
    if (state == CLOSED) {
      promise.setSuccess();
      return;
    }

    // Retain the ByteBuf until it is released by the deframer.
    deframer.deliverFrame(frame.retain(), endOfStream);

    // TODO(user): add flow control.
    promise.setSuccess();
  }

  /**
   * Sets the status if not already set and notifies the stream listener that the stream was closed.
   * This method must be called from the Netty channel thread.
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
   * Writes the given frame to the channel.
   *
   * @param data the grpc frame to be written.
   * @param endStream indicates whether this is the last frame to be sent for this stream.
   * @param endMessage indicates whether the data ends at a message boundary.
   */
  private void send(ByteBuf data, boolean endStream, boolean endMessage) {
    SendGrpcFrameCommand frame = new SendGrpcFrameCommand(this, data, endStream, endMessage);
    channel.writeAndFlush(frame);
  }

  /**
   * Copies the content of the given {@link ByteBuffer} to a new {@link ByteBuf} instance.
   */
  private ByteBuf toByteBuf(ByteBuffer source) {
    ByteBuf buf = channel.alloc().buffer(source.remaining());
    buf.writeBytes(source);
    return buf;
  }

  /**
   * Transitions the inbound phase. If the transition is disallowed, throws a
   * {@link IllegalStateException}.
   */
  private void inboundPhase(Phase nextPhase) {
    inboundPhase = verifyNextPhase(inboundPhase, nextPhase);
  }

  /**
   * Transitions the outbound phase. If the transition is disallowed, throws a
   * {@link IllegalStateException}.
   */
  private void outboundPhase(Phase nextPhase) {
    outboundPhase = verifyNextPhase(outboundPhase, nextPhase);
  }

  private Phase verifyNextPhase(Phase currentPhase, Phase nextPhase) {
    // Only allow forward progression.
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
