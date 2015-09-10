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
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.AbstractReferenceCounted;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourceHolder;
import io.netty.channel.Channel;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.ssl.SslContext;

import java.net.InetSocketAddress;
import java.net.SocketAddress;

import javax.annotation.Nullable;
import javax.net.ssl.SSLException;

/**
 * A builder to help simplify construction of channels using the Netty transport.
 */
@ExperimentalApi("There is no plan to make this API stable, given transport API instability")
public final class NettyChannelBuilder
        extends AbstractManagedChannelImplBuilder<NettyChannelBuilder> {
  public static final int DEFAULT_FLOW_CONTROL_WINDOW = 1048576; // 1MiB

  private final SocketAddress serverAddress;
  private String authority;
  private NegotiationType negotiationType = NegotiationType.TLS;
  private Class<? extends Channel> channelType = NioSocketChannel.class;
  @Nullable
  private EventLoopGroup eventLoopGroup;
  private SslContext sslContext;
  private int flowControlWindow = DEFAULT_FLOW_CONTROL_WINDOW;
  private int maxMessageSize = DEFAULT_MAX_MESSAGE_SIZE;

  /**
   * Creates a new builder with the given server address. This factory method is primarily intended
   * for using Netty Channel types other than SocketChannel. {@link #forAddress(String, int)} should
   * generally be preferred over this method, since that API permits delaying DNS lookups and
   * noticing changes to DNS.
   */
  public static NettyChannelBuilder forAddress(SocketAddress serverAddress) {
    String authority;
    if (serverAddress instanceof InetSocketAddress) {
      InetSocketAddress address = (InetSocketAddress) serverAddress;
      authority = GrpcUtil.authorityFromHostAndPort(address.getHostString(), address.getPort());
    } else {
      // Specialized address types are allowed to support custom Channel types so just assume their
      // toString() values are valid :authority values. We defer checking validity of authority
      // until buildTransportFactory() to provide the user an opportunity to override the value.
      authority = serverAddress.toString();
    }
    return new NettyChannelBuilder(serverAddress, authority);
  }

  /**
   * Creates a new builder with the given host and port.
   */
  public static NettyChannelBuilder forAddress(String host, int port) {
    return new NettyChannelBuilder(
        new InetSocketAddress(host, port), GrpcUtil.authorityFromHostAndPort(host, port));
  }

  private NettyChannelBuilder(SocketAddress serverAddress, String authority) {
    this.serverAddress = serverAddress;
    this.authority = authority;
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
  public NettyChannelBuilder eventLoopGroup(@Nullable EventLoopGroup eventLoopGroup) {
    this.eventLoopGroup = eventLoopGroup;
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
   * Sets the flow control window in bytes. If not called, the default value
   * is {@link #DEFAULT_FLOW_CONTROL_WINDOW}).
   */
  public NettyChannelBuilder flowControlWindow(int flowControlWindow) {
    Preconditions.checkArgument(flowControlWindow > 0, "flowControlWindow must be positive");
    this.flowControlWindow = flowControlWindow;
    return this;
  }

  /**
   * Sets the maximum message size allowed to be received on the channel. If not called,
   * defaults to {@link io.grpc.internal.GrpcUtil#DEFAULT_MAX_MESSAGE_SIZE}.
   */
  public NettyChannelBuilder maxMessageSize(int maxMessageSize) {
    checkArgument(maxMessageSize >= 0, "maxMessageSize must be >= 0");
    this.maxMessageSize = maxMessageSize;
    return this;
  }

  /**
   * Equivalent to using {@link #negotiationType(NegotiationType)} with {@code PLAINTEXT} or
   * {@code PLAINTEXT_UPGRADE}.
   */
  @Override
  public NettyChannelBuilder usePlaintext(boolean skipNegotiation) {
    if (skipNegotiation) {
      negotiationType(NegotiationType.PLAINTEXT);
    } else {
      negotiationType(NegotiationType.PLAINTEXT_UPGRADE);
    }
    return this;
  }

  @Override
  public NettyChannelBuilder overrideAuthority(String authority) {
    this.authority = GrpcUtil.checkAuthority(authority);
    return this;
  }

  @Override
  protected ClientTransportFactory buildTransportFactory() {
    // Check authority, since non-inet ServerAddresses delay the authority check.
    GrpcUtil.checkAuthority(authority);
    return new NettyTransportFactory(serverAddress, authority, channelType, eventLoopGroup,
        flowControlWindow, createProtocolNegotiator(), maxMessageSize);
  }

  private ProtocolNegotiator createProtocolNegotiator() {
    switch (negotiationType) {
      case PLAINTEXT:
        return ProtocolNegotiators.plaintext();
      case PLAINTEXT_UPGRADE:
        return ProtocolNegotiators.plaintextUpgrade();
      case TLS:
        if (sslContext == null) {
          try {
            sslContext = GrpcSslContexts.forClient().build();
          } catch (SSLException ex) {
            throw new RuntimeException(ex);
          }
        }
        return ProtocolNegotiators.tls(sslContext, authority);
      default:
        throw new IllegalArgumentException("Unsupported negotiationType: " + negotiationType);
    }
  }

  private static class NettyTransportFactory extends AbstractReferenceCounted
          implements ClientTransportFactory {
    private final SocketAddress serverAddress;
    private final Class<? extends Channel> channelType;
    private final EventLoopGroup group;
    private final boolean usingSharedGroup;
    private final int flowControlWindow;
    private final ProtocolNegotiator negotiator;
    private final int maxMessageSize;
    private final String authority;

    private NettyTransportFactory(SocketAddress serverAddress,
                                  String authority,
                                  Class<? extends Channel> channelType,
                                  EventLoopGroup group,
                                  int flowControlWindow,
                                  ProtocolNegotiator negotiator,
                                  int maxMessageSize) {
      this.serverAddress = serverAddress;
      this.channelType = channelType;
      this.flowControlWindow = flowControlWindow;
      this.negotiator = negotiator;
      this.maxMessageSize = maxMessageSize;
      this.authority = authority;

      usingSharedGroup = group == null;
      if (usingSharedGroup) {
        // The group was unspecified, using the shared group.
        this.group = SharedResourceHolder.get(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP);
      } else {
        this.group = group;
      }
    }

    @Override
    public ClientTransport newClientTransport() {
      return new NettyClientTransport(serverAddress, channelType, group, negotiator,
              flowControlWindow, maxMessageSize, authority);
    }

    @Override
    public String authority() {
      return authority;
    }

    @Override
    protected void deallocate() {
      if (usingSharedGroup) {
        SharedResourceHolder.release(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP, group);
      }
    }
  }
}
