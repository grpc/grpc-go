package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufHolder;
import io.netty.buffer.DefaultByteBufHolder;

/**
 * Command sent from the transport to the Netty channel to send a GRPC frame to the remote endpoint.
 */
class SendGrpcFrameCommand extends DefaultByteBufHolder {
  private final NettyClientStream stream;
  private final boolean endStream;
  private final boolean endSegment;

  SendGrpcFrameCommand(NettyClientStream stream, ByteBuf content, boolean endStream,
      boolean endSegment) {
    super(content);
    this.stream = Preconditions.checkNotNull(stream, "stream");
    this.endStream = endStream;
    this.endSegment = endSegment;
  }

  NettyClientStream stream() {
    return stream;
  }

  boolean endStream() {
    return endStream;
  }

  boolean endSegment() {
    return endSegment;
  }

  @Override
  public ByteBufHolder copy() {
    return new SendGrpcFrameCommand(stream, content().copy(), endStream, endSegment);
  }

  @Override
  public ByteBufHolder duplicate() {
    return new SendGrpcFrameCommand(stream, content().duplicate(), endStream, endSegment);
  }

  @Override
  public SendGrpcFrameCommand retain() {
    super.retain();
    return this;
  }

  @Override
  public SendGrpcFrameCommand retain(int increment) {
    super.retain(increment);
    return this;
  }

  @Override
  public SendGrpcFrameCommand touch() {
    super.touch();
    return this;
  }

  @Override
  public SendGrpcFrameCommand touch(Object hint) {
    super.touch(hint);
    return this;
  }
}
