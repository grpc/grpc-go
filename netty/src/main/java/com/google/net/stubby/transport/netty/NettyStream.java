package com.google.net.stubby.transport.netty;

import com.google.net.stubby.transport.Stream;

import io.netty.buffer.ByteBuf;

/**
 * A common interface shared between NettyClientStream and NettyServerStream.
 */
interface NettyStream extends Stream {

  /**
   * Called in the network thread to process the content of an inbound DATA frame.
   *
   * @param frame the inbound HTTP/2 DATA frame. If this buffer is not used immediately, it must
   *              be retained.
   */
  void inboundDataReceived(ByteBuf frame, boolean endOfStream);

  /**
   * Returns the HTTP/2 stream ID.
   */
  int id();
}
