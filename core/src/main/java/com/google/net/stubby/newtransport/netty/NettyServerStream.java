package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.newtransport.AbstractServerStream;
import com.google.net.stubby.newtransport.GrpcDeframer;
import com.google.net.stubby.newtransport.StreamState;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelPromise;

import java.nio.ByteBuffer;

/**
 * Server stream for a Netty transport
 */
class NettyServerStream extends AbstractServerStream implements NettyStream {

  private final GrpcDeframer deframer;
  private final Channel channel;
  private final int id;

  private boolean headersSent;

  NettyServerStream(Channel channel, int id) {
    this.channel = Preconditions.checkNotNull(channel, "channel is null");
    this.id = id;
    this.deframer = new GrpcDeframer(new NettyDecompressor(channel.alloc()),
        inboundMessageHandler());
  }

  @Override
  public void inboundDataReceived(ByteBuf frame, boolean endOfStream, ChannelPromise promise) {
    if (state() == StreamState.CLOSED) {
      promise.setSuccess();
      return;
    }
    // Retain the ByteBuf until it is released by the deframer.
    // TODO(user): It sounds sub-optimal to deframe in the network thread. That means
    // decompression is serialized.
    deframer.deframe(new NettyBuffer(frame.retain()), endOfStream);
    promise.setSuccess();
  }

  @Override
  protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
    if (!headersSent) {
      channel.write(new SendResponseHeadersCommand(id));
      headersSent = true;
    }
    SendGrpcFrameCommand cmd =
        new SendGrpcFrameCommand(id, Utils.toByteBuf(channel.alloc(), frame), endOfStream);
    channel.writeAndFlush(cmd);
  }

  @Override
  public int id() {
    return id;
  }
}
