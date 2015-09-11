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

package io.grpc.netty;

import static com.google.common.base.Preconditions.checkArgument;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;

import com.google.common.base.Preconditions;

import io.grpc.ExperimentalApi;
import io.grpc.HandlerRegistry;
import io.grpc.internal.AbstractServerImplBuilder;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.ServerChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.handler.ssl.SslContext;

import java.io.File;
import java.net.InetSocketAddress;
import java.net.SocketAddress;

import javax.annotation.Nullable;
import javax.net.ssl.SSLException;

/**
 * A builder to help simplify the construction of a Netty-based GRPC server.
 */
@ExperimentalApi("There is no plan to make this API stable, given transport API instability")
public final class NettyServerBuilder extends AbstractServerImplBuilder<NettyServerBuilder> {
  public static final int DEFAULT_FLOW_CONTROL_WINDOW = 1048576; // 1MiB

  private final SocketAddress address;
  private Class<? extends ServerChannel> channelType = NioServerSocketChannel.class;
  @Nullable
  private EventLoopGroup bossEventLoopGroup;
  @Nullable
  private EventLoopGroup workerEventLoopGroup;
  private SslContext sslContext;
  private int maxConcurrentCallsPerConnection = Integer.MAX_VALUE;
  private int flowControlWindow = DEFAULT_FLOW_CONTROL_WINDOW;
  private int maxMessageSize = DEFAULT_MAX_MESSAGE_SIZE;

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
   * <p>Grpc uses non-daemon {@link Thread}s by default and thus a {@link io.grpc.Server} will
   * continue to run even after the main thread has terminated. However, users have to be cautious
   * when providing their own {@link EventLoopGroup}s.
   * For example, Netty's {@link EventLoopGroup}s use daemon threads by default
   * and thus an application with only daemon threads running besides the main thread will exit as
   * soon as the main thread completes.
   * A simple solution to this problem is to call {@link io.grpc.Server#awaitTermination()} to
   * keep the main thread alive until the server has terminated.
   */
  public NettyServerBuilder bossEventLoopGroup(EventLoopGroup group) {
    this.bossEventLoopGroup = group;
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
   * <p>Grpc uses non-daemon {@link Thread}s by default and thus a {@link io.grpc.Server} will
   * continue to run even after the main thread has terminated. However, users have to be cautious
   * when providing their own {@link EventLoopGroup}s.
   * For example, Netty's {@link EventLoopGroup}s use daemon threads by default
   * and thus an application with only daemon threads running besides the main thread will exit as
   * soon as the main thread completes.
   * A simple solution to this problem is to call {@link io.grpc.Server#awaitTermination()} to
   * keep the main thread alive until the server has terminated.
   */
  public NettyServerBuilder workerEventLoopGroup(EventLoopGroup group) {
    this.workerEventLoopGroup = group;
    return this;
  }

  /**
   * Sets the TLS context to use for encryption. Providing a context enables encryption. It must
   * have been configured with {@link GrpcSslContexts}, but options could have been overridden.
   */
  public NettyServerBuilder sslContext(SslContext sslContext) {
    this.sslContext = sslContext;
    return this;
  }

  /**
   * The maximum number of concurrent calls permitted for each incoming connection. Defaults to no
   * limit.
   */
  public NettyServerBuilder maxConcurrentCallsPerConnection(int maxCalls) {
    Preconditions.checkArgument(maxCalls > 0, "max must be positive: %s", maxCalls);
    this.maxConcurrentCallsPerConnection = maxCalls;
    return this;
  }

  /**
   * Sets the HTTP/2 flow control window. If not called, the default value
   * is {@link #DEFAULT_FLOW_CONTROL_WINDOW}).
   */
  public NettyServerBuilder flowControlWindow(int flowControlWindow) {
    Preconditions.checkArgument(flowControlWindow > 0, "flowControlWindow must be positive");
    this.flowControlWindow = flowControlWindow;
    return this;
  }

  /**
   * Sets the maximum message size allowed to be received on the server. If not called,
   * defaults to 100 MiB.
   */
  public NettyServerBuilder maxMessageSize(int maxMessageSize) {
    checkArgument(maxMessageSize >= 0, "maxMessageSize must be >= 0");
    this.maxMessageSize = maxMessageSize;
    return this;
  }

  @Override
  protected NettyServer buildTransportServer() {
    return new NettyServer(address, channelType, bossEventLoopGroup,
            workerEventLoopGroup, sslContext, maxConcurrentCallsPerConnection, flowControlWindow,
            maxMessageSize);
  }

  @Override
  public NettyServerBuilder useTransportSecurity(File certChain, File privateKey) {
    try {
      sslContext = GrpcSslContexts.forServer(certChain, privateKey).build();
    } catch (SSLException e) {
      // This should likely be some other, easier to catch exception.
      throw new RuntimeException(e);
    }
    return this;
  }
}
