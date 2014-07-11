package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.MethodDescriptor;

/**
 * A command to create a new stream. This is created by {@link NettyClientStream} and passed to the
 * {@link NettyClientHandler} for processing in the Channel thread.
 */
class CreateStreamCommand {
  final MethodDescriptor<?, ?> method;
  final NettyClientStream stream;

  CreateStreamCommand(MethodDescriptor<?, ?> method, NettyClientStream stream) {
    this.method = Preconditions.checkNotNull(method, "method");
    this.stream = Preconditions.checkNotNull(stream, "stream");
  }

  MethodDescriptor<?, ?> method() {
    return method;
  }

  NettyClientStream stream() {
    return stream;
  }
}
