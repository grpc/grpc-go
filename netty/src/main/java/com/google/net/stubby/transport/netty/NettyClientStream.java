package com.google.net.stubby.transport.netty;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.transport.ClientStreamListener;
import com.google.net.stubby.transport.Http2ClientStream;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.Http2Headers;

import java.nio.ByteBuffer;

import javax.annotation.Nullable;

/**
 * Client stream for a Netty transport.
 */
class NettyClientStream extends Http2ClientStream {

  private final WindowUpdateManager windowUpdateManager;
  private final Channel channel;

  NettyClientStream(ClientStreamListener listener, Channel channel,
      DefaultHttp2InboundFlowController inboundFlow) {
    super(listener, new NettyDecompressor(channel.alloc()), channel.eventLoop());
    this.channel = Preconditions.checkNotNull(channel, "channel");
    windowUpdateManager = new WindowUpdateManager(channel, inboundFlow);
  }

  @Override
  public void id(Integer id) {
    super.id(id);
    // TODO(user): This is ugly, find a way to move into the constructor
    windowUpdateManager.streamId(id);
  }


  void transportHeadersReceived(Http2Headers headers, boolean endOfStream) {
    if (endOfStream) {
      transportTrailersReceived(Utils.convertTrailers(headers));
    } else {
      transportHeadersReceived(Utils.convertHeaders(headers));
    }
  }

  void transportDataReceived(ByteBuf frame, boolean endOfStream) {
    transportDataReceived(new NettyBuffer(frame.retain()), endOfStream);
  }

  @Override
  protected void sendCancel() {
    // Send the cancel command to the handler.
    channel.writeAndFlush(new CancelStreamCommand(this));
  }

  @Override
  protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
    SendGrpcFrameCommand cmd = new SendGrpcFrameCommand(id(),
        Utils.toByteBuf(channel.alloc(), frame), endOfStream);
    channel.writeAndFlush(cmd);
  }

  @Override
  protected void disableWindowUpdate(@Nullable ListenableFuture<Void> processingFuture) {
    windowUpdateManager.disableWindowUpdate(processingFuture);
  }
}
