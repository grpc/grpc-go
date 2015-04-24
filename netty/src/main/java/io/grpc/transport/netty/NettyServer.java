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

import static io.netty.channel.ChannelOption.SO_BACKLOG;
import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;

import io.grpc.transport.Server;
import io.grpc.transport.ServerListener;
import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.ServerChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.handler.ssl.SslContext;

import java.io.IOException;
import java.net.SocketAddress;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;

/**
 * Netty-based server implementation.
 */
public class NettyServer implements Server {
  private static final Logger log = Logger.getLogger(Server.class.getName());

  private final SocketAddress address;
  private final Class<? extends ServerChannel> channelType;
  private final EventLoopGroup bossGroup;
  private final EventLoopGroup workerGroup;
  private final SslContext sslContext;
  private final int maxStreamsPerConnection;
  private ServerListener listener;
  private Channel channel;
  private int connectionWindowSize;
  private int streamWindowSize;

  NettyServer(SocketAddress address, Class<? extends ServerChannel> channelType,
      EventLoopGroup bossGroup, EventLoopGroup workerGroup, int maxStreamsPerConnection,
      int connectionWindowSize, int streamWindowSize) {
    this(address, channelType, bossGroup, workerGroup, null, maxStreamsPerConnection,
         connectionWindowSize, streamWindowSize);
  }

  NettyServer(SocketAddress address, Class<? extends ServerChannel> channelType,
      EventLoopGroup bossGroup, EventLoopGroup workerGroup, @Nullable SslContext sslContext,
      int maxStreamsPerConnection, int connectionWindowSize, int streamWindowSize) {
    this.address = address;
    this.channelType = Preconditions.checkNotNull(channelType, "channelType");
    this.bossGroup = Preconditions.checkNotNull(bossGroup, "bossGroup");
    this.workerGroup = Preconditions.checkNotNull(workerGroup, "workerGroup");
    this.sslContext = sslContext;
    this.maxStreamsPerConnection = maxStreamsPerConnection;
    this.connectionWindowSize = connectionWindowSize;
    this.streamWindowSize = streamWindowSize;
  }

  @Override
  public void start(ServerListener serverListener) throws IOException {
    listener = serverListener;
    ServerBootstrap b = new ServerBootstrap();
    b.group(bossGroup, workerGroup);
    b.channel(channelType);
    if (NioServerSocketChannel.class.isAssignableFrom(channelType)) {
      b.option(SO_BACKLOG, 128);
      b.childOption(SO_KEEPALIVE, true);
    }
    b.childHandler(new ChannelInitializer<Channel>() {
      @Override
      public void initChannel(Channel ch) throws Exception {
        NettyServerTransport transport
            = new NettyServerTransport(ch, sslContext, maxStreamsPerConnection,
                                       connectionWindowSize, streamWindowSize);
        transport.start(listener.transportCreated(transport));
      }
    });

    // Bind and start to accept incoming connections.
    ChannelFuture future = b.bind(address);
    try {
      future.await();
    } catch (InterruptedException ex) {
      Thread.currentThread().interrupt();
      throw new RuntimeException("Interrupted waiting for bind");
    }
    if (!future.isSuccess()) {
      throw new IOException("Failed to bind", future.cause());
    }
    channel = future.channel();
  }

  @Override
  public void shutdown() {
    if (channel == null || !channel.isOpen()) {
      return;
    }
    channel.close().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          log.log(Level.WARNING, "Error shutting down server", future.cause());
        }
        listener.serverShutdown();
      }
    });
  }
}
