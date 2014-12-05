package com.google.net.stubby.transport.netty;

import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.Service;
import com.google.net.stubby.AbstractServerBuilder;
import com.google.net.stubby.HandlerRegistry;
import com.google.net.stubby.SharedResourceHolder;
import com.google.net.stubby.transport.ServerListener;

import java.net.InetSocketAddress;
import java.net.SocketAddress;

import io.netty.channel.EventLoopGroup;
import io.netty.handler.ssl.SslContext;

/**
 * The convenient builder for a netty-based GRPC server.
 */
public final class NettyServerBuilder extends AbstractServerBuilder<NettyServerBuilder> {

  private final SocketAddress address;

  private EventLoopGroup userBossEventLoopGroup;
  private EventLoopGroup userWorkerEventLoopGroup;
  private SslContext sslContext;

  public static NettyServerBuilder forPort(int port) {
    return new NettyServerBuilder(port);
  }

  public static NettyServerBuilder forRegistryAndPort(HandlerRegistry registry, int port) {
    return new NettyServerBuilder(registry, port);
  }

  public static NettyServerBuilder forAddress(SocketAddress address) {
    return new NettyServerBuilder(address);
  }

  private NettyServerBuilder(int port) {
    this.address = new InetSocketAddress(port);
  }

  private NettyServerBuilder(HandlerRegistry registry, int port) {
    super(registry);
    this.address = new InetSocketAddress(port);
  }

  private NettyServerBuilder(SocketAddress address) {
    this.address = address;
  }

  /**
   * Provides the boss EventGroupLoop to the server.
   *
   * <p>It's an optional parameter. If the user has not provided one when the server is built, the
   * builder will use the default one which is static.
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

  /**
   * Sets the TLS context to use for encryption. Providing a context enables encryption.
   */
  public NettyServerBuilder sslContext(SslContext sslContext) {
    this.sslContext = sslContext;
    return this;
  }

  @Override
  protected Service buildTransportServer(ServerListener serverListener) {
    final EventLoopGroup bossEventLoopGroup  = (userBossEventLoopGroup == null)
        ? SharedResourceHolder.get(Utils.DEFAULT_BOSS_EVENT_LOOP_GROUP) : userBossEventLoopGroup;
    final EventLoopGroup workerEventLoopGroup = (userWorkerEventLoopGroup == null)
        ? SharedResourceHolder.get(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP)
        : userWorkerEventLoopGroup;
    NettyServer server = new NettyServer(serverListener, address, bossEventLoopGroup,
        workerEventLoopGroup, sslContext);
    if (userBossEventLoopGroup == null) {
      server.addListener(new ClosureHook() {
        @Override
        protected void onClosed() {
          SharedResourceHolder.release(Utils.DEFAULT_BOSS_EVENT_LOOP_GROUP, bossEventLoopGroup);
        }
      }, MoreExecutors.directExecutor());
    }
    if (userWorkerEventLoopGroup == null) {
      server.addListener(new ClosureHook() {
        @Override
        protected void onClosed() {
          SharedResourceHolder.release(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP, workerEventLoopGroup);
        }
      }, MoreExecutors.directExecutor());
    }
    return server;
  }
}
