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
import io.grpc.transport.ClientTransport;
import io.grpc.transport.ClientTransportFactory;
import io.netty.channel.Channel;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.http2.Http2CodecUtil;
import io.netty.handler.ssl.SslContext;

import java.net.InetSocketAddress;
import java.net.SocketAddress;

import javax.net.ssl.SSLException;

/**
 * A builder to help simplify construction of channels using the Netty transport.
 */
public final class NettyChannelBuilder extends AbstractChannelBuilder<NettyChannelBuilder> {
  public static final int DEFAULT_CONNECTION_WINDOW_SIZE = 1048576; // 1MiB
  public static final int DEFAULT_STREAM_WINDOW_SIZE = Http2CodecUtil.DEFAULT_WINDOW_SIZE;

  private final SocketAddress serverAddress;
  private NegotiationType negotiationType = NegotiationType.TLS;
  private Class<? extends Channel> channelType = NioSocketChannel.class;
  private EventLoopGroup userEventLoopGroup;
  private SslContext sslContext;
  private int connectionWindowSize = DEFAULT_CONNECTION_WINDOW_SIZE;
  private int streamWindowSize = DEFAULT_STREAM_WINDOW_SIZE;

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

  /**
   * SSL/TLS context to use instead of the system default. It must have been configured with {@link
   * GrpcSslContexts}, but options could have been overridden.
   */
  public NettyChannelBuilder sslContext(SslContext sslContext) {
    this.sslContext = sslContext;
    return this;
  }

  /**
   * Sets the HTTP/2 connection flow control window. If not called, the default value
   * is {@link #DEFAULT_CONNECTION_WINDOW_SIZE}).
   */
  public NettyChannelBuilder connectionWindowSize(int connectionWindowSize) {
    Preconditions.checkArgument(connectionWindowSize > 0, "connectionWindowSize must be positive");
    this.connectionWindowSize = connectionWindowSize;
    return this;
  }

  /**
   * Sets the HTTP/2 per-stream flow control window. If not called, the default value
   * is {@link #DEFAULT_STREAM_WINDOW_SIZE}).
   */
  public NettyChannelBuilder streamWindowSize(int streamWindowSize) {
    Preconditions.checkArgument(streamWindowSize > 0, "streamWindowSize must be positive");
    this.streamWindowSize = streamWindowSize;
    return this;
  }

  @Override
  protected ChannelEssentials buildEssentials() {
    final EventLoopGroup group = (userEventLoopGroup == null)
        ? SharedResourceHolder.get(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP) : userEventLoopGroup;
    final NegotiationType negotiationType = this.negotiationType;
    final Class<? extends Channel> channelType = this.channelType;
    final int connectionWindowSize = this.connectionWindowSize;
    final int streamWindowSize = this.streamWindowSize;
    final ProtocolNegotiator negotiator;
    switch (negotiationType) {
      case PLAINTEXT:
        negotiator = ProtocolNegotiators.plaintext();
        break;
      case PLAINTEXT_UPGRADE:
        negotiator = ProtocolNegotiators.plaintextUpgrade();
        break;
      case TLS:
        if (!(serverAddress instanceof InetSocketAddress)) {
          throw new IllegalStateException("TLS not supported for non-internet socket types");
        }
        if (sslContext == null) {
          try {
            sslContext = GrpcSslContexts.forClient().build();
          } catch (SSLException ex) {
            throw new RuntimeException(ex);
          }
        }
        negotiator = ProtocolNegotiators.tls(sslContext, (InetSocketAddress) serverAddress);
        break;
      default:
        throw new IllegalArgumentException("Unsupported negotiationType: " + negotiationType);
    }

    ClientTransportFactory transportFactory = new ClientTransportFactory() {
      @Override
      public ClientTransport newClientTransport() {
        return new NettyClientTransport(serverAddress, channelType, group,
            negotiator, connectionWindowSize, streamWindowSize);
      }
    };
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
