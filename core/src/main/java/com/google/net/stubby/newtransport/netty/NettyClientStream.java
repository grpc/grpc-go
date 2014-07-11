package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;

import com.google.common.base.Preconditions;
import com.google.net.stubby.newtransport.AbstractStream;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.Deframer;
import com.google.net.stubby.newtransport.StreamListener;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelPromise;

import java.nio.ByteBuffer;

/**
 * Client stream for a Netty transport.
 */
class NettyClientStream extends AbstractStream implements ClientStream {
  public static final int PENDING_STREAM_ID = -1;

  private volatile int id = PENDING_STREAM_ID;
  private final Channel channel;
  private final Deframer<ByteBuf> deframer;

  NettyClientStream(StreamListener listener, Channel channel) {
    super(listener);
    this.channel = Preconditions.checkNotNull(channel, "channel");
    this.deframer = new ByteBufDeframer(channel.alloc(), inboundMessageHandler());
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
  public void cancel() {
    outboundPhase = Phase.STATUS;

    // Send the cancel command to the handler.
    channel.writeAndFlush(new CancelStreamCommand(this));
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
    if (state() == CLOSED) {
      promise.setSuccess();
      return;
    }

    // Retain the ByteBuf until it is released by the deframer.
    deframer.deliverFrame(frame.retain(), endOfStream);

    // TODO(user): add flow control.
    promise.setSuccess();
  }

  @Override
  protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
    SendGrpcFrameCommand cmd =
        new SendGrpcFrameCommand(this, toByteBuf(frame), endOfStream, endOfStream);
    channel.writeAndFlush(cmd);
  }

  /**
   * Copies the content of the given {@link ByteBuffer} to a new {@link ByteBuf} instance.
   */
  private ByteBuf toByteBuf(ByteBuffer source) {
    ByteBuf buf = channel.alloc().buffer(source.remaining());
    buf.writeBytes(source);
    return buf;
  }
}
