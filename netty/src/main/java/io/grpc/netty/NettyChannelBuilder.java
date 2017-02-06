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
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.internal.GrpcUtil.DEFAULT_KEEPALIVE_DELAY_NANOS;
import static io.grpc.internal.GrpcUtil.DEFAULT_KEEPALIVE_TIMEOUT_NANOS;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.errorprone.annotations.CanIgnoreReturnValue;
import io.grpc.Attributes;
import io.grpc.ExperimentalApi;
import io.grpc.NameResolver;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.ConnectionClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourceHolder;
import io.netty.channel.Channel;
import io.netty.channel.ChannelOption;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.ssl.SslContext;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;
import javax.net.ssl.SSLException;

/**
 * A builder to help simplify construction of channels using the Netty transport.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1784")
@CanIgnoreReturnValue
public final class NettyChannelBuilder
    extends AbstractManagedChannelImplBuilder<NettyChannelBuilder> {
  public static final int DEFAULT_FLOW_CONTROL_WINDOW = 1048576; // 1MiB

  private final Map<ChannelOption<?>, Object> channelOptions =
      new HashMap<ChannelOption<?>, Object>();

  private NegotiationType negotiationType = NegotiationType.TLS;
  private OverrideAuthorityChecker authorityChecker;
  private Class<? extends Channel> channelType = NioSocketChannel.class;

  @Nullable
  private EventLoopGroup eventLoopGroup;
  private SslContext sslContext;
  private int flowControlWindow = DEFAULT_FLOW_CONTROL_WINDOW;
  private int maxHeaderListSize = GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE;
  private boolean enableKeepAlive;
  private long keepAliveDelayNanos;
  private long keepAliveTimeoutNanos;
  private TransportCreationParamsFilterFactory dynamicParamsFactory;

  /**
   * Creates a new builder with the given server address. This factory method is primarily intended
   * for using Netty Channel types other than SocketChannel. {@link #forAddress(String, int)} should
   * generally be preferred over this method, since that API permits delaying DNS lookups and
   * noticing changes to DNS.
   */
  @CheckReturnValue
  public static NettyChannelBuilder forAddress(SocketAddress serverAddress) {
    return new NettyChannelBuilder(serverAddress);
  }

  /**
   * Creates a new builder with the given host and port.
   */
  @CheckReturnValue
  public static NettyChannelBuilder forAddress(String host, int port) {
    return new NettyChannelBuilder(host, port);
  }

  /**
   * Creates a new builder with the given target string that will be resolved by
   * {@link io.grpc.NameResolver}.
   */
  @CheckReturnValue
  public static NettyChannelBuilder forTarget(String target) {
    return new NettyChannelBuilder(target);
  }

  @CheckReturnValue
  NettyChannelBuilder(String host, int port) {
    this(GrpcUtil.authorityFromHostAndPort(host, port));
  }

  @CheckReturnValue
  NettyChannelBuilder(String target) {
    super(target);
  }

  @CheckReturnValue
  NettyChannelBuilder(SocketAddress address) {
    super(address, getAuthorityFromAddress(address));
  }

  @CheckReturnValue
  private static String getAuthorityFromAddress(SocketAddress address) {
    if (address instanceof InetSocketAddress) {
      InetSocketAddress inetAddress = (InetSocketAddress) address;
      return GrpcUtil.authorityFromHostAndPort(inetAddress.getHostString(), inetAddress.getPort());
    } else {
      return address.toString();
    }
  }

  /**
   * Specifies the channel type to use, by default we use {@link NioSocketChannel}.
   */
  public NettyChannelBuilder channelType(Class<? extends Channel> channelType) {
    this.channelType = Preconditions.checkNotNull(channelType, "channelType");
    return this;
  }

  /**
   * Specifies a channel option. As the underlying channel as well as network implementation may
   * ignore this value applications should consider it a hint.
   */
  public <T> NettyChannelBuilder withOption(ChannelOption<T> option, T value) {
    channelOptions.put(option, value);
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
    if (sslContext != null) {
      checkArgument(sslContext.isClient(),
          "Server SSL context can not be used for client channel");
      GrpcSslContexts.ensureAlpnAndH2Enabled(sslContext.applicationProtocolNegotiator());
    }
    this.sslContext = sslContext;
    return this;
  }

  /**
   * Sets the flow control window in bytes. If not called, the default value
   * is {@link #DEFAULT_FLOW_CONTROL_WINDOW}).
   */
  public NettyChannelBuilder flowControlWindow(int flowControlWindow) {
    checkArgument(flowControlWindow > 0, "flowControlWindow must be positive");
    this.flowControlWindow = flowControlWindow;
    return this;
  }

  /**
   * Sets the max message size.
   *
   * @deprecated Use {@link #maxInboundMessageSize} instead
   */
  @Deprecated
  public NettyChannelBuilder maxMessageSize(int maxMessageSize) {
    maxInboundMessageSize(maxMessageSize);
    return this;
  }

  /**
   * Sets the maximum size of header list allowed to be received on the channel. If not called,
   * defaults to {@link GrpcUtil#DEFAULT_MAX_HEADER_LIST_SIZE}.
   */
  public NettyChannelBuilder maxHeaderListSize(int maxHeaderListSize) {
    checkArgument(maxHeaderListSize > 0, "maxHeaderListSize must be > 0");
    this.maxHeaderListSize = maxHeaderListSize;
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


  /**
   * Enable keepalive with default delay and timeout.
   */
  public final NettyChannelBuilder enableKeepAlive(boolean enable) {
    enableKeepAlive = enable;
    if (enable) {
      keepAliveDelayNanos = DEFAULT_KEEPALIVE_DELAY_NANOS;
      keepAliveTimeoutNanos = DEFAULT_KEEPALIVE_TIMEOUT_NANOS;
    }
    return this;
  }

  /**
   * Enable keepalive with custom delay and timeout.
   */
  public final NettyChannelBuilder enableKeepAlive(boolean enable, long keepAliveDelay,
      TimeUnit delayUnit, long keepAliveTimeout, TimeUnit timeoutUnit) {
    enableKeepAlive = enable;
    if (enable) {
      keepAliveDelayNanos = delayUnit.toNanos(keepAliveDelay);
      keepAliveTimeoutNanos = timeoutUnit.toNanos(keepAliveTimeout);
    }
    return this;
  }

  @Override
  @CheckReturnValue
  protected ClientTransportFactory buildTransportFactory() {
    return new NettyTransportFactory(dynamicParamsFactory, channelType, channelOptions,
        negotiationType, sslContext, eventLoopGroup, flowControlWindow, maxInboundMessageSize(),
        maxHeaderListSize, enableKeepAlive, keepAliveDelayNanos, keepAliveTimeoutNanos);
  }

  @Override
  @CheckReturnValue
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

  void overrideAuthorityChecker(@Nullable OverrideAuthorityChecker authorityChecker) {
    this.authorityChecker = authorityChecker;
  }

  @VisibleForTesting
  @CheckReturnValue
  static ProtocolNegotiator createProtocolNegotiator(
      String authority,
      NegotiationType negotiationType,
      SslContext sslContext) {
    ProtocolNegotiator negotiator =
        createProtocolNegotiatorByType(authority, negotiationType, sslContext);
    String proxy = System.getenv("GRPC_PROXY_EXP");
    if (proxy != null) {
      String[] parts = proxy.split(":", 2);
      int port = 80;
      if (parts.length > 1) {
        port = Integer.parseInt(parts[1]);
      }
      InetSocketAddress proxyAddress = new InetSocketAddress(parts[0], port);
      negotiator = ProtocolNegotiators.httpProxy(proxyAddress, null, null, negotiator);
    }
    return negotiator;
  }

  @CheckReturnValue
  private static ProtocolNegotiator createProtocolNegotiatorByType(
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

  @CheckReturnValue
  interface OverrideAuthorityChecker {
    String checkAuthority(String authority);
  }

  @Override
  @CheckReturnValue
  protected String checkAuthority(String authority) {
    if (authorityChecker != null) {
      return authorityChecker.checkAuthority(authority);
    }
    return super.checkAuthority(authority);
  }

  void setDynamicParamsFactory(TransportCreationParamsFilterFactory factory) {
    this.dynamicParamsFactory = checkNotNull(factory, "factory");
  }

  interface TransportCreationParamsFilterFactory {
    @CheckReturnValue
    TransportCreationParamsFilter create(
        SocketAddress targetServerAddress, String authority, @Nullable String userAgent);
  }

  @CheckReturnValue
  interface TransportCreationParamsFilter {
    SocketAddress getTargetServerAddress();

    String getAuthority();

    @Nullable String getUserAgent();

    ProtocolNegotiator getProtocolNegotiator();
  }

  /**
   * Creates Netty transports. Exposed for internal use, as it should be private.
   */
  @CheckReturnValue
  private static final class NettyTransportFactory implements ClientTransportFactory {
    private final TransportCreationParamsFilterFactory transportCreationParamsFilterFactory;
    private final Class<? extends Channel> channelType;
    private final Map<ChannelOption<?>, ?> channelOptions;
    private final NegotiationType negotiationType;
    private final SslContext sslContext;
    private final EventLoopGroup group;
    private final boolean usingSharedGroup;
    private final int flowControlWindow;
    private final int maxMessageSize;
    private final int maxHeaderListSize;
    private final boolean enableKeepAlive;
    private final long keepAliveDelayNanos;
    private final long keepAliveTimeoutNanos;

    private boolean closed;

    NettyTransportFactory(TransportCreationParamsFilterFactory transportCreationParamsFilterFactory,
        Class<? extends Channel> channelType, Map<ChannelOption<?>, ?> channelOptions,
        NegotiationType negotiationType, SslContext sslContext, EventLoopGroup group,
        int flowControlWindow, int maxMessageSize, int maxHeaderListSize, boolean enableKeepAlive,
        long keepAliveDelayNanos, long keepAliveTimeoutNanos) {
      this.channelType = channelType;
      this.negotiationType = negotiationType;
      this.channelOptions = new HashMap<ChannelOption<?>, Object>(channelOptions);
      this.sslContext = sslContext;

      if (transportCreationParamsFilterFactory == null) {
        transportCreationParamsFilterFactory = new TransportCreationParamsFilterFactory() {
          @Override
          public TransportCreationParamsFilter create(
              SocketAddress targetServerAddress, String authority, String userAgent) {
            return new DynamicNettyTransportParams(targetServerAddress, authority, userAgent);
          }
        };
      }
      this.transportCreationParamsFilterFactory = transportCreationParamsFilterFactory;

      this.flowControlWindow = flowControlWindow;
      this.maxMessageSize = maxMessageSize;
      this.maxHeaderListSize = maxHeaderListSize;
      this.enableKeepAlive = enableKeepAlive;
      this.keepAliveDelayNanos = keepAliveDelayNanos;
      this.keepAliveTimeoutNanos = keepAliveTimeoutNanos;
      usingSharedGroup = group == null;
      if (usingSharedGroup) {
        // The group was unspecified, using the shared group.
        this.group = SharedResourceHolder.get(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP);
      } else {
        this.group = group;
      }
    }

    @Override
    public ConnectionClientTransport newClientTransport(
        SocketAddress serverAddress, String authority, @Nullable String userAgent) {
      checkState(!closed, "The transport factory is closed.");

      TransportCreationParamsFilter dparams =
          transportCreationParamsFilterFactory.create(serverAddress, authority, userAgent);

      NettyClientTransport transport = new NettyClientTransport(
          dparams.getTargetServerAddress(), channelType, channelOptions, group,
          dparams.getProtocolNegotiator(), flowControlWindow,
          maxMessageSize, maxHeaderListSize, dparams.getAuthority(), dparams.getUserAgent());
      if (enableKeepAlive) {
        transport.enableKeepAlive(true, keepAliveDelayNanos, keepAliveTimeoutNanos);
      }
      return transport;
    }

    @Override
    public void close() {
      if (closed) {
        return;
      }
      closed = true;

      if (usingSharedGroup) {
        SharedResourceHolder.release(Utils.DEFAULT_WORKER_EVENT_LOOP_GROUP, group);
      }
    }

    @CheckReturnValue
    private final class DynamicNettyTransportParams implements TransportCreationParamsFilter {

      private final SocketAddress targetServerAddress;
      private final String authority;
      @Nullable private final String userAgent;

      private DynamicNettyTransportParams(
          SocketAddress targetServerAddress, String authority, String userAgent) {
        this.targetServerAddress = targetServerAddress;
        this.authority = authority;
        this.userAgent = userAgent;
      }

      @Override
      public SocketAddress getTargetServerAddress() {
        return targetServerAddress;
      }

      @Override
      public String getAuthority() {
        return authority;
      }

      @Override
      public String getUserAgent() {
        return userAgent;
      }

      @Override
      public ProtocolNegotiator getProtocolNegotiator() {
        return createProtocolNegotiator(authority, negotiationType, sslContext);
      }
    }
  }
}
