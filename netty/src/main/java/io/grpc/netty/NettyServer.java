/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.netty;

import static com.google.common.base.Preconditions.checkNotNull;
import static io.grpc.netty.NettyServerBuilder.MAX_CONNECTION_AGE_NANOS_DISABLED;
import static io.netty.channel.ChannelOption.SO_BACKLOG;
import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalInstrumented;
import io.grpc.InternalLogId;
import io.grpc.InternalWithLogId;
import io.grpc.ServerStreamTracer;
import io.grpc.internal.InternalServer;
import io.grpc.internal.ServerListener;
import io.grpc.internal.ServerTransportListener;
import io.grpc.internal.SharedResourceHolder;
import io.grpc.internal.TransportTracer;
import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.ChannelOption;
import io.netty.channel.ChannelPromise;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.ServerChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.util.AbstractReferenceCounted;
import io.netty.util.ReferenceCounted;
import io.netty.util.concurrent.Future;
import io.netty.util.concurrent.GenericFutureListener;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Netty-based server implementation.
 */
class NettyServer implements InternalServer, InternalWithLogId {
  private static final Logger log = Logger.getLogger(InternalServer.class.getName());

  private final InternalLogId logId = InternalLogId.allocate(getClass().getName());
  private final SocketAddress address;
  private final Class<? extends ServerChannel> channelType;
  private final Map<ChannelOption<?>, ?> channelOptions;
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
  private final TransportTracer.Factory transportTracerFactory;
  private final InternalChannelz channelz;
  // Only modified in event loop but safe to read any time. Set at startup and unset at shutdown.
  // In the future we may have >1 listen socket.
  private volatile ImmutableList<InternalInstrumented<SocketStats>> listenSockets
      = ImmutableList.of();

