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

import com.google.common.base.Preconditions;
import io.grpc.ServerStreamTracer;
import io.grpc.Status;
import io.grpc.internal.LogId;
import io.grpc.internal.ServerTransport;
import io.grpc.internal.ServerTransportListener;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandler;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * The Netty-based server transport.
 */
class NettyServerTransport implements ServerTransport {
  private static final Logger log = Logger.getLogger(NettyServerTransport.class.getName());

  private final LogId logId = LogId.allocate(getClass().getName());
  private final Channel channel;
  private final ProtocolNegotiator protocolNegotiator;
  private final int maxStreams;
  private ServerTransportListener listener;
  private boolean terminated;
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
  private final List<ServerStreamTracer.Factory> streamTracerFactories;

  NettyServerTransport(
      Channel channel, ProtocolNegotiator protocolNegotiator,
      List<ServerStreamTracer.Factory> streamTracerFactories, int maxStreams,
      int flowControlWindow, int maxMessageSize, int maxHeaderListSize,
      long keepAliveTimeInNanos, long keepAliveTimeoutInNanos,
      long maxConnectionIdleInNanos,
      long maxConnectionAgeInNanos, long maxConnectionAgeGraceInNanos,
      boolean permitKeepAliveWithoutCalls,long permitKeepAliveTimeInNanos) {
    this.channel = Preconditions.checkNotNull(channel, "channel");
    this.protocolNegotiator = Preconditions.checkNotNull(protocolNegotiator, "protocolNegotiator");
    this.streamTracerFactories =
        Preconditions.checkNotNull(streamTracerFactories, "streamTracerFactories");
    this.maxStreams = maxStreams;
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

  public void start(ServerTransportListener listener) {
    Preconditions.checkState(this.listener == null, "Handler already registered");
    this.listener = listener;

    // Create the Netty handler for the pipeline.
    final NettyServerHandler grpcHandler = createHandler(listener);
    HandlerSettings.setAutoWindow(grpcHandler);

    // Notify when the channel closes.
    channel.closeFuture().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        notifyTerminated(grpcHandler.connectionError());
      }
    });

    ChannelHandler negotiationHandler = protocolNegotiator.newHandler(grpcHandler);
    channel.pipeline().addLast(negotiationHandler);
  }

  @Override
  public void shutdown() {
    if (channel.isOpen()) {
      channel.close();
    }
  }

  @Override
  public void shutdownNow(Status reason) {
    if (channel.isOpen()) {
      channel.writeAndFlush(new ForcefulCloseCommand(reason));
    }
  }

  @Override
  public LogId getLogId() {
    return logId;
  }

  /**
   * For testing purposes only.
   */
  Channel channel() {
    return channel;
  }

  private void notifyTerminated(Throwable t) {
    if (t != null) {
      log.log(Level.SEVERE, "Transport failed", t);
    }
    if (!terminated) {
      terminated = true;
      listener.transportTerminated();
    }
  }

  /**
   * Creates the Netty handler to be used in the channel pipeline.
   */
  private NettyServerHandler createHandler(ServerTransportListener transportListener) {
    return NettyServerHandler.newHandler(
        transportListener, streamTracerFactories, maxStreams,
        flowControlWindow, maxHeaderListSize, maxMessageSize,
        keepAliveTimeInNanos, keepAliveTimeoutInNanos,
        maxConnectionIdleInNanos,
        maxConnectionAgeInNanos, maxConnectionAgeGraceInNanos,
        permitKeepAliveWithoutCalls, permitKeepAliveTimeInNanos);
  }
}
