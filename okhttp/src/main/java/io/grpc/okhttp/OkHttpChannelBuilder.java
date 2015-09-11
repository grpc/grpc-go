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

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ThreadFactoryBuilder;

import com.squareup.okhttp.CipherSuite;
import com.squareup.okhttp.ConnectionSpec;
import com.squareup.okhttp.TlsVersion;

import io.grpc.ExperimentalApi;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.AbstractReferenceCounted;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourceHolder;
import io.grpc.internal.SharedResourceHolder.Resource;

import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import javax.annotation.Nullable;
import javax.net.ssl.SSLSocketFactory;

/** Convenience class for building channels with the OkHttp transport. */
@ExperimentalApi("There is no plan to make this API stable, given transport API instability")
public final class OkHttpChannelBuilder extends
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

  private Executor transportExecutor;
  private final String host;
  private final int port;
  private String authority;
  private SSLSocketFactory sslSocketFactory;
  private ConnectionSpec connectionSpec = DEFAULT_CONNECTION_SPEC;
  private NegotiationType negotiationType = NegotiationType.TLS;
  private int maxMessageSize = DEFAULT_MAX_MESSAGE_SIZE;

  private OkHttpChannelBuilder(String host, int port) {
    this.host = Preconditions.checkNotNull(host);
    this.port = port;
    this.authority = GrpcUtil.authorityFromHostAndPort(host, port);
  }

  /**
   * Override the default executor necessary for internal transport use.
   *
   * <p>The channel does not take ownership of the given executor. It is the caller' responsibility
   * to shutdown the executor when appropriate.
   */
  public OkHttpChannelBuilder transportExecutor(@Nullable Executor transportExecutor) {
    this.transportExecutor = transportExecutor;
    return this;
  }

  /**
   * Overrides the host used with TLS and HTTP virtual hosting. It does not change what host is
   * actually connected to. This method differs from {@link #overrideAuthority(String)} in that it
   * appends the port number to the host provided.
   *
   * <p>Should only used by tests.
   *
   * @deprecated use {@link #overrideAuthority} instead
   */
  @Deprecated
  public OkHttpChannelBuilder overrideHostForAuthority(String host) {
    this.authority = GrpcUtil.authorityFromHostAndPort(host, this.port);
    return this;
  }

  @Override
  public OkHttpChannelBuilder overrideAuthority(String authority) {
    this.authority = GrpcUtil.checkAuthority(authority);
    return this;
  }

  /**
   * Sets the negotiation type for the HTTP/2 connection.
   *
   * <p>Default: <code>TLS</code>
   */
  public OkHttpChannelBuilder negotiationType(NegotiationType type) {
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
  public OkHttpChannelBuilder sslSocketFactory(SSLSocketFactory factory) {
    this.sslSocketFactory = factory;
    negotiationType(NegotiationType.TLS);
    return this;
  }

  /**
   * For secure connection, provides a ConnectionSpec to specify Cipher suite and
   * TLS versions.
   *
   * <p>By default DEFAULT_CONNECTION_SPEC will be used.
   */
  public OkHttpChannelBuilder connectionSpec(ConnectionSpec connectionSpec) {
    this.connectionSpec = connectionSpec;
    return this;
  }

  /**
   * Sets the maximum message size allowed to be received on the channel. If not called,
   * defaults to {@link io.grpc.internal.GrpcUtil#DEFAULT_MAX_MESSAGE_SIZE}.
   */
  public OkHttpChannelBuilder maxMessageSize(int maxMessageSize) {
    checkArgument(maxMessageSize >= 0, "maxMessageSize must be >= 0");
    this.maxMessageSize = maxMessageSize;
    return this;
  }

  /**
   * Equivalent to using {@link #negotiationType(NegotiationType)} with {@code PLAINTEXT}.
   */
  @Override
  public OkHttpChannelBuilder usePlaintext(boolean skipNegotiation) {
    if (skipNegotiation) {
      negotiationType(NegotiationType.PLAINTEXT);
    } else {
      throw new IllegalArgumentException("Plaintext negotiation not currently supported");
    }
    return this;
  }

  @Override
  protected ClientTransportFactory buildTransportFactory() {
    return new OkHttpTransportFactory(host, port, authority, transportExecutor,
            createSocketFactory(), connectionSpec, maxMessageSize);
  }

  private SSLSocketFactory createSocketFactory() {
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

  private static class OkHttpTransportFactory extends AbstractReferenceCounted
          implements ClientTransportFactory {
    private final String host;
    private final int port;
    private final String authority;
    private final Executor executor;
    private final boolean usingSharedExecutor;
    private final SSLSocketFactory socketFactory;
    private final ConnectionSpec connectionSpec;
    private final int maxMessageSize;

    private OkHttpTransportFactory(String host,
                                   int port,
                                   String authority,
                                   Executor executor,
                                   SSLSocketFactory socketFactory,
                                   ConnectionSpec connectionSpec,
                                   int maxMessageSize) {
      this.host = host;
      this.port = port;
      this.authority = authority;
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
    public ClientTransport newClientTransport() {
      return new OkHttpClientTransport(host, port, authority, executor, socketFactory,
              connectionSpec, maxMessageSize);
    }

    @Override
    public String authority() {
      return authority;
    }

    @Override
    protected void deallocate() {
      if (usingSharedExecutor) {
        SharedResourceHolder.release(SHARED_EXECUTOR, (ExecutorService) executor);
      }
    }
  }
}
