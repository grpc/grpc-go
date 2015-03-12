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
import com.google.common.util.concurrent.AbstractService;

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

import java.net.SocketAddress;

import javax.annotation.Nullable;

/**
 * Implementation of the {@link com.google.common.util.concurrent.Service} interface for a
 * Netty-based server.
 */
public class NettyServer extends AbstractService {
  private final SocketAddress address;
  private final Class<? extends ServerChannel> channelType;
  private final ChannelInitializer<Channel> channelInitializer;
  private final EventLoopGroup bossGroup;
  private final EventLoopGroup workerGroup;
  private Channel channel;

  public NettyServer(ServerListener serverListener, SocketAddress address,
      Class<? extends ServerChannel> channelType, EventLoopGroup bossGroup,
      EventLoopGroup workerGroup) {
    this(serverListener, address, channelType, bossGroup, workerGroup, null);
  }

  public NettyServer(final ServerListener serverListener, SocketAddress address,
      Class<? extends ServerChannel> channelType, EventLoopGroup bossGroup,
      EventLoopGroup workerGroup, @Nullable final SslContext sslContext) {
    this.address = address;
    this.channelType = Preconditions.checkNotNull(channelType, "channelType");
    this.bossGroup = Preconditions.checkNotNull(bossGroup, "bossGroup");
    this.workerGroup = Preconditions.checkNotNull(workerGroup, "workerGroup");
    this.channelInitializer = new ChannelInitializer<Channel>() {
      @Override
      public void initChannel(Channel ch) throws Exception {
        NettyServerTransport transport = new NettyServerTransport(ch, serverListener, sslContext);
        transport.startAsync();
        // TODO(nmittler): Should we wait for transport shutdown before shutting down server?
      }
    };
  }

  @Override
  protected void doStart() {
    ServerBootstrap b = new ServerBootstrap();
    b.group(bossGroup, workerGroup);
    b.channel(channelType);
    if (NioServerSocketChannel.class.isAssignableFrom(channelType)) {
      b.option(SO_BACKLOG, 128);
      b.childOption(SO_KEEPALIVE, true);
    }
    b.childHandler(channelInitializer);

    // Bind and start to accept incoming connections.
    b.bind(address).addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (future.isSuccess()) {
          channel = future.channel();
          notifyStarted();
        } else {
          notifyFailed(future.cause());
        }
      }
    });
  }

  @Override
  protected void doStop() {
    // Wait for the channel to close.
    if (channel != null && channel.isOpen()) {
      channel.close().addListener(new ChannelFutureListener() {
        @Override
        public void operationComplete(ChannelFuture future) throws Exception {
          if (future.isSuccess()) {
            notifyStopped();
          } else {
            notifyFailed(future.cause());
          }
        }
      });
    }
  }
}
