package com.google.net.stubby.transport.netty;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufHolder;
import io.netty.buffer.DefaultByteBufHolder;

/**
 * Command sent from the transport to the Netty channel to send a GRPC frame to the remote endpoint.
 */
class SendGrpcFrameCommand extends DefaultByteBufHolder {
  private final int streamId;
  private final boolean endStream;

  SendGrpcFrameCommand(int streamId, ByteBuf content, boolean endStream) {
    super(content);
    this.streamId = streamId;
    this.endStream = endStream;
  }

  int streamId() {
    return streamId;
  }

  boolean endStream() {
    return endStream;
  }

  @Override
  public ByteBufHolder copy() {
    return new SendGrpcFrameCommand(streamId, content().copy(), endStream);
  }

  @Override
  public ByteBufHolder duplicate() {
    return new SendGrpcFrameCommand(streamId, content().duplicate(), endStream);
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

  @Override
  public boolean equals(Object that) {
    if (that == null || !that.getClass().equals(SendGrpcFrameCommand.class)) {
      return false;
    }
    SendGrpcFrameCommand thatCmd = (SendGrpcFrameCommand) that;
    return thatCmd.streamId == streamId && thatCmd.endStream == endStream
        && thatCmd.content().equals(content());
  }

  @Override
  public String toString() {
    return getClass().getSimpleName() + "(streamId=" + streamId
        + ", endStream=" + endStream + ", content=" + content()
        + ")";
  }

  @Override
  public int hashCode() {
    int hash = content().hashCode();
    hash = hash * 31 + streamId;
    if (endStream) {
      hash = -hash;
    }
    return hash;
  }
}
