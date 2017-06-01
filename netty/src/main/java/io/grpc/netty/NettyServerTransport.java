/*
 * Copyright 2014, gRPC Authors All rights reserved.
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
