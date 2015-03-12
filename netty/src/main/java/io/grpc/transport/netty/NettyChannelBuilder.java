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

import io.grpc.AbstractChannelBuilder;
import io.grpc.SharedResourceHolder;
import io.grpc.transport.ClientTransportFactory;
import io.netty.channel.Channel;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.ssl.SslContext;

import java.net.InetSocketAddress;
import java.net.SocketAddress;

/**
 * A builder to help simplify construction of channels using the Netty transport.
 */
public final class NettyChannelBuilder extends AbstractChannelBuilder<NettyChannelBuilder> {

  private final SocketAddress serverAddress;

  private NegotiationType negotiationType = NegotiationType.TLS;
  private Class<? extends Channel> channelType = NioSocketChannel.class;
  private EventLoopGroup userEventLoopGroup;
  private SslContext sslContext;

  /**
   * Creates a new builder with the given server address.
   */
  public static NettyChannelBuilder forAddress(SocketAddress serverAddress) {
    return new NettyChannelBuilder(serverAddress);
  }

  /**
   * Creates a new builder with the given host and port.
   */
  public static NettyChannelBuilder forAddress(String host, int port) {
    return forAddress(new InetSocketAddress(host, port));
  }

  private NettyChannelBuilder(SocketAddress serverAddress) {
    this.serverAddress = serverAddress;
  }

  /**
   * Specify the channel type to use, by default we use {@link NioSocketChannel}.
   */
  public NettyChannelBuilder channelType(Class<? extends Channel> channelType) {
    this.channelType = Preconditions.checkNotNull(channelType);
    return this;
  }

  /**
   * Sets the negotiation type for the HTTP/2 connection.
   *
   * <p>Default: <code>TLS</code>
   */
  public NettyChannelBuilder negotiationType(NegotiationType type) {
    negotiationType = type;
    return this;
  }

  /**
   * Provides an EventGroupLoop to be used by the netty transport.
   *
   * <p>It's an optional parameter. If the user has not provided an EventGroupLoop when the channel
   * is built, the builder will use the default one which is static.
   *
   * <p>The channel won't take ownership of the given EventLoopGroup. It's caller's responsibility
   * to shut it down when it's desired.
   */
  public NettyChannelBuilder eventLoopGroup(EventLoopGroup group) {
    userEventLoopGroup = group;
    return this;
  }

  /** SSL/TLS context to use instead of the system default. */
  public NettyChannelBuilder sslContext(SslContext sslContext) {
    this.sslContext = sslContext;
    return this;
  }

  @Override
  protected ChannelEssentials buildEssentials() {
    final EventLoopGroup group = (userEventLoopGroup == null)
        ? SharedResourceHolder.get(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP) : userEventLoopGroup;
    ClientTransportFactory transportFactory = new NettyClientTransportFactory(
        serverAddress, channelType, negotiationType, group, sslContext);
    Runnable terminationRunnable = null;
    if (userEventLoopGroup == null) {
      terminationRunnable = new Runnable() {
        @Override
        public void run() {
          SharedResourceHolder.release(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP, group);
        }
      };
    }
    return new ChannelEssentials(transportFactory, terminationRunnable);
  }
}
