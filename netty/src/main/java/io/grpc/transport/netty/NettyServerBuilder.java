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
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.Service;

import io.grpc.AbstractServerBuilder;
import io.grpc.HandlerRegistry;
import io.grpc.SharedResourceHolder;
import io.grpc.transport.ServerListener;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.ServerChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.handler.ssl.SslContext;
import io.grpc.ServerImpl;

import java.net.InetSocketAddress;
import java.net.SocketAddress;

/**
 * A builder to help simplify the construction of a Netty-based GRPC server.
 */
public final class NettyServerBuilder extends AbstractServerBuilder<NettyServerBuilder> {

  private final SocketAddress address;
  private Class<? extends ServerChannel> channelType = NioServerSocketChannel.class;
  private EventLoopGroup userBossEventLoopGroup;
  private EventLoopGroup userWorkerEventLoopGroup;
  private SslContext sslContext;

  /**
   * Creates a server builder that will bind to the given port.
   *
   * @param port the port on which the server is to be bound.
   * @return the server builder.
   */
  public static NettyServerBuilder forPort(int port) {
    return new NettyServerBuilder(port);
  }

  /**
   * Creates a server builder that will bind to the given port and use the {@link HandlerRegistry}
   * for call dispatching.
   *
   * @param registry the registry of handlers used for dispatching incoming calls.
   * @param port the port on which to the server is to be bound.
   * @return the server builder.
   */
  public static NettyServerBuilder forRegistryAndPort(HandlerRegistry registry, int port) {
    return new NettyServerBuilder(registry, port);
  }

  /**
   * Creates a server builder configured with the given {@link SocketAddress}.
   *
   * @param address the socket address on which the server is to be bound.
   * @return the server builder
   */
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
   * Specify the channel type to use, by default we use {@link NioServerSocketChannel}.
   */
  public NettyServerBuilder channelType(Class<? extends ServerChannel> channelType) {
    this.channelType = Preconditions.checkNotNull(channelType);
    return this;
  }

  /**
   * Provides the boss EventGroupLoop to the server.
   *
   * <p>It's an optional parameter. If the user has not provided one when the server is built, the
   * builder will use the default one which is static.
   *
   * <p>The server won't take ownership of the given EventLoopGroup. It's caller's responsibility
   * to shut it down when it's desired.
   *
   * <p>Grpc uses non-daemon {@link Thread}s by default and thus a {@link ServerImpl} will
   * continue to run even after the main thread has terminated. However, users have to be cautious
   * when providing their own {@link EventLoopGroup}s.
   * For example, Netty's {@link EventLoopGroup}s use daemon threads by default
   * and thus an application with only daemon threads running besides the main thread will exit as
   * soon as the main thread completes.
   * A simple solution to this problem is to call {@link ServerImpl#awaitTerminated()} to
   * keep the main thread alive until the server has terminated.
   */
  public NettyServerBuilder bossEventLoopGroup(EventLoopGroup group) {
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
   *
   * <p>Grpc uses non-daemon {@link Thread}s by default and thus a {@link ServerImpl} will
   * continue to run even after the main thread has terminated. However, users have to be cautious
   * when providing their own {@link EventLoopGroup}s.
   * For example, Netty's {@link EventLoopGroup}s use daemon threads by default
   * and thus an application with only daemon threads running besides the main thread will exit as
   * soon as the main thread completes.
   * A simple solution to this problem is to call {@link ServerImpl#awaitTerminated()} to
   * keep the main thread alive until the server has terminated.
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
    NettyServer server = new NettyServer(serverListener, address, channelType, bossEventLoopGroup,
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

  private abstract static class ClosureHook extends Service.Listener {
    protected abstract void onClosed();

    @Override
    public void terminated(Service.State from) {
      onClosed();
    }

    @Override
    public void failed(Service.State from, Throwable failure) {
      onClosed();
    }
  }
}
