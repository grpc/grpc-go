package com.google.net.stubby.transport.netty;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.transport.AbstractServerStream;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.handler.codec.http2.Http2Headers;

import java.nio.ByteBuffer;

/**
 * Server stream for a Netty HTTP2 transport
 */
class NettyServerStream extends AbstractServerStream<Integer> {

  private final Channel channel;
  private final NettyServerHandler handler;

  NettyServerStream(Channel channel, int id, NettyServerHandler handler) {
    super(id, new NettyDecompressor(channel.alloc()), channel.eventLoop());
    this.channel = checkNotNull(channel, "channel");
    this.handler = checkNotNull(handler, "handler");
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
  protected void returnProcessedBytes(int processedBytes) {
    handler.returnProcessedBytes(id(), processedBytes);
  }
}
