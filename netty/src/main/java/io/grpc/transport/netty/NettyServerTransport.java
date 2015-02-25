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
import com.google.common.util.concurrent.AbstractService;

import io.grpc.transport.ServerListener;
import io.grpc.transport.ServerTransportListener;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2LocalFlowController;
import io.netty.handler.codec.http2.DefaultHttp2StreamRemovalPolicy;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2FrameLogger;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.handler.codec.http2.Http2OutboundFrameLogger;
import io.netty.handler.codec.http2.Http2StreamRemovalPolicy;
import io.netty.handler.ssl.SslContext;
import io.netty.util.internal.logging.InternalLogLevel;

import javax.annotation.Nullable;

/**
 * The Netty-based server transport.
 */
class NettyServerTransport extends AbstractService {
  private static final Http2FrameLogger frameLogger = new Http2FrameLogger(InternalLogLevel.DEBUG); 
  private final Channel channel;
  private final ServerListener serverListener;
  private final SslContext sslContext;
  private NettyServerHandler handler;

  NettyServerTransport(Channel channel, ServerListener serverListener,
      @Nullable SslContext sslContext) {
    this.channel = Preconditions.checkNotNull(channel, "channel");
    this.serverListener = Preconditions.checkNotNull(serverListener, "serverListener");
    this.sslContext = sslContext;
  }

  @Override
  protected void doStart() {
    Preconditions.checkState(handler == null, "Handler already registered");

    // Notify the listener that this transport is being constructed.
    ServerTransportListener transportListener = serverListener.transportCreated(this);

    // Create the Netty handler for the pipeline.
    DefaultHttp2StreamRemovalPolicy streamRemovalPolicy = new DefaultHttp2StreamRemovalPolicy();
    handler = createHandler(transportListener, streamRemovalPolicy);

    // Notify when the channel closes.
    channel.closeFuture().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          // Close failed.
          notifyFailed(future.cause());
        } else if (handler.connectionError() != null) {
          // The handler encountered a connection error.
          notifyFailed(handler.connectionError());
        } else {
          // Normal termination of the connection.
          notifyStopped();
        }
      }
    });

    if (sslContext != null) {
      channel.pipeline().addLast(Http2Negotiator.serverTls(sslContext.newEngine(channel.alloc())));
    }
    channel.pipeline().addLast(streamRemovalPolicy);
    channel.pipeline().addLast(handler);

    notifyStarted();
  }

  @Override
  protected void doStop() {
    // No explicit call to notifyStopped() here, since this is automatically done when the
    // channel closes.
    if (channel.isOpen()) {
      channel.close();
    }
  }

  /**
   * Creates the Netty handler to be used in the channel pipeline.
   */
  private NettyServerHandler createHandler(ServerTransportListener transportListener,
      Http2StreamRemovalPolicy streamRemovalPolicy) {
    Http2Connection connection = new DefaultHttp2Connection(true, streamRemovalPolicy);
    Http2FrameReader frameReader =
        new Http2InboundFrameLogger(new DefaultHttp2FrameReader(), frameLogger);
    Http2FrameWriter frameWriter =
        new Http2OutboundFrameLogger(new DefaultHttp2FrameWriter(), frameLogger);

    DefaultHttp2LocalFlowController inboundFlow =
        new DefaultHttp2LocalFlowController(connection, frameWriter);
    return new NettyServerHandler(transportListener, connection, frameReader, frameWriter,
        inboundFlow);
  }
}
