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

import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;
import com.google.common.base.Ticker;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientStream;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.Http2Ping;
import io.grpc.internal.ManagedClientTransport;
import io.netty.bootstrap.Bootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.util.AsciiString;

import java.net.SocketAddress;
import java.nio.channels.ClosedChannelException;
import java.util.concurrent.Executor;

import javax.annotation.Nullable;

/**
 * A Netty-based {@link ManagedClientTransport} implementation.
 */
class NettyClientTransport implements ManagedClientTransport {
  private final SocketAddress address;
  private final Class<? extends Channel> channelType;
  private final EventLoopGroup group;
  private final ProtocolNegotiator negotiator;
  private final AsciiString authority;
  private final AsciiString userAgent;
  private final int flowControlWindow;
  private final int maxMessageSize;
  private final int maxHeaderListSize;

  private ProtocolNegotiator.Handler negotiationHandler;
  private NettyClientHandler handler;
  // We should not send on the channel until negotiation completes. This is a hard requirement
  // by SslHandler but is appropriate for HTTP/1.1 Upgrade as well.
  private Channel channel;
  /** Since not thread-safe, may only be used from event loop. */
  private ClientTransportLifecycleManager lifecycleManager;

  NettyClientTransport(SocketAddress address, Class<? extends Channel> channelType,
                       EventLoopGroup group, ProtocolNegotiator negotiator,
                       int flowControlWindow, int maxMessageSize, int maxHeaderListSize,
                       String authority, @Nullable String userAgent) {
    this.negotiator = Preconditions.checkNotNull(negotiator, "negotiator");
    this.address = Preconditions.checkNotNull(address, "address");
    this.group = Preconditions.checkNotNull(group, "group");
    this.channelType = Preconditions.checkNotNull(channelType, "channelType");
    this.flowControlWindow = flowControlWindow;
    this.maxMessageSize = maxMessageSize;
    this.maxHeaderListSize = maxHeaderListSize;
    this.authority = new AsciiString(authority);
    this.userAgent = new AsciiString(GrpcUtil.getGrpcUserAgent("netty", userAgent));
  }

  @Override
  public void ping(final PingCallback callback, final Executor executor) {
    ChannelFutureListener failureListener = new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          Status s = statusFromFailedFuture(future);
          Http2Ping.notifyFailed(callback, executor, s.asException());
        }
      }
    };
    // Write the command requesting the ping
    handler.getWriteQueue().enqueue(new SendPingCommand(callback, executor), true)
        .addListener(failureListener);
  }

  @Override
  public ClientStream newStream(MethodDescriptor<?, ?> method, Metadata headers) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(headers, "headers");
    return new NettyClientStream(method, headers, channel, handler, maxMessageSize, authority,
        negotiationHandler.scheme(), userAgent) {
      @Override
      protected Status statusFromFailedFuture(ChannelFuture f) {
        return NettyClientTransport.this.statusFromFailedFuture(f);
      }
    };
  }

  @Override
  public void start(Listener transportListener) {
    lifecycleManager = new ClientTransportLifecycleManager(
        Preconditions.checkNotNull(transportListener, "listener"));

    handler = newHandler();
    negotiationHandler = negotiator.newHandler(handler);

    Bootstrap b = new Bootstrap();
    b.group(group);
    b.channel(channelType);
    if (NioSocketChannel.class.isAssignableFrom(channelType)) {
      b.option(SO_KEEPALIVE, true);
    }
    /**
     * We don't use a ChannelInitializer in the client bootstrap because its "initChannel" method
     * is executed in the event loop and we need this handler to be in the pipeline immediately so
     * that it may begin buffering writes.
     */
    b.handler(negotiationHandler);
    // Start the connection operation to the server.
    channel = b.connect(address).addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          ChannelHandlerContext ctx = future.channel().pipeline().context(handler);
          if (ctx != null) {
            // NettyClientHandler doesn't propagate exceptions, but the negotiator will need the
            // exception to fail any writes. Note that this fires after handler, because it is as if
            // handler was propagating the notification.
            ctx.fireExceptionCaught(future.cause());
          }
          future.channel().pipeline().fireExceptionCaught(future.cause());
        }
      }
    }).channel();
    // Start the write queue as soon as the channel is constructed
    handler.startWriteQueue(channel);
    // This write will have no effect, yet it will only complete once the negotiationHandler
    // flushes any pending writes.
    channel.write(NettyClientHandler.NOOP_MESSAGE).addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          // Need to notify of this failure, because NettyClientHandler may not have been added to
          // the pipeline before the error occurred.
          lifecycleManager.notifyTerminated(Utils.statusFromThrowable(future.cause()));
        }
      }
    });
    // Handle transport shutdown when the channel is closed.
    channel.closeFuture().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        // Typically we should have noticed shutdown before this point.
        lifecycleManager.notifyTerminated(
            Status.INTERNAL.withDescription("Connection closed with unknown cause"));
      }
    });
  }

  @Override
  public void shutdown() {
    // Notifying of termination is automatically done when the channel closes.
    if (channel.isOpen()) {
      Status status
          = Status.UNAVAILABLE.withDescription("Channel requested transport to shut down");
      handler.getWriteQueue().enqueue(new GracefulCloseCommand(status), true);
    }
  }

  @Override
  public String toString() {
    return getLogId() + "(" + address + ")";
  }

  @Override
  public String getLogId() {
    return GrpcUtil.getLogId(this);
  }

  /**
   * Convert ChannelFuture.cause() to a Status, taking into account that all handlers are removed
   * from the pipeline when the channel is closed. Since handlers are removed, you may get an
   * unhelpful exception like ClosedChannelException.
   */
  private Status statusFromFailedFuture(ChannelFuture f) {
    Throwable t = f.cause();
    if (t instanceof ClosedChannelException) {
      synchronized (this) {
        Status shutdownStatus = lifecycleManager.getShutdownStatus();
        if (shutdownStatus == null) {
          return Status.UNKNOWN.withDescription("Channel closed but for unknown reason");
        }
        return shutdownStatus;
      }
    }
    return Utils.statusFromThrowable(t);
  }

  private NettyClientHandler newHandler() {
    return NettyClientHandler.newHandler(lifecycleManager, flowControlWindow, maxHeaderListSize,
        Ticker.systemTicker());
  }
}
