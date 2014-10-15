package com.google.net.stubby.newtransport.netty;

import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.Service;
import com.google.net.stubby.AbstractServerBuilder;
import com.google.net.stubby.HandlerRegistry;
import com.google.net.stubby.newtransport.ServerListener;

import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;

/**
 * The convenient builder for a netty-based GRPC server.
 */
public final class NettyServerBuilder extends AbstractServerBuilder<NettyServerBuilder> {

  private final int port;

  private EventLoopGroup userBossEventLoopGroup;
  private EventLoopGroup userWorkerEventLoopGroup;

  public static NettyServerBuilder forPort(int port) {
    return new NettyServerBuilder(port);
  }

  public static NettyServerBuilder forRegistryAndPort(HandlerRegistry registry, int port) {
    return new NettyServerBuilder(registry, port);
  }

  private NettyServerBuilder(int port) {
    this.port = port;
  }

  private NettyServerBuilder(HandlerRegistry registry, int port) {
    super(registry);
    this.port = port;
  }

  /**
   * Provides the boss EventGroupLoop to the server.
   *
   * <p>It's an optional parameter. If the user has not provided one when the server is built, the
   * builder will create one.
   *
   * <p>The server won't take ownership of the given EventLoopGroup. It's caller's responsibility
   * to shut it down when it's desired.
   */
  public NettyServerBuilder userBossEventLoopGroup(EventLoopGroup group) {
    this.userBossEventLoopGroup = group;
    return this;
  }

  /**
   * Provides the worker EventGroupLoop to the server.
   *
   * <p>It's an optional parameter. If the user has not provided one when the server is built, the
   * builder will create one.
   *
   * <p>The server won't take ownership of the given EventLoopGroup. It's caller's responsibility
   * to shut it down when it's desired.
   */
  public NettyServerBuilder workerEventLoopGroup(EventLoopGroup group) {
    this.userWorkerEventLoopGroup = group;
    return this;
  }

  @Override
  protected Service buildTransportServer(ServerListener serverListener) {
    final EventLoopGroup bossEventLoopGroup  = (userBossEventLoopGroup == null)
        ? new NioEventLoopGroup() : userBossEventLoopGroup;
    final EventLoopGroup workerEventLoopGroup = (userWorkerEventLoopGroup == null)
        ? new NioEventLoopGroup() : userWorkerEventLoopGroup;
    NettyServer server = new NettyServer(serverListener, port);
    if (userBossEventLoopGroup == null) {
      server.addListener(new ClosureHook() {
        @Override
        protected void onClosed() {
          bossEventLoopGroup.shutdownGracefully();
        }
      }, MoreExecutors.directExecutor());
    }
    if (userWorkerEventLoopGroup == null) {
      server.addListener(new ClosureHook() {
        @Override
        protected void onClosed() {
          workerEventLoopGroup.shutdownGracefully();
        }
      }, MoreExecutors.directExecutor());
    }
    return server;
  }
}
