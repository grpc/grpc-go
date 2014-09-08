package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.MethodDescriptor;

/**
 * A command to create a new stream. This is created by {@link NettyClientStream} and passed to the
 * {@link NettyClientHandler} for processing in the Channel thread.
 */
class CreateStreamCommand {
  private final MethodDescriptor<?, ?> method;
  private final String[] headers;
  private final NettyClientStream stream;

  CreateStreamCommand(MethodDescriptor<?, ?> method, String[] headers,
                      NettyClientStream stream) {
    this.method = Preconditions.checkNotNull(method, "method");
    this.stream = Preconditions.checkNotNull(stream, "stream");
    this.headers = Preconditions.checkNotNull(headers, "headers");
  }

  MethodDescriptor<?, ?> method() {
    return method;
  }

  NettyClientStream stream() {
    return stream;
  }

  String[] headers() {
    return headers;
  }
}