  NettyServer(
      SocketAddress address, Class<? extends ServerChannel> channelType,
      Map<ChannelOption<?>, ?> channelOptions,
      @Nullable EventLoopGroup bossGroup, @Nullable EventLoopGroup workerGroup,
      ProtocolNegotiator protocolNegotiator, List<ServerStreamTracer.Factory> streamTracerFactories,
      TransportTracer.Factory transportTracerFactory,
      int maxStreamsPerConnection, int flowControlWindow, int maxMessageSize, int maxHeaderListSize,
      long keepAliveTimeInNanos, long keepAliveTimeoutInNanos,
      long maxConnectionIdleInNanos,
      long maxConnectionAgeInNanos, long maxConnectionAgeGraceInNanos,
      boolean permitKeepAliveWithoutCalls, long permitKeepAliveTimeInNanos,
      InternalChannelz channelz) {
    this.address = address;
    this.channelType = checkNotNull(channelType, "channelType");
    checkNotNull(channelOptions, "channelOptions");
    this.channelOptions = new HashMap<ChannelOption<?>, Object>(channelOptions);
    this.bossGroup = bossGroup;
    this.workerGroup = workerGroup;
    this.protocolNegotiator = checkNotNull(protocolNegotiator, "protocolNegotiator");
    this.streamTracerFactories = checkNotNull(streamTracerFactories, "streamTracerFactories");
    this.usingSharedBossGroup = bossGroup == null;
    this.usingSharedWorkerGroup = workerGroup == null;
    this.transportTracerFactory = transportTracerFactory;
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
    this.channelz = Preconditions.checkNotNull(channelz);
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
  public List<InternalInstrumented<SocketStats>> getListenSockets() {
    return listenSockets;
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

    if (channelOptions != null) {
      for (Map.Entry<ChannelOption<?>, ?> entry : channelOptions.entrySet()) {
        @SuppressWarnings("unchecked")
        ChannelOption<Object> key = (ChannelOption<Object>) entry.getKey();
        b.childOption(key, entry.getValue());
      }
    }

    b.childHandler(new ChannelInitializer<Channel>() {
      @Override
      public void initChannel(Channel ch) throws Exception {

        ChannelPromise channelDone = ch.newPromise();

        long maxConnectionAgeInNanos = NettyServer.this.maxConnectionAgeInNanos;
        if (maxConnectionAgeInNanos != MAX_CONNECTION_AGE_NANOS_DISABLED) {
          // apply a random jitter of +/-10% to max connection age
          maxConnectionAgeInNanos =
              (long) ((.9D + Math.random() * .2D) * maxConnectionAgeInNanos);
        }

        NettyServerTransport transport =
            new NettyServerTransport(
                ch,
                channelDone,
                protocolNegotiator,
                streamTracerFactories,
                transportTracerFactory.create(),
                maxStreamsPerConnection,
                flowControlWindow,
                maxMessageSize,
                maxHeaderListSize,
                keepAliveTimeInNanos,
                keepAliveTimeoutInNanos,
                maxConnectionIdleInNanos,
                maxConnectionAgeInNanos,
                maxConnectionAgeGraceInNanos,
                permitKeepAliveWithoutCalls,
                permitKeepAliveTimeInNanos);
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

        /**
         * Releases the event loop if the channel is "done", possibly due to the channel closing.
         */
        final class LoopReleaser implements ChannelFutureListener {
          boolean done;

          @Override
          public void operationComplete(ChannelFuture future) throws Exception {
            if (!done) {
              done = true;
              eventLoopReferenceCounter.release();
            }
          }
        }

        transport.start(transportListener);
        ChannelFutureListener loopReleaser = new LoopReleaser();
        channelDone.addListener(loopReleaser);
        ch.closeFuture().addListener(loopReleaser);
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
    Future<?> channelzFuture = channel.eventLoop().submit(new Runnable() {
      @Override
      public void run() {
        InternalInstrumented<SocketStats> listenSocket = new ListenSocket(channel);
        listenSockets = ImmutableList.of(listenSocket);
        channelz.addListenSocket(listenSocket);
      }
    });
    try {
      channelzFuture.await();
    } catch (InterruptedException ex) {
      throw new RuntimeException("Interrupted while registering listen socket to channelz", ex);
    }
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
        for (InternalInstrumented<SocketStats> listenSocket : listenSockets) {
          channelz.removeListenSocket(listenSocket);
        }
        listenSockets = null;
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

  @Override
  public InternalLogId getLogId() {
    return logId;
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("logId", logId.getId())
        .add("address", address)
        .toString();
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

  /**
   * A class that can answer channelz queries about the server listen sockets.
   */
  private static final class ListenSocket implements InternalInstrumented<SocketStats> {
    private final InternalLogId id = InternalLogId.allocate(getClass().getName());
    private final Channel ch;

    ListenSocket(Channel ch) {
      this.ch = ch;
    }

    @Override
    public ListenableFuture<SocketStats> getStats() {
      final SettableFuture<SocketStats> ret = SettableFuture.create();
      if (ch.eventLoop().inEventLoop()) {
        // This is necessary, otherwise we will block forever if we get the future from inside
        // the event loop.
        ret.set(new SocketStats(
            /*data=*/ null,
            ch.localAddress(),
            /*remote=*/ null,
            Utils.getSocketOptions(ch),
            /*security=*/ null));
        return ret;
      }
      ch.eventLoop()
          .submit(
              new Runnable() {
                @Override
                public void run() {
                  ret.set(new SocketStats(
                      /*data=*/ null,
                      ch.localAddress(),
                      /*remote=*/ null,
                      Utils.getSocketOptions(ch),
                      /*security=*/ null));
                }
              })
          .addListener(
              new GenericFutureListener<Future<Object>>() {
                @Override
                public void operationComplete(Future<Object> future) throws Exception {
                  if (!future.isSuccess()) {
                    ret.setException(future.cause());
                  }
                }
              });
      return ret;
    }

    @Override
    public InternalLogId getLogId() {
      return id;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("logId", id.getId())
          .add("channel", ch)
          .toString();
    }
  }
}
