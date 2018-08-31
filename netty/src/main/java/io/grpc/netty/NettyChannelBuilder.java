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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.internal.GrpcUtil.DEFAULT_KEEPALIVE_TIMEOUT_NANOS;
import static io.grpc.internal.GrpcUtil.DEFAULT_KEEPALIVE_TIME_NANOS;
import static io.grpc.internal.GrpcUtil.KEEPALIVE_TIME_NANOS_DISABLED;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.errorprone.annotations.CanIgnoreReturnValue;
import io.grpc.Attributes;
import io.grpc.ExperimentalApi;
import io.grpc.Internal;
import io.grpc.NameResolver;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.AtomicBackoff;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.ConnectionClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.KeepAliveManager;
import io.grpc.internal.ProxyParameters;
import io.grpc.internal.SharedResourceHolder;
import io.grpc.internal.TransportTracer;
import io.netty.channel.Channel;
import io.netty.channel.ChannelOption;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.ssl.SslContext;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.ScheduledExecutorService;
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

  private static final long AS_LARGE_AS_INFINITE = TimeUnit.DAYS.toNanos(1000L);

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
  private long keepAliveTimeNanos = KEEPALIVE_TIME_NANOS_DISABLED;
  private long keepAliveTimeoutNanos = DEFAULT_KEEPALIVE_TIMEOUT_NANOS;
  private boolean keepAliveWithoutCalls;
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
   * Sets the maximum size of header list allowed to be received. This is cumulative size of the
   * headers with some overhead, as defined for
   * <a href="http://httpwg.org/specs/rfc7540.html#rfc.section.6.5.2">
   * HTTP/2's SETTINGS_MAX_HEADER_LIST_SIZE</a>. The default is 8 KiB.
   */
  public NettyChannelBuilder maxHeaderListSize(int maxHeaderListSize) {
    checkArgument(maxHeaderListSize > 0, "maxHeaderListSize must be > 0");
    this.maxHeaderListSize = maxHeaderListSize;
    return this;
  }

  /**
   * Equivalent to using {@link #negotiationType(NegotiationType)} with {@code PLAINTEXT} or
   * {@code PLAINTEXT_UPGRADE}.
   *
   * @deprecated use {@link #usePlaintext()} instead.
   */
  @Override
  @Deprecated
  public NettyChannelBuilder usePlaintext(boolean skipNegotiation) {
    if (skipNegotiation) {
      negotiationType(NegotiationType.PLAINTEXT);
    } else {
      negotiationType(NegotiationType.PLAINTEXT_UPGRADE);
    }
    return this;
  }

  /**
   * Equivalent to using {@link #negotiationType(NegotiationType)} with {@code PLAINTEXT}.
   */
  @Override
  public NettyChannelBuilder usePlaintext() {
    negotiationType(NegotiationType.PLAINTEXT);
    return this;
  }

  /**
   * Equivalent to using {@link #negotiationType(NegotiationType)} with {@code TLS}.
   */
  @Override
  public NettyChannelBuilder useTransportSecurity() {
    negotiationType(NegotiationType.TLS);
    return this;
  }

  /**
   * Enable keepalive with default delay and timeout.
   *
   * @deprecated Please use {@link #keepAliveTime} and {@link #keepAliveTimeout} instead
   */
  @Deprecated
  public final NettyChannelBuilder enableKeepAlive(boolean enable) {
    if (enable) {
      return keepAliveTime(DEFAULT_KEEPALIVE_TIME_NANOS, TimeUnit.NANOSECONDS);
    }
    return keepAliveTime(KEEPALIVE_TIME_NANOS_DISABLED, TimeUnit.NANOSECONDS);
  }

  /**
   * Enable keepalive with custom delay and timeout.
   *
   * @deprecated Please use {@link #keepAliveTime} and {@link #keepAliveTimeout} instead
   */
  @Deprecated
  public final NettyChannelBuilder enableKeepAlive(boolean enable, long keepAliveTime,
      TimeUnit delayUnit, long keepAliveTimeout, TimeUnit timeoutUnit) {
    if (enable) {
      return keepAliveTime(keepAliveTime, delayUnit)
          .keepAliveTimeout(keepAliveTimeout, timeoutUnit);
    }
    return keepAliveTime(KEEPALIVE_TIME_NANOS_DISABLED, TimeUnit.NANOSECONDS);
  }

  /**
   * {@inheritDoc}
   *
   * @since 1.3.0
   */
  @Override
  public NettyChannelBuilder keepAliveTime(long keepAliveTime, TimeUnit timeUnit) {
    checkArgument(keepAliveTime > 0L, "keepalive time must be positive");
    keepAliveTimeNanos = timeUnit.toNanos(keepAliveTime);
    keepAliveTimeNanos = KeepAliveManager.clampKeepAliveTimeInNanos(keepAliveTimeNanos);
    if (keepAliveTimeNanos >= AS_LARGE_AS_INFINITE) {
      // Bump keepalive time to infinite. This disables keepalive.
      keepAliveTimeNanos = KEEPALIVE_TIME_NANOS_DISABLED;
    }
    return this;
  }

  /**
   * {@inheritDoc}
   *
   * @since 1.3.0
   */
  @Override
  public NettyChannelBuilder keepAliveTimeout(long keepAliveTimeout, TimeUnit timeUnit) {
    checkArgument(keepAliveTimeout > 0L, "keepalive timeout must be positive");
    keepAliveTimeoutNanos = timeUnit.toNanos(keepAliveTimeout);
    keepAliveTimeoutNanos = KeepAliveManager.clampKeepAliveTimeoutInNanos(keepAliveTimeoutNanos);
    return this;
  }

  /**
   * {@inheritDoc}
   *
   * @since 1.3.0
   */
  @Override
  public NettyChannelBuilder keepAliveWithoutCalls(boolean enable) {
    keepAliveWithoutCalls = enable;
    return this;
  }

  @Override
  @CheckReturnValue
  @Internal
  protected ClientTransportFactory buildTransportFactory() {
    TransportCreationParamsFilterFactory transportCreationParamsFilterFactory =
        dynamicParamsFactory;
    if (transportCreationParamsFilterFactory == null) {
      SslContext localSslContext = sslContext;
      if (negotiationType == NegotiationType.TLS && localSslContext == null) {
        try {
          localSslContext = GrpcSslContexts.forClient().build();
        } catch (SSLException ex) {
          throw new RuntimeException(ex);
        }
      }
      ProtocolNegotiator negotiator =
          createProtocolNegotiatorByType(negotiationType, localSslContext);
      transportCreationParamsFilterFactory =
          new DefaultNettyTransportCreationParamsFilterFactory(negotiator);
    }
    return new NettyTransportFactory(
        transportCreationParamsFilterFactory, channelType, channelOptions,
        eventLoopGroup, flowControlWindow, maxInboundMessageSize(),
        maxHeaderListSize, keepAliveTimeNanos, keepAliveTimeoutNanos, keepAliveWithoutCalls,
        transportTracerFactory.create());
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
  static ProtocolNegotiator createProtocolNegotiatorByType(
      NegotiationType negotiationType,
      SslContext sslContext) {
    switch (negotiationType) {
      case PLAINTEXT:
        return ProtocolNegotiators.plaintext();
      case PLAINTEXT_UPGRADE:
        return ProtocolNegotiators.plaintextUpgrade();
      case TLS:
        return ProtocolNegotiators.tls(sslContext);
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
  @Internal
  protected String checkAuthority(String authority) {
    if (authorityChecker != null) {
      return authorityChecker.checkAuthority(authority);
    }
    return super.checkAuthority(authority);
  }

  void setDynamicParamsFactory(TransportCreationParamsFilterFactory factory) {
    this.dynamicParamsFactory = checkNotNull(factory, "factory");
  }

  @Override
  protected void setTracingEnabled(boolean value) {
    super.setTracingEnabled(value);
  }

  @Override
  protected void setStatsEnabled(boolean value) {
    super.setStatsEnabled(value);
  }

  @Override
  protected void setStatsRecordStartedRpcs(boolean value) {
    super.setStatsRecordStartedRpcs(value);
  }

  @VisibleForTesting
  NettyChannelBuilder setTransportTracerFactory(TransportTracer.Factory transportTracerFactory) {
    this.transportTracerFactory = transportTracerFactory;
    return this;
  }

  interface TransportCreationParamsFilterFactory {
    @CheckReturnValue
    TransportCreationParamsFilter create(
        SocketAddress targetServerAddress,
        String authority,
        @Nullable String userAgent,
        @Nullable ProxyParameters proxy);
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
    private final EventLoopGroup group;
    private final boolean usingSharedGroup;
    private final int flowControlWindow;
    private final int maxMessageSize;
    private final int maxHeaderListSize;
    private final AtomicBackoff keepAliveTimeNanos;
    private final long keepAliveTimeoutNanos;
    private final boolean keepAliveWithoutCalls;
    private final TransportTracer transportTracer;

    private boolean closed;

    NettyTransportFactory(TransportCreationParamsFilterFactory transportCreationParamsFilterFactory,
        Class<? extends Channel> channelType, Map<ChannelOption<?>, ?> channelOptions,
        EventLoopGroup group, int flowControlWindow, int maxMessageSize, int maxHeaderListSize,
        long keepAliveTimeNanos, long keepAliveTimeoutNanos, boolean keepAliveWithoutCalls,
        TransportTracer transportTracer) {
      this.transportCreationParamsFilterFactory = transportCreationParamsFilterFactory;
      this.channelType = channelType;
      this.channelOptions = new HashMap<ChannelOption<?>, Object>(channelOptions);
      this.flowControlWindow = flowControlWindow;
      this.maxMessageSize = maxMessageSize;
      this.maxHeaderListSize = maxHeaderListSize;
      this.keepAliveTimeNanos = new AtomicBackoff("keepalive time nanos", keepAliveTimeNanos);
      this.keepAliveTimeoutNanos = keepAliveTimeoutNanos;
      this.keepAliveWithoutCalls = keepAliveWithoutCalls;
      this.transportTracer = transportTracer;

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
        SocketAddress serverAddress, ClientTransportOptions options) {
      checkState(!closed, "The transport factory is closed.");

      TransportCreationParamsFilter dparams =
          transportCreationParamsFilterFactory.create(
              serverAddress,
              options.getAuthority(),
              options.getUserAgent(),
              options.getProxyParameters());

      final AtomicBackoff.State keepAliveTimeNanosState = keepAliveTimeNanos.getState();
      Runnable tooManyPingsRunnable = new Runnable() {
        @Override
        public void run() {
          keepAliveTimeNanosState.backoff();
        }
      };
      NettyClientTransport transport = new NettyClientTransport(
          dparams.getTargetServerAddress(), channelType, channelOptions, group,
          dparams.getProtocolNegotiator(), flowControlWindow,
          maxMessageSize, maxHeaderListSize, keepAliveTimeNanosState.get(), keepAliveTimeoutNanos,
          keepAliveWithoutCalls, dparams.getAuthority(), dparams.getUserAgent(),
          tooManyPingsRunnable, transportTracer, options.getEagAttributes());
      return transport;
    }

    @Override
    public ScheduledExecutorService getScheduledExecutorService() {
      return group;
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
  }

  private static final class DefaultNettyTransportCreationParamsFilterFactory
      implements TransportCreationParamsFilterFactory {
    final ProtocolNegotiator negotiator;

    DefaultNettyTransportCreationParamsFilterFactory(ProtocolNegotiator negotiator) {
      this.negotiator = negotiator;
    }

    @Override
    public TransportCreationParamsFilter create(
        SocketAddress targetServerAddress,
        String authority,
        String userAgent,
        ProxyParameters proxyParams) {
      ProtocolNegotiator localNegotiator = negotiator;
      if (proxyParams != null) {
        localNegotiator = ProtocolNegotiators.httpProxy(
            proxyParams.proxyAddress, proxyParams.username, proxyParams.password, negotiator);
      }
      return new DynamicNettyTransportParams(
          targetServerAddress, authority, userAgent, localNegotiator);
    }
  }

  @CheckReturnValue
  private static final class DynamicNettyTransportParams implements TransportCreationParamsFilter {

    private final SocketAddress targetServerAddress;
    private final String authority;
    @Nullable private final String userAgent;
    private final ProtocolNegotiator protocolNegotiator;

    private DynamicNettyTransportParams(
        SocketAddress targetServerAddress,
        String authority,
        String userAgent,
        ProtocolNegotiator protocolNegotiator) {
      this.targetServerAddress = targetServerAddress;
      this.authority = authority;
      this.userAgent = userAgent;
      this.protocolNegotiator = protocolNegotiator;
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
      return protocolNegotiator;
    }
  }
}
