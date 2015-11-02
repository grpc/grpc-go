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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;

import io.grpc.Attributes;
import io.grpc.ExperimentalApi;
import io.grpc.Internal;
import io.grpc.NameResolver;
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
public class NettyChannelBuilder extends AbstractManagedChannelImplBuilder<NettyChannelBuilder> {
  public static final int DEFAULT_FLOW_CONTROL_WINDOW = 1048576; // 1MiB

  private NegotiationType negotiationType = NegotiationType.TLS;
  private ProtocolNegotiator protocolNegotiator;
  private Class<? extends Channel> channelType = NioSocketChannel.class;
  @Nullable
  private EventLoopGroup eventLoopGroup;
  private SslContext sslContext;
  private int flowControlWindow = DEFAULT_FLOW_CONTROL_WINDOW;
  private int maxMessageSize = DEFAULT_MAX_MESSAGE_SIZE;
  private int maxHeaderListSize = GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE;

  /**
   * Creates a new builder with the given server address. This factory method is primarily intended
   * for using Netty Channel types other than SocketChannel. {@link #forAddress(String, int)} should
   * generally be preferred over this method, since that API permits delaying DNS lookups and
   * noticing changes to DNS.
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

  /**
   * Creates a new builder with the given target string that will be resolved by
   * {@link io.grpc.NameResolver}.
   */
  public static NettyChannelBuilder forTarget(String target) {
    return new NettyChannelBuilder(target);
  }

  private NettyChannelBuilder(String target) {
    super(target);
  }

  protected NettyChannelBuilder(SocketAddress address) {
    super(address, getAuthorityFromAddress(address));
  }

  private static String getAuthorityFromAddress(SocketAddress address) {
    if (address instanceof InetSocketAddress) {
      InetSocketAddress inetAddress = (InetSocketAddress) address;
      return GrpcUtil.authorityFromHostAndPort(inetAddress.getHostString(), inetAddress.getPort());
    } else {
      return address.toString();
    }
  }

  /**
   * Specify the channel type to use, by default we use {@link NioSocketChannel}.
   */
  public final NettyChannelBuilder channelType(Class<? extends Channel> channelType) {
    this.channelType = Preconditions.checkNotNull(channelType);
    return this;
  }

  /**
   * Sets the negotiation type for the HTTP/2 connection.
   *
   * <p>Default: <code>TLS</code>
   */
  public final NettyChannelBuilder negotiationType(NegotiationType type) {
    negotiationType = type;
    return this;
  }

