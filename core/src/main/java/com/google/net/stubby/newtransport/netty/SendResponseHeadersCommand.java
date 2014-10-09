package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;

import io.netty.handler.codec.http2.Http2Headers;

/**
 * Command sent from the transport to the Netty channel to send response headers to the client.
 */
class SendResponseHeadersCommand {
  private final int streamId;
  private final Http2Headers headers;
  private final boolean endOfStream;

  SendResponseHeadersCommand(int streamId, Http2Headers headers, boolean endOfStream) {
    this.streamId = streamId;
    this.headers = Preconditions.checkNotNull(headers);
    this.endOfStream = endOfStream;
  }

  int streamId() {
    return streamId;
  }

  Http2Headers headers() {
    return headers;
  }

  boolean endOfStream() {
    return endOfStream;
  }

  @Override
  public boolean equals(Object that) {
    if (that == null || !that.getClass().equals(SendResponseHeadersCommand.class)) {
      return false;
    }
    SendResponseHeadersCommand thatCmd = (SendResponseHeadersCommand) that;
    return thatCmd.streamId == streamId
        && thatCmd.headers.equals(headers)
        && thatCmd.endOfStream == endOfStream;
  }

  @Override
  public String toString() {
    return getClass().getSimpleName() + "(streamId=" + streamId + ", headers=" + headers
        + ", endOfStream=" + endOfStream + ")";
  }

  @Override
  public int hashCode() {
    return streamId;
  }
}
