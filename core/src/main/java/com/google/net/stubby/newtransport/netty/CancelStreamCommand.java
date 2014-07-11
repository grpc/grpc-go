package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;

/**
 * Command sent from a Netty client stream to the handler to cancel the stream.
 */
class CancelStreamCommand {
  private final NettyClientStream stream;

  CancelStreamCommand(NettyClientStream stream) {
    this.stream = Preconditions.checkNotNull(stream, "stream");
  }

  NettyClientStream stream() {
    return stream;
  }
}
