/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.netty;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufHolder;
import io.netty.buffer.DefaultByteBufHolder;
import io.netty.channel.Channel;
import io.netty.channel.ChannelPromise;

/**
 * Command sent from the transport to the Netty channel to send a GRPC frame to the remote endpoint.
 */
class SendGrpcFrameCommand extends DefaultByteBufHolder implements WriteQueue.QueuedCommand {
  private final StreamIdHolder stream;
  private final boolean endStream;

  private ChannelPromise promise;

  SendGrpcFrameCommand(StreamIdHolder stream, ByteBuf content, boolean endStream) {
    super(content);
    this.stream = stream;
    this.endStream = endStream;
  }

  int streamId() {
    return stream.id();
  }

  boolean endStream() {
    return endStream;
  }

  @Override
  public ByteBufHolder copy() {
    return new SendGrpcFrameCommand(stream, content().copy(), endStream);
  }

  @Override
  public ByteBufHolder duplicate() {
    return new SendGrpcFrameCommand(stream, content().duplicate(), endStream);
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
    return thatCmd.stream.equals(stream) && thatCmd.endStream == endStream
        && thatCmd.content().equals(content());
  }

  @Override
  public String toString() {
    return getClass().getSimpleName() + "(streamId=" + streamId()
        + ", endStream=" + endStream + ", content=" + content()
        + ")";
  }

  @Override
  public int hashCode() {
    int hash = content().hashCode();
    hash = hash * 31 + stream.hashCode();
    if (endStream) {
      hash = -hash;
    }
    return hash;
  }

  @Override
  public ChannelPromise promise() {
    return promise;
  }

  @Override
  public void promise(ChannelPromise promise) {
    this.promise = promise;
  }

  @Override
  public final void run(Channel channel) {
    channel.write(this, promise);
  }
}
