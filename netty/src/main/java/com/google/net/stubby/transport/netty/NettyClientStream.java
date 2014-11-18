package com.google.net.stubby.transport.netty;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.net.stubby.transport.ClientStreamListener;
import com.google.net.stubby.transport.Http2ClientStream;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.handler.codec.http2.Http2Headers;

import java.nio.ByteBuffer;

/**
 * Client stream for a Netty transport.
 */
class NettyClientStream extends Http2ClientStream {

  private final Channel channel;
  private final NettyClientHandler handler;

  NettyClientStream(ClientStreamListener listener, Channel channel, NettyClientHandler handler) {
    super(listener, new NettyDecompressor(channel.alloc()), channel.eventLoop());
    this.channel = checkNotNull(channel, "channel");
    this.handler = checkNotNull(handler, "handler");
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
  protected void returnProcessedBytes(int processedBytes) {
    handler.returnProcessedBytes(id(), processedBytes);
  }
}
