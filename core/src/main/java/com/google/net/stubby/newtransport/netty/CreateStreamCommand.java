package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;

import io.netty.handler.codec.http2.Http2Headers;

/**
 * A command to create a new stream. This is created by {@link NettyClientStream} and passed to the
 * {@link NettyClientHandler} for processing in the Channel thread.
 */
class CreateStreamCommand {
  private final Http2Headers headers;
  private final NettyClientStream stream;

  CreateStreamCommand(Http2Headers headers,
                      NettyClientStream stream) {
    this.stream = Preconditions.checkNotNull(stream, "stream");
    this.headers = Preconditions.checkNotNull(headers, "headers");
  }

  NettyClientStream stream() {
    return stream;
  }

  Http2Headers headers() {
    return headers;
  }
}