  /**
   * Sets the {@link ProtocolNegotiator} to be used. If non-{@code null}, overrides the value
   * specified in {@link #negotiationType(NegotiationType)} or {@link #usePlaintext(boolean)}.
   *
   * <p>Default: {@code null}.
   */
  @Internal
  public final NettyChannelBuilder protocolNegotiator(
          @Nullable ProtocolNegotiator protocolNegotiator) {
    this.protocolNegotiator = protocolNegotiator;
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
  public final NettyChannelBuilder eventLoopGroup(@Nullable EventLoopGroup eventLoopGroup) {
    this.eventLoopGroup = eventLoopGroup;
    return this;
  }

  /**
   * SSL/TLS context to use instead of the system default. It must have been configured with {@link
   * GrpcSslContexts}, but options could have been overridden.
   */
  public final NettyChannelBuilder sslContext(SslContext sslContext) {
    this.sslContext = sslContext;
    return this;
  }

  /**
   * Sets the flow control window in bytes. If not called, the default value
   * is {@link #DEFAULT_FLOW_CONTROL_WINDOW}).
   */
  public final NettyChannelBuilder flowControlWindow(int flowControlWindow) {
    checkArgument(flowControlWindow > 0, "flowControlWindow must be positive");
    this.flowControlWindow = flowControlWindow;
    return this;
  }

  /**
   * Sets the maximum message size allowed to be received on the channel. If not called,
   * defaults to {@link GrpcUtil#DEFAULT_MAX_MESSAGE_SIZE}.
   */
  public final NettyChannelBuilder maxMessageSize(int maxMessageSize) {
    checkArgument(maxMessageSize >= 0, "maxMessageSize must be >= 0");
    this.maxMessageSize = maxMessageSize;
    return this;
  }

  /**
   * Sets the maximum size of header list allowed to be received on the channel. If not called,
   * defaults to {@link GrpcUtil#DEFAULT_MAX_HEADER_LIST_SIZE}.
   */
  public final NettyChannelBuilder maxHeaderListSize(int maxHeaderListSize) {
    checkArgument(maxHeaderListSize > 0, "maxHeaderListSize must be > 0");
    this.maxHeaderListSize = maxHeaderListSize;
    return this;
  }

  /**
   * Equivalent to using {@link #negotiationType(NegotiationType)} with {@code PLAINTEXT} or
   * {@code PLAINTEXT_UPGRADE}.
   */
  @Override
  public final NettyChannelBuilder usePlaintext(boolean skipNegotiation) {
    if (skipNegotiation) {
      negotiationType(NegotiationType.PLAINTEXT);
    } else {
      negotiationType(NegotiationType.PLAINTEXT_UPGRADE);
    }
    return this;
  }

  @Override
  protected ClientTransportFactory buildTransportFactory() {
    return new NettyTransportFactory(channelType, negotiationType, protocolNegotiator, sslContext,
        eventLoopGroup, flowControlWindow, maxMessageSize, maxHeaderListSize);
  }

  @Override
  protected Attributes getNameResolverParams() {
    int defaultPort;
    switch (negotiationType) {
      case PLAINTEXT:
      case PLAINTEXT_UPGRADE:
        defaultPort = GrpcUtil.DEFAULT_PORT_PLAINTEXT;
        break;
      case TLS:
        defaultPort = GrpcUtil.DEFAULT_PORT_SSL;
        break;
      default:
        throw new AssertionError(negotiationType + " not handled");
    }
    return Attributes.newBuilder()
        .set(NameResolver.Factory.PARAMS_DEFAULT_PORT, defaultPort).build();
  }

  @VisibleForTesting
  static ProtocolNegotiator createProtocolNegotiator(
      String authority,
      NegotiationType negotiationType,
      SslContext sslContext) {
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
    private final Class<? extends Channel> channelType;
    private final NegotiationType negotiationType;
    private final ProtocolNegotiator protocolNegotiator;
    private final SslContext sslContext;
    private final EventLoopGroup group;
    private final boolean usingSharedGroup;
    private final int flowControlWindow;
    private final int maxMessageSize;
    private final int maxHeaderListSize;

    private NettyTransportFactory(Class<? extends Channel> channelType,
                                  NegotiationType negotiationType,
                                  ProtocolNegotiator protocolNegotiator,
                                  SslContext sslContext,
                                  EventLoopGroup group,
                                  int flowControlWindow,
                                  int maxMessageSize,
                                  int maxHeaderListSize) {
      this.channelType = channelType;
      this.negotiationType = negotiationType;
      this.protocolNegotiator = protocolNegotiator;
      this.sslContext = sslContext;
      this.flowControlWindow = flowControlWindow;
      this.maxMessageSize = maxMessageSize;
      this.maxHeaderListSize = maxHeaderListSize;
      usingSharedGroup = group == null;
      if (usingSharedGroup) {
        // The group was unspecified, using the shared group.
        this.group = SharedResourceHolder.get(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP);
      } else {
        this.group = group;
      }
    }

    @Override
    public ClientTransport newClientTransport(SocketAddress serverAddress, String authority) {
      ProtocolNegotiator negotiator = protocolNegotiator != null ? protocolNegotiator :
          createProtocolNegotiator(authority, negotiationType, sslContext);
      return new NettyClientTransport(serverAddress, channelType, group, negotiator,
          flowControlWindow, maxMessageSize, maxHeaderListSize, authority);
    }

    @Override
    protected void deallocate() {
      if (usingSharedGroup) {
        SharedResourceHolder.release(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP, group);
      }
    }
  }
}
