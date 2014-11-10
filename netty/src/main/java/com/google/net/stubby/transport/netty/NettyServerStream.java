package com.google.net.stubby.transport.netty;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.transport.AbstractServerStream;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.Http2Headers;

import java.nio.ByteBuffer;

/**
 * Server stream for a Netty HTTP2 transport
 */
class NettyServerStream extends AbstractServerStream<Integer> {

  private final Channel channel;
  private final WindowUpdateManager windowUpdateManager;

  NettyServerStream(Channel channel, int id, DefaultHttp2InboundFlowController inboundFlow) {
    super(id, new NettyDecompressor(channel.alloc()), channel.eventLoop());
    this.channel = Preconditions.checkNotNull(channel, "channel is null");
    windowUpdateManager =
        new WindowUpdateManager(channel, Preconditions.checkNotNull(inboundFlow, "inboundFlow"));
    windowUpdateManager.streamId(id());
  }

  void inboundDataReceived(ByteBuf frame, boolean endOfStream) {
    super.inboundDataReceived(new NettyBuffer(frame.retain()), endOfStream);
  }

  @Override
  protected void internalSendHeaders(Metadata.Headers headers) {
    channel.writeAndFlush(new SendResponseHeadersCommand(id(),
        Utils.convertServerHeaders(headers), false));
  }

  @Override
  protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
    SendGrpcFrameCommand cmd =
        new SendGrpcFrameCommand(id(), Utils.toByteBuf(channel.alloc(), frame), endOfStream);
    channel.writeAndFlush(cmd);
  }

  @Override
  protected void sendTrailers(Metadata.Trailers trailers, boolean headersSent) {
    Http2Headers http2Trailers = Utils.convertTrailers(trailers, headersSent);
    channel.writeAndFlush(new SendResponseHeadersCommand(id(), http2Trailers, true));
  }

  @Override
  protected void disableWindowUpdate(ListenableFuture<Void> processingFuture) {
    windowUpdateManager.disableWindowUpdate(processingFuture);
  }
}
