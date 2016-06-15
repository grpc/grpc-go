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

package io.grpc.okhttp;

import static com.google.common.base.Preconditions.checkArgument;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ThreadFactoryBuilder;

import com.squareup.okhttp.CipherSuite;
import com.squareup.okhttp.ConnectionSpec;
import com.squareup.okhttp.TlsVersion;

import io.grpc.Attributes;
import io.grpc.ExperimentalApi;
import io.grpc.Internal;
import io.grpc.NameResolver;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.ConnectionClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourceHolder;
import io.grpc.internal.SharedResourceHolder.Resource;

import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import javax.annotation.Nullable;
import javax.net.ssl.SSLSocketFactory;

/** Convenience class for building channels with the OkHttp transport. */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1785")
public class OkHttpChannelBuilder extends
        AbstractManagedChannelImplBuilder<OkHttpChannelBuilder> {

  public static final ConnectionSpec DEFAULT_CONNECTION_SPEC =
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

  private static final Resource<ExecutorService> SHARED_EXECUTOR =
      new Resource<ExecutorService>() {
        @Override
        public ExecutorService create() {
          return Executors.newCachedThreadPool(new ThreadFactoryBuilder()
              .setDaemon(true)
              .setNameFormat("grpc-okhttp-%d")
              .build());
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

  private SSLSocketFactory sslSocketFactory;
  private ConnectionSpec connectionSpec = DEFAULT_CONNECTION_SPEC;
  private NegotiationType negotiationType = NegotiationType.TLS;
  private int maxMessageSize = DEFAULT_MAX_MESSAGE_SIZE;

  protected OkHttpChannelBuilder(String host, int port) {
    this(GrpcUtil.authorityFromHostAndPort(host, port));
  }

  private OkHttpChannelBuilder(String target) {
    super(target);
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
   * <p>Default: <code>TLS</code>
   */
  public final OkHttpChannelBuilder negotiationType(NegotiationType type) {
    negotiationType = Preconditions.checkNotNull(type);
    return this;
  }

  /**
   * Provides a SSLSocketFactory to replace the default SSLSocketFactory used for TLS.
   *
   * <p>By default, when TLS is enabled, <code>SSLSocketFactory.getDefault()</code> will be used.
   *
   * <p>{@link NegotiationType#TLS} will be applied by calling this method.
   */
  public final OkHttpChannelBuilder sslSocketFactory(SSLSocketFactory factory) {
    this.sslSocketFactory = factory;
    negotiationType(NegotiationType.TLS);
    return this;
  }

  /**
   * For secure connection, provides a ConnectionSpec to specify Cipher suite and
   * TLS versions.
   *
   * <p>By default {@link #DEFAULT_CONNECTION_SPEC} will be used.
   *
   * <p>This method is only used when building a secure connection. For plaintext
   * connection, use {@link #usePlaintext} instead.
   *
   * @throws IllegalArgumentException
   *         If {@code connectionSpec} is not with TLS
   */
  public final OkHttpChannelBuilder connectionSpec(ConnectionSpec connectionSpec) {
    Preconditions.checkArgument(connectionSpec.isTls(), "plaintext ConnectionSpec is not accepted");
    this.connectionSpec = connectionSpec;
    return this;
  }

  /**
   * Sets the maximum message size allowed to be received on the channel. If not called,
   * defaults to {@link io.grpc.internal.GrpcUtil#DEFAULT_MAX_MESSAGE_SIZE}.
   */
  public final OkHttpChannelBuilder maxMessageSize(int maxMessageSize) {
    checkArgument(maxMessageSize >= 0, "maxMessageSize must be >= 0");
    this.maxMessageSize = maxMessageSize;
    return this;
  }

  /**
   * Equivalent to using {@link #negotiationType(NegotiationType)} with {@code PLAINTEXT}.
   */
  @Override
  public final OkHttpChannelBuilder usePlaintext(boolean skipNegotiation) {
    if (skipNegotiation) {
      negotiationType(NegotiationType.PLAINTEXT);
    } else {
      throw new IllegalArgumentException("Plaintext negotiation not currently supported");
    }
    return this;
  }

  @Override
  protected final ClientTransportFactory buildTransportFactory() {
    return new OkHttpTransportFactory(transportExecutor,
        createSocketFactory(), connectionSpec, maxMessageSize);
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
        return sslSocketFactory == null
            ? (SSLSocketFactory) SSLSocketFactory.getDefault() : sslSocketFactory;
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
    @Nullable
    private final SSLSocketFactory socketFactory;
    private final ConnectionSpec connectionSpec;
    private final int maxMessageSize;

    private boolean closed;

    private OkHttpTransportFactory(Executor executor,
        @Nullable SSLSocketFactory socketFactory,
        ConnectionSpec connectionSpec,
        int maxMessageSize) {
      this.socketFactory = socketFactory;
      this.connectionSpec = connectionSpec;
      this.maxMessageSize = maxMessageSize;

      usingSharedExecutor = executor == null;
      if (usingSharedExecutor) {
        // The executor was unspecified, using the shared executor.
        this.executor = SharedResourceHolder.get(SHARED_EXECUTOR);
      } else {
        this.executor = executor;
      }
    }

    @Override
    public ConnectionClientTransport newClientTransport(
        SocketAddress addr, String authority, @Nullable String userAgent) {
      if (closed) {
        throw new IllegalStateException("The transport factory is closed.");
      }
      InetSocketAddress inetSocketAddr = (InetSocketAddress) addr;
      return new OkHttpClientTransport(inetSocketAddr, authority, userAgent, executor,
          socketFactory, Utils.convertSpec(connectionSpec), maxMessageSize);
    }

    @Override
    public void close() {
      if (closed) {
        return;
      }
      closed = true;

      if (usingSharedExecutor) {
        SharedResourceHolder.release(SHARED_EXECUTOR, (ExecutorService) executor);
      }
    }
  }
}
