package com.google.net.stubby.transport.netty;

import static io.netty.channel.ChannelOption.SO_BACKLOG;
import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.transport.ServerListener;

import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.handler.ssl.SslContext;

import javax.annotation.Nullable;

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

  public NettyServer(ServerListener serverListener, int port, EventLoopGroup bossGroup,
      EventLoopGroup workerGroup) {
    this(serverListener, port, bossGroup, workerGroup, null);
  }

  public NettyServer(final ServerListener serverListener, int port, EventLoopGroup bossGroup,
      EventLoopGroup workerGroup, @Nullable final SslContext sslContext) {
    Preconditions.checkNotNull(bossGroup, "bossGroup");
    Preconditions.checkNotNull(workerGroup, "workerGroup");
    Preconditions.checkArgument(port >= 0, "port must be positive");
    this.port = port;
    this.channelInitializer = new ChannelInitializer<SocketChannel>() {
      @Override
      public void initChannel(SocketChannel ch) throws Exception {
        NettyServerTransport transport = new NettyServerTransport(ch, serverListener, sslContext);
        transport.startAsync();
        // TODO(user): Should we wait for transport shutdown before shutting down server?
      }
    };
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
  }
}
