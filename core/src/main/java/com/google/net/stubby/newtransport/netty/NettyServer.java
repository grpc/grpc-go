package com.google.net.stubby.newtransport.netty;

import static io.netty.channel.ChannelOption.SO_BACKLOG;
import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;

import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;

/**
 * Implementation of the {@link com.google.common.util.concurrent.Service} interface for a
 * Netty-based server.
 */
public class NettyServer extends AbstractService {
  private final int port;
  private final ChannelInitializer<SocketChannel> channelInitializer;
  private final EventLoopGroup bossGroup;
  private final EventLoopGroup workerGroup;
  private Channel channel;

  public NettyServer(int port, ChannelInitializer<SocketChannel> channelInitializer) {
    this(port, channelInitializer, new NioEventLoopGroup(), new NioEventLoopGroup());
  }

  public NettyServer(int port, ChannelInitializer<SocketChannel> channelInitializer,
      EventLoopGroup bossGroup, EventLoopGroup workerGroup) {
    Preconditions.checkNotNull(channelInitializer, "channelInitializer");
    Preconditions.checkNotNull(bossGroup, "bossGroup");
    Preconditions.checkNotNull(workerGroup, "workerGroup");
    Preconditions.checkArgument(port >= 0, "port must be positive");
    this.port = port;
    this.channelInitializer = channelInitializer;
    this.bossGroup = bossGroup;
    this.workerGroup = workerGroup;
  }

  @Override
  protected void doStart() {
    ServerBootstrap b = new ServerBootstrap();
    b.group(bossGroup, workerGroup);
    b.channel(NioServerSocketChannel.class);
    b.option(SO_BACKLOG, 128);
    b.childOption(SO_KEEPALIVE, true);
    b.childHandler(channelInitializer);

    // Bind and start to accept incoming connections.
    b.bind(port).addListener(new ChannelFutureListener() {
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

    // Wait for the event loop group to shutdown.
    if (bossGroup != null) {
      bossGroup.shutdownGracefully();
    }
    if (workerGroup != null) {
      workerGroup.shutdownGracefully();
    }
  }
}
