package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.newtransport.AbstractServerStream;
import com.google.net.stubby.newtransport.GrpcDeframer;
import com.google.net.stubby.newtransport.StreamState;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;

import java.nio.ByteBuffer;

/**
 * Server stream for a Netty transport
 */
class NettyServerStream extends AbstractServerStream implements NettyStream {

  private final GrpcDeframer deframer;
  private final Channel channel;
  private final int id;
  private final WindowUpdateManager windowUpdateManager;

  private boolean headersSent;

  NettyServerStream(Channel channel, int id, DefaultHttp2InboundFlowController inboundFlow) {
    this.channel = Preconditions.checkNotNull(channel, "channel is null");
    this.id = id;
    deframer = new GrpcDeframer(new NettyDecompressor(channel.alloc()), inboundMessageHandler(),
        channel.eventLoop());
    windowUpdateManager =
        new WindowUpdateManager(channel, Preconditions.checkNotNull(inboundFlow, "inboundFlow"));
    windowUpdateManager.streamId(id);
  }

  @Override
  public void inboundDataReceived(ByteBuf frame, boolean endOfStream) {
    if (state() == StreamState.CLOSED) {
      return;
    }
    // Retain the ByteBuf until it is released by the deframer.
    // TODO(user): It sounds sub-optimal to deframe in the network thread. That means
    // decompression is serialized.
    deframer.deframe(new NettyBuffer(frame.retain()), endOfStream);
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

  @Override
  protected void disableWindowUpdate(ListenableFuture<Void> processingFuture) {
    windowUpdateManager.disableWindowUpdate(processingFuture);
  }
}
