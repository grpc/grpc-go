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

package io.grpc.okhttp;

import static com.google.common.base.Preconditions.checkNotNull;
import static io.grpc.internal.GrpcUtil.DEFAULT_KEEPALIVE_TIMEOUT_NANOS;
import static io.grpc.internal.GrpcUtil.DEFAULT_KEEPALIVE_TIME_NANOS;
import static io.grpc.internal.GrpcUtil.KEEPALIVE_TIME_NANOS_DISABLED;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
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
import io.grpc.internal.SharedResourceHolder;
import io.grpc.internal.SharedResourceHolder.Resource;
import io.grpc.internal.TransportTracer;
import io.grpc.okhttp.internal.CipherSuite;
import io.grpc.okhttp.internal.ConnectionSpec;
import io.grpc.okhttp.internal.Platform;
import io.grpc.okhttp.internal.TlsVersion;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.security.GeneralSecurityException;
import java.security.KeyStore;
import java.security.SecureRandom;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;
import javax.net.ssl.HostnameVerifier;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManagerFactory;

/** Convenience class for building channels with the OkHttp transport. */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1785")
public class OkHttpChannelBuilder extends
        AbstractManagedChannelImplBuilder<OkHttpChannelBuilder> {

  /** Identifies the negotiation used for starting up HTTP/2. */
  private enum NegotiationType {
    /** Uses TLS ALPN/NPN negotiation, assumes an SSL connection. */
    TLS,

    /**
     * Just assume the connection is plaintext (non-SSL) and the remote endpoint supports HTTP/2
     * directly without an upgrade.
     */
    PLAINTEXT
  }

  /**
   * ConnectionSpec closely matching the default configuration that could be used as a basis for
   * modification.
   *
   * <p>Since this field is the only reference in gRPC to ConnectionSpec that may not be ProGuarded,
   * we are removing the field to reduce method count. We've been unable to find any existing users
   * of the field, and any such user would highly likely at least be changing the cipher suites,
   * which is sort of the only part that's non-obvious. Any existing user should instead create
   * their own spec from scratch or base it off ConnectionSpec.MODERN_TLS if believed to be
   * necessary. If this was providing you with value and don't want to see it removed, open a GitHub
   * issue to discuss keeping it.
   *
   * @deprecated Deemed of little benefit and users weren't using it. Just define one yourself
   */
  @Deprecated
  public static final com.squareup.okhttp.ConnectionSpec DEFAULT_CONNECTION_SPEC =
      new com.squareup.okhttp.ConnectionSpec.Builder(com.squareup.okhttp.ConnectionSpec.MODERN_TLS)
          .cipherSuites(
              // The following items should be sync with Netty's Http2SecurityUtil.CIPHERS.
              com.squareup.okhttp.CipherSuite.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
              com.squareup.okhttp.CipherSuite.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
              com.squareup.okhttp.CipherSuite.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
              com.squareup.okhttp.CipherSuite.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
              com.squareup.okhttp.CipherSuite.TLS_DHE_RSA_WITH_AES_128_GCM_SHA256,
              com.squareup.okhttp.CipherSuite.TLS_DHE_DSS_WITH_AES_128_GCM_SHA256,
              com.squareup.okhttp.CipherSuite.TLS_DHE_RSA_WITH_AES_256_GCM_SHA384,
              com.squareup.okhttp.CipherSuite.TLS_DHE_DSS_WITH_AES_256_GCM_SHA384)
          .tlsVersions(com.squareup.okhttp.TlsVersion.TLS_1_2)
          .supportsTlsExtensions(true)
          .build();

  @VisibleForTesting
  static final ConnectionSpec INTERNAL_DEFAULT_CONNECTION_SPEC =
      new ConnectionSpec.Builder(ConnectionSpec.MODERN_TLS)
          .cipherSuites(
              // The following items should be sync with Netty's Http2SecurityUtil.CIPHERS.
              CipherSuite.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
              CipherSuite.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
              CipherSuite.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
              CipherSuite.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
              CipherSuite.TLS_DHE_RSA_WITH_AES_128_GCM_SHA256,
              CipherSuite.TLS_DHE_DSS_WITH_AES_128_GCM_SHA256,
              CipherSuite.TLS_DHE_RSA_WITH_AES_256_GCM_SHA384,
              CipherSuite.TLS_DHE_DSS_WITH_AES_256_GCM_SHA384)
          .tlsVersions(TlsVersion.TLS_1_2)
          .supportsTlsExtensions(true)
          .build();

  private static final long AS_LARGE_AS_INFINITE = TimeUnit.DAYS.toNanos(1000L);
  private static final Resource<ExecutorService> SHARED_EXECUTOR =
      new Resource<ExecutorService>() {
        @Override
        public ExecutorService create() {
          return Executors.newCachedThreadPool(GrpcUtil.getThreadFactory("grpc-okhttp-%d", true));
        }

        @Override
        public void close(ExecutorService executor) {
          executor.shutdown();
        }
      };

  /** Creates a new builder for the given server host and port. */
  public static OkHttpChannelBuilder forAddress(String host, int port) {
    return new OkHttpChannelBuilder(host, port);
  }

  /**
   * Creates a new builder for the given target that will be resolved by
   * {@link io.grpc.NameResolver}.
   */
  public static OkHttpChannelBuilder forTarget(String target) {
    return new OkHttpChannelBuilder(target);
  }

  private Executor transportExecutor;
  private ScheduledExecutorService scheduledExecutorService;

  private SSLSocketFactory sslSocketFactory;
  private HostnameVerifier hostnameVerifier;
  private ConnectionSpec connectionSpec = INTERNAL_DEFAULT_CONNECTION_SPEC;
  private NegotiationType negotiationType = NegotiationType.TLS;
  private long keepAliveTimeNanos = KEEPALIVE_TIME_NANOS_DISABLED;
  private long keepAliveTimeoutNanos = DEFAULT_KEEPALIVE_TIMEOUT_NANOS;
  private boolean keepAliveWithoutCalls;

  protected OkHttpChannelBuilder(String host, int port) {
    this(GrpcUtil.authorityFromHostAndPort(host, port));
  }

  private OkHttpChannelBuilder(String target) {
    super(target);
  }

  @VisibleForTesting
  final OkHttpChannelBuilder setTransportTracerFactory(
      TransportTracer.Factory transportTracerFactory) {
    this.transportTracerFactory = transportTracerFactory;
    return this;
  }

  /**
   * Override the default executor necessary for internal transport use.
   *
   * <p>The channel does not take ownership of the given executor. It is the caller' responsibility
   * to shutdown the executor when appropriate.
   */
  public final OkHttpChannelBuilder transportExecutor(@Nullable Executor transportExecutor) {
    this.transportExecutor = transportExecutor;
    return this;
  }

  /**
   * Sets the negotiation type for the HTTP/2 connection.
   *
   * <p>If TLS is enabled a default {@link SSLSocketFactory} is created using the best
   * {@link java.security.Provider} available and is NOT based on
   * {@link SSLSocketFactory#getDefault}. To more precisely control the TLS configuration call
   * {@link #sslSocketFactory} to override the socket factory used.
   *
   * <p>Default: <code>TLS</code>
   *
   * @deprecated use {@link #usePlaintext()} or {@link #useTransportSecurity()} instead.
   */
  @Deprecated
  public final OkHttpChannelBuilder negotiationType(io.grpc.okhttp.NegotiationType type) {
    Preconditions.checkNotNull(type, "type");
    switch (type) {
      case TLS:
        negotiationType = NegotiationType.TLS;
        break;
      case PLAINTEXT:
        negotiationType = NegotiationType.PLAINTEXT;
        break;
      default:
        throw new AssertionError("Unknown negotiation type: " + type);
    }
    return this;
  }

  /**
   * Enable keepalive with default delay and timeout.
   *
   * @deprecated Use {@link #keepAliveTime} instead
   */
  @Deprecated
  public final OkHttpChannelBuilder enableKeepAlive(boolean enable) {
    if (enable) {
      return keepAliveTime(DEFAULT_KEEPALIVE_TIME_NANOS, TimeUnit.NANOSECONDS);
    } else {
      return keepAliveTime(KEEPALIVE_TIME_NANOS_DISABLED, TimeUnit.NANOSECONDS);
    }
  }

  /**
   * Enable keepalive with custom delay and timeout.
   *
   * @deprecated Use {@link #keepAliveTime} and {@link #keepAliveTimeout} instead
   */
  @Deprecated
  public final OkHttpChannelBuilder enableKeepAlive(boolean enable, long keepAliveTime,
      TimeUnit delayUnit, long keepAliveTimeout, TimeUnit timeoutUnit) {
    if (enable) {
      return keepAliveTime(keepAliveTime, delayUnit)
          .keepAliveTimeout(keepAliveTimeout, timeoutUnit);
    } else {
      return keepAliveTime(KEEPALIVE_TIME_NANOS_DISABLED, TimeUnit.NANOSECONDS);
    }
  }

  /**
   * {@inheritDoc}
   *
   * @since 1.3.0
   */
  @Override
  public OkHttpChannelBuilder keepAliveTime(long keepAliveTime, TimeUnit timeUnit) {
    Preconditions.checkArgument(keepAliveTime > 0L, "keepalive time must be positive");
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
  public OkHttpChannelBuilder keepAliveTimeout(long keepAliveTimeout, TimeUnit timeUnit) {
    Preconditions.checkArgument(keepAliveTimeout > 0L, "keepalive timeout must be positive");
    keepAliveTimeoutNanos = timeUnit.toNanos(keepAliveTimeout);
    keepAliveTimeoutNanos = KeepAliveManager.clampKeepAliveTimeoutInNanos(keepAliveTimeoutNanos);
    return this;
  }

  /**
   * {@inheritDoc}
   *
   * @since 1.3.0
   * @see #keepAliveTime(long, TimeUnit)
   */
  @Override
  public OkHttpChannelBuilder keepAliveWithoutCalls(boolean enable) {
    keepAliveWithoutCalls = enable;
    return this;
  }

  /**
   * Override the default {@link SSLSocketFactory} and enable TLS negotiation.
   */
  public final OkHttpChannelBuilder sslSocketFactory(SSLSocketFactory factory) {
    this.sslSocketFactory = factory;
    negotiationType = NegotiationType.TLS;
    return this;
  }

  /**
   * Set the hostname verifier to use when using TLS negotiation. The hostnameVerifier is only used
   * if using TLS negotiation. If the hostname verifier is not set, a default hostname verifier is
   * used.
   *
   * <p>Be careful when setting a custom hostname verifier! By setting a non-null value, you are
   * replacing all default verification behavior. If the hostname verifier you supply does not
   * effectively supply the same checks, you may be removing the security assurances that TLS aims
   * to provide.</p>
   *
   * <p>This method should not be used to avoid hostname verification, even during testing, since
   * {@link #overrideAuthority} is a safer alternative as it does not disable any security checks.
   * </p>
   *
   * @see io.grpc.okhttp.internal.OkHostnameVerifier
   *
   * @since 1.6.0
   * @return this
   *
   */
  public final OkHttpChannelBuilder hostnameVerifier(@Nullable HostnameVerifier hostnameVerifier) {
    this.hostnameVerifier = hostnameVerifier;
    return this;
  }

  /**
   * For secure connection, provides a ConnectionSpec to specify Cipher suite and
   * TLS versions.
   *
   * <p>By default a modern, HTTP/2-compatible spec will be used.
   *
   * <p>This method is only used when building a secure connection. For plaintext
   * connection, use {@link #usePlaintext()} instead.
   *
   * @throws IllegalArgumentException
   *         If {@code connectionSpec} is not with TLS
   */
  public final OkHttpChannelBuilder connectionSpec(
      com.squareup.okhttp.ConnectionSpec connectionSpec) {
    Preconditions.checkArgument(connectionSpec.isTls(), "plaintext ConnectionSpec is not accepted");
    this.connectionSpec = Utils.convertSpec(connectionSpec);
    return this;
  }

  /**
   * Equivalent to using {@link #negotiationType} with {@code PLAINTEXT}.
   *
   * @deprecated use {@link #usePlaintext()} instead.
   */
  @Override
  @Deprecated
  public final OkHttpChannelBuilder usePlaintext(boolean skipNegotiation) {
    if (skipNegotiation) {
      negotiationType(io.grpc.okhttp.NegotiationType.PLAINTEXT);
    } else {
      throw new IllegalArgumentException("Plaintext negotiation not currently supported");
    }
    return this;
  }

  /** Sets the negotiation type for the HTTP/2 connection to plaintext. */
  @Override
  public final OkHttpChannelBuilder usePlaintext() {
    negotiationType = NegotiationType.PLAINTEXT;
    return this;
  }

  /**
   * Sets the negotiation type for the HTTP/2 connection to TLS (this is the default).
   *
   * <p>With TLS enabled, a default {@link SSLSocketFactory} is created using the best {@link
   * java.security.Provider} available and is NOT based on {@link SSLSocketFactory#getDefault}. To
   * more precisely control the TLS configuration call {@link #sslSocketFactory} to override the
   * socket factory used.
   */
  @Override
  public final OkHttpChannelBuilder useTransportSecurity() {
    negotiationType = NegotiationType.TLS;
    return this;
  }

  /**
   * Provides a custom scheduled executor service.
   *
   * <p>It's an optional parameter. If the user has not provided a scheduled executor service when
   * the channel is built, the builder will use a static cached thread pool.
   *
   * @return this
   *
   * @since 1.11.0
   */
  public final OkHttpChannelBuilder scheduledExecutorService(
      ScheduledExecutorService scheduledExecutorService) {
    this.scheduledExecutorService =
        checkNotNull(scheduledExecutorService, "scheduledExecutorService");
    return this;
  }

  @Override
  @Internal
  protected final ClientTransportFactory buildTransportFactory() {
    boolean enableKeepAlive = keepAliveTimeNanos != KEEPALIVE_TIME_NANOS_DISABLED;
    return new OkHttpTransportFactory(transportExecutor, scheduledExecutorService,
        createSocketFactory(), hostnameVerifier, connectionSpec, maxInboundMessageSize(),
        enableKeepAlive, keepAliveTimeNanos, keepAliveTimeoutNanos, keepAliveWithoutCalls,
        transportTracerFactory);
  }

  @Override
  protected Attributes getNameResolverParams() {
    int defaultPort;
    switch (negotiationType) {
      case PLAINTEXT:
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
  @Nullable
  SSLSocketFactory createSocketFactory() {
    switch (negotiationType) {
      case TLS:
        try {
          if (sslSocketFactory == null) {
            SSLContext sslContext;
            if (GrpcUtil.IS_RESTRICTED_APPENGINE) {
              // The following auth code circumvents the following AccessControlException:
              // access denied ("java.util.PropertyPermission" "javax.net.ssl.keyStore" "read")
              // Conscrypt will attempt to load the default KeyStore if a trust manager is not
              // provided, which is forbidden on AppEngine
              sslContext = SSLContext.getInstance("TLS", Platform.get().getProvider());
              TrustManagerFactory trustManagerFactory =
                  TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
              trustManagerFactory.init((KeyStore) null);
              sslContext.init(
                  null,
                  trustManagerFactory.getTrustManagers(),
                  // Use an algorithm that doesn't need /dev/urandom
                  SecureRandom.getInstance("SHA1PRNG", Platform.get().getProvider()));

            } else {
              sslContext = SSLContext.getInstance("Default", Platform.get().getProvider());
            }
            sslSocketFactory = sslContext.getSocketFactory();
          }
          return sslSocketFactory;
        } catch (GeneralSecurityException gse) {
          throw new RuntimeException("TLS Provider failure", gse);
        }
      case PLAINTEXT:
        return null;
      default:
        throw new RuntimeException("Unknown negotiation type: " + negotiationType);
    }
  }

  /**
   * Creates OkHttp transports. Exposed for internal use, as it should be private.
   */
  @Internal
  static final class OkHttpTransportFactory implements ClientTransportFactory {
    private final Executor executor;
    private final boolean usingSharedExecutor;
    private final boolean usingSharedScheduler;
    private final TransportTracer.Factory transportTracerFactory;
    @Nullable
    private final SSLSocketFactory socketFactory;
    @Nullable
    private final HostnameVerifier hostnameVerifier;
    private final ConnectionSpec connectionSpec;
    private final int maxMessageSize;
    private final boolean enableKeepAlive;
    private final AtomicBackoff keepAliveTimeNanos;
    private final long keepAliveTimeoutNanos;
    private final boolean keepAliveWithoutCalls;
    private final ScheduledExecutorService timeoutService;
    private boolean closed;

    private OkHttpTransportFactory(Executor executor,
        @Nullable ScheduledExecutorService timeoutService,
        @Nullable SSLSocketFactory socketFactory,
        @Nullable HostnameVerifier hostnameVerifier,
        ConnectionSpec connectionSpec,
        int maxMessageSize,
        boolean enableKeepAlive,
        long keepAliveTimeNanos,
        long keepAliveTimeoutNanos,
        boolean keepAliveWithoutCalls,
        TransportTracer.Factory transportTracerFactory) {
      usingSharedScheduler = timeoutService == null;
      this.timeoutService = usingSharedScheduler
          ? SharedResourceHolder.get(GrpcUtil.TIMER_SERVICE) : timeoutService;
      this.socketFactory = socketFactory;
      this.hostnameVerifier = hostnameVerifier;
      this.connectionSpec = connectionSpec;
      this.maxMessageSize = maxMessageSize;
      this.enableKeepAlive = enableKeepAlive;
      this.keepAliveTimeNanos = new AtomicBackoff("keepalive time nanos", keepAliveTimeNanos);
      this.keepAliveTimeoutNanos = keepAliveTimeoutNanos;
      this.keepAliveWithoutCalls = keepAliveWithoutCalls;

      usingSharedExecutor = executor == null;
      this.transportTracerFactory =
          Preconditions.checkNotNull(transportTracerFactory, "transportTracerFactory");
      if (usingSharedExecutor) {
        // The executor was unspecified, using the shared executor.
        this.executor = SharedResourceHolder.get(SHARED_EXECUTOR);
      } else {
        this.executor = executor;
      }
    }

    @Override
    public ConnectionClientTransport newClientTransport(
        SocketAddress addr, ClientTransportOptions options) {
      if (closed) {
        throw new IllegalStateException("The transport factory is closed.");
      }
      final AtomicBackoff.State keepAliveTimeNanosState = keepAliveTimeNanos.getState();
      Runnable tooManyPingsRunnable = new Runnable() {
        @Override
        public void run() {
          keepAliveTimeNanosState.backoff();
        }
      };
      InetSocketAddress inetSocketAddr = (InetSocketAddress) addr;
      OkHttpClientTransport transport = new OkHttpClientTransport(
          inetSocketAddr,
          options.getAuthority(),
          options.getUserAgent(),
          executor,
          socketFactory,
          hostnameVerifier,
          connectionSpec,
          maxMessageSize,
          options.getProxyParameters(),
          tooManyPingsRunnable,
          transportTracerFactory.create());
      if (enableKeepAlive) {
        transport.enableKeepAlive(
            true, keepAliveTimeNanosState.get(), keepAliveTimeoutNanos, keepAliveWithoutCalls);
      }
      return transport;
    }

    @Override
    public ScheduledExecutorService getScheduledExecutorService() {
      return timeoutService;
    }

    @Override
    public void close() {
      if (closed) {
        return;
      }
      closed = true;

      if (usingSharedScheduler) {
        SharedResourceHolder.release(GrpcUtil.TIMER_SERVICE, timeoutService);
      }

      if (usingSharedExecutor) {
        SharedResourceHolder.release(SHARED_EXECUTOR, (ExecutorService) executor);
      }
    }
  }
}
