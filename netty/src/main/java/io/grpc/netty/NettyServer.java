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

import static com.google.common.base.Preconditions.checkNotNull;
import static io.grpc.netty.NettyServerBuilder.MAX_CONNECTION_AGE_NANOS_DISABLED;
import static io.netty.channel.ChannelOption.SO_BACKLOG;
import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import io.grpc.ServerStreamTracer;
import io.grpc.internal.InternalServer;
import io.grpc.internal.ServerListener;
import io.grpc.internal.ServerTransportListener;
import io.grpc.internal.SharedResourceHolder;
import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.ServerChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.util.AbstractReferenceCounted;
import io.netty.util.ReferenceCounted;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Netty-based server implementation.
 */
class NettyServer implements InternalServer {
  private static final Logger log = Logger.getLogger(InternalServer.class.getName());

  private final SocketAddress address;
  private final Class<? extends ServerChannel> channelType;
  private final ProtocolNegotiator protocolNegotiator;
  private final int maxStreamsPerConnection;
  private final boolean usingSharedBossGroup;
  private final boolean usingSharedWorkerGroup;
  private EventLoopGroup bossGroup;
  private EventLoopGroup workerGroup;
  private ServerListener listener;
  private Channel channel;
  private final int flowControlWindow;
  private final int maxMessageSize;
  private final int maxHeaderListSize;
  private final long keepAliveTimeInNanos;
  private final long keepAliveTimeoutInNanos;
  private final long maxConnectionIdleInNanos;
  private final long maxConnectionAgeInNanos;
  private final long maxConnectionAgeGraceInNanos;
  private final boolean permitKeepAliveWithoutCalls;
  private final long permitKeepAliveTimeInNanos;
  private final ReferenceCounted eventLoopReferenceCounter = new EventLoopReferenceCounter();
  private final List<ServerStreamTracer.Factory> streamTracerFactories;

  NettyServer(
      SocketAddress address, Class<? extends ServerChannel> channelType,
      @Nullable EventLoopGroup bossGroup, @Nullable EventLoopGroup workerGroup,
      ProtocolNegotiator protocolNegotiator, List<ServerStreamTracer.Factory> streamTracerFactories,
      int maxStreamsPerConnection, int flowControlWindow, int maxMessageSize, int maxHeaderListSize,
      long keepAliveTimeInNanos, long keepAliveTimeoutInNanos,
      long maxConnectionIdleInNanos,
      long maxConnectionAgeInNanos, long maxConnectionAgeGraceInNanos,
      boolean permitKeepAliveWithoutCalls, long permitKeepAliveTimeInNanos) {
    this.address = address;
    this.channelType = checkNotNull(channelType, "channelType");
    this.bossGroup = bossGroup;
    this.workerGroup = workerGroup;
    this.protocolNegotiator = checkNotNull(protocolNegotiator, "protocolNegotiator");
    this.streamTracerFactories = checkNotNull(streamTracerFactories, "streamTracerFactories");
    this.usingSharedBossGroup = bossGroup == null;
    this.usingSharedWorkerGroup = workerGroup == null;
    this.maxStreamsPerConnection = maxStreamsPerConnection;
    this.flowControlWindow = flowControlWindow;
    this.maxMessageSize = maxMessageSize;
    this.maxHeaderListSize = maxHeaderListSize;
    this.keepAliveTimeInNanos = keepAliveTimeInNanos;
    this.keepAliveTimeoutInNanos = keepAliveTimeoutInNanos;
    this.maxConnectionIdleInNanos = maxConnectionIdleInNanos;
    this.maxConnectionAgeInNanos = maxConnectionAgeInNanos;
    this.maxConnectionAgeGraceInNanos = maxConnectionAgeGraceInNanos;
    this.permitKeepAliveWithoutCalls = permitKeepAliveWithoutCalls;
    this.permitKeepAliveTimeInNanos = permitKeepAliveTimeInNanos;
  }

  @Override
  public int getPort() {
    if (channel == null) {
      return -1;
    }
    SocketAddress localAddr = channel.localAddress();
    if (!(localAddr instanceof InetSocketAddress)) {
      return -1;
    }
    return ((InetSocketAddress) localAddr).getPort();
  }

  @Override
  public void start(ServerListener serverListener) throws IOException {
    listener = checkNotNull(serverListener, "serverListener");

    // If using the shared groups, get references to them.
    allocateSharedGroups();

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

        long maxConnectionAgeInNanos = NettyServer.this.maxConnectionAgeInNanos;
        if (maxConnectionAgeInNanos != MAX_CONNECTION_AGE_NANOS_DISABLED) {
          // apply a random jitter of +/-10% to max connection age
          maxConnectionAgeInNanos =
              (long) ((.9D + Math.random() * .2D) * maxConnectionAgeInNanos);
        }

        NettyServerTransport transport =
            new NettyServerTransport(
                ch, protocolNegotiator, streamTracerFactories, maxStreamsPerConnection,
                flowControlWindow, maxMessageSize, maxHeaderListSize,
                keepAliveTimeInNanos, keepAliveTimeoutInNanos,
                maxConnectionIdleInNanos,
                maxConnectionAgeInNanos, maxConnectionAgeGraceInNanos,
                permitKeepAliveWithoutCalls, permitKeepAliveTimeInNanos);
        ServerTransportListener transportListener;
        // This is to order callbacks on the listener, not to guard access to channel.
        synchronized (NettyServer.this) {
          if (channel != null && !channel.isOpen()) {
            // Server already shutdown.
            ch.close();
            return;
          }
          // `channel` shutdown can race with `ch` initialization, so this is only safe to increment
          // inside the lock.
          eventLoopReferenceCounter.retain();
          transportListener = listener.transportCreated(transport);
        }
        transport.start(transportListener);
        ch.closeFuture().addListener(new ChannelFutureListener() {
          @Override
          public void operationComplete(ChannelFuture future) {
            eventLoopReferenceCounter.release();
          }
        });
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
      // Already closed.
      return;
    }
    channel.close().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          log.log(Level.WARNING, "Error shutting down server", future.cause());
        }
        synchronized (NettyServer.this) {
          listener.serverShutdown();
        }
        eventLoopReferenceCounter.release();
      }
    });
  }

  private void allocateSharedGroups() {
    if (bossGroup == null) {
      bossGroup = SharedResourceHolder.get(Utils.DEFAULT_BOSS_EVENT_LOOP_GROUP);
    }
    if (workerGroup == null) {
      workerGroup = SharedResourceHolder.get(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP);
    }
  }

  class EventLoopReferenceCounter extends AbstractReferenceCounted {
    @Override
    protected void deallocate() {
      try {
        if (usingSharedBossGroup && bossGroup != null) {
          SharedResourceHolder.release(Utils.DEFAULT_BOSS_EVENT_LOOP_GROUP, bossGroup);
        }
      } finally {
        bossGroup = null;
        try {
          if (usingSharedWorkerGroup && workerGroup != null) {
            SharedResourceHolder.release(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP, workerGroup);
          }
        } finally {
          workerGroup = null;
        }
      }
    }

    @Override
    public ReferenceCounted touch(Object hint) {
      return this;
    }
  }
}
