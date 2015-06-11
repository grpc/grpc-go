/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.transport.netty;

import com.google.common.base.Preconditions;

import io.grpc.transport.ServerTransport;
import io.grpc.transport.ServerTransportListener;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2FrameLogger;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.handler.codec.http2.Http2OutboundFrameLogger;
import io.netty.handler.logging.LogLevel;
import io.netty.handler.ssl.SslContext;

import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.net.ssl.SSLEngine;

/**
 * The Netty-based server transport.
 */
class NettyServerTransport implements ServerTransport {
  private static final Logger log = Logger.getLogger(NettyServerTransport.class.getName());

  private final Channel channel;
  private final SslContext sslContext;
  private final int maxStreams;
  private ServerTransportListener listener;
  private boolean terminated;
  private int connectionWindowSize;
  private int streamWindowSize;

  NettyServerTransport(Channel channel, @Nullable SslContext sslContext, int maxStreams,
                       int connectionWindowSize, int streamWindowSize) {
    this.channel = Preconditions.checkNotNull(channel, "channel");
    this.sslContext = sslContext;
    this.maxStreams = maxStreams;
    this.connectionWindowSize = connectionWindowSize;
    this.streamWindowSize = streamWindowSize;
  }

  public void start(ServerTransportListener listener) {
    Preconditions.checkState(this.listener == null, "Handler already registered");
    this.listener = listener;

    // Create the Netty handler for the pipeline.
    final NettyServerHandler handler = createHandler(listener);

    // Notify when the channel closes.
    channel.closeFuture().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        notifyTerminated(handler.connectionError());
      }
    });

    if (sslContext != null) {
      SSLEngine sslEngine = sslContext.newEngine(channel.alloc());
      channel.pipeline().addLast(ProtocolNegotiators.serverTls(sslEngine));
    }
    channel.pipeline().addLast(handler);
  }

  @Override
  public void shutdown() {
    if (channel.isOpen()) {
      channel.close();
    }
  }

  /**
   * For testing purposes only.
   */
  Channel channel() {
    return channel;
  }

  private void notifyTerminated(Throwable t) {
    if (t != null) {
      log.log(Level.SEVERE, "Transport failed", t);
    }
    if (!terminated) {
      terminated = true;
      listener.transportTerminated();
    }
  }

  /**
   * Creates the Netty handler to be used in the channel pipeline.
   */
  private NettyServerHandler createHandler(ServerTransportListener transportListener) {
    Http2Connection connection = new DefaultHttp2Connection(true);
    Http2FrameLogger frameLogger = new Http2FrameLogger(LogLevel.DEBUG, getClass());
    Http2FrameReader frameReader =
        new Http2InboundFrameLogger(new DefaultHttp2FrameReader(), frameLogger);
    Http2FrameWriter frameWriter =
        new Http2OutboundFrameLogger(new DefaultHttp2FrameWriter(), frameLogger);

    return new NettyServerHandler(transportListener, connection, frameReader, frameWriter,
        maxStreams, connectionWindowSize, streamWindowSize);
  }
}
