package com.google.net.stubby.newtransport.netty;

import com.google.net.stubby.newtransport.Stream;

import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelPromise;

/**
 * A common interface shared between NettyClientStream and NettyServerStream.
 */
interface NettyStream extends Stream {

  /**
   * Called in the network thread to process the content of an inbound DATA frame.
   *
   * @param frame the inbound HTTP/2 DATA frame. If this buffer is not used immediately, it must
   *              be retained.
   * @param promise the promise to be set after the application has finished
   *                processing the frame.
   */
  void inboundDataReceived(ByteBuf frame, boolean endOfStream, ChannelPromise promise);

  /**
   * Returns the HTTP/2 stream ID.
   */
  int id();
}
