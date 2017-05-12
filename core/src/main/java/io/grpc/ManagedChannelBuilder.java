/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc;

import java.util.List;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;

/**
 * A builder for {@link ManagedChannel} instances.
 *
 * @param <T> The concrete type of this builder.
 */
public abstract class ManagedChannelBuilder<T extends ManagedChannelBuilder<T>> {
  /**
   * Creates a channel with the target's address and port number.
   *
   * @see #forTarget(String)
   * @since 1.0.0
   */
  public static ManagedChannelBuilder<?> forAddress(String name, int port) {
    return ManagedChannelProvider.provider().builderForAddress(name, port);
  }

  /**
   * Creates a channel with a target string, which can be either a valid {@link
   * NameResolver}-compliant URI, or an authority string.
   *
   * <p>A {@code NameResolver}-compliant URI is an absolute hierarchical URI as defined by {@link
   * java.net.URI}. Example URIs:
   * <ul>
   *   <li>{@code "dns:///foo.googleapis.com:8080"}</li>
   *   <li>{@code "dns:///foo.googleapis.com"}</li>
   *   <li>{@code "dns:///%5B2001:db8:85a3:8d3:1319:8a2e:370:7348%5D:443"}</li>
   *   <li>{@code "dns://8.8.8.8/foo.googleapis.com:8080"}</li>
   *   <li>{@code "dns://8.8.8.8/foo.googleapis.com"}</li>
   *   <li>{@code "zookeeper://zk.example.com:9900/example_service"}</li>
   * </ul>
   *
   * <p>An authority string will be converted to a {@code NameResolver}-compliant URI, which has
   * {@code "dns"} as the scheme, no authority, and the original authority string as its path after
   * properly escaped. Example authority strings:
   * <ul>
   *   <li>{@code "localhost"}</li>
   *   <li>{@code "127.0.0.1"}</li>
   *   <li>{@code "localhost:8080"}</li>
   *   <li>{@code "foo.googleapis.com:8080"}</li>
   *   <li>{@code "127.0.0.1:8080"}</li>
   *   <li>{@code "[2001:db8:85a3:8d3:1319:8a2e:370:7348]"}</li>
   *   <li>{@code "[2001:db8:85a3:8d3:1319:8a2e:370:7348]:443"}</li>
   * </ul>
   *
   * @since 1.0.0
   */
  public static ManagedChannelBuilder<?> forTarget(String target) {
    return ManagedChannelProvider.provider().builderForTarget(target);
  }

  /**
   * Execute application code directly in the transport thread.
   *
   * <p>Depending on the underlying transport, using a direct executor may lead to substantial
   * performance improvements. However, it also requires the application to not block under
   * any circumstances.
   *
   * <p>Calling this method is semantically equivalent to calling {@link #executor(Executor)} and
   * passing in a direct executor. However, this is the preferred way as it may allow the transport
   * to perform special optimizations.
   *
   * @return this
   * @since 1.0.0
   */
  public abstract T directExecutor();

  /**
   * Provides a custom executor.
   *
   * <p>It's an optional parameter. If the user has not provided an executor when the channel is
   * built, the builder will use a static cached thread pool.
   *
   * <p>The channel won't take ownership of the given executor. It's caller's responsibility to
   * shut down the executor when it's desired.
   *
   * @return this
   * @since 1.0.0
   */
  public abstract T executor(Executor executor);

  /**
   * Adds interceptors that will be called before the channel performs its real work. This is
   * functionally equivalent to using {@link ClientInterceptors#intercept(Channel, List)}, but while
   * still having access to the original {@code ManagedChannel}.
   *
   * @return this
   * @since 1.0.0
   */
  public abstract T intercept(List<ClientInterceptor> interceptors);

  /**
   * Adds interceptors that will be called before the channel performs its real work. This is
   * functionally equivalent to using {@link ClientInterceptors#intercept(Channel,
   * ClientInterceptor...)}, but while still having access to the original {@code ManagedChannel}.
   *
   * @return this
   * @since 1.0.0
   */
  public abstract T intercept(ClientInterceptor... interceptors);

  /**
   * Provides a custom {@code User-Agent} for the application.
   *
   * <p>It's an optional parameter. The library will provide a user agent independent of this
   * option. If provided, the given agent will prepend the library's user agent information.
   *
   * @return this
   * @since 1.0.0
   */
  public abstract T userAgent(String userAgent);

  /**
   * Overrides the authority used with TLS and HTTP virtual hosting. It does not change what host is
   * actually connected to. Is commonly in the form {@code host:port}.
   *
   * <p>Should only used by tests.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1767")
  public abstract T overrideAuthority(String authority);

  /**
   * Use of a plaintext connection to the server. By default a secure connection mechanism
   * such as TLS will be used.
   *
   * <p>Should only be used for testing or for APIs where the use of such API or the data
   * exchanged is not sensitive.
   *
   * @param skipNegotiation @{code true} if there is a priori knowledge that the endpoint supports
   *                        plaintext, {@code false} if plaintext use must be negotiated.
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1772")
  public abstract T usePlaintext(boolean skipNegotiation);

  /**
   * Provides a custom {@link NameResolver.Factory} for the channel.
   *
   * <p>If this method is not called, the builder will try the providers listed by {@link
   * NameResolverProvider#providers()} for the given target.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
  public abstract T nameResolverFactory(NameResolver.Factory resolverFactory);

  /**
   * Provides a custom {@link LoadBalancer.Factory} for the channel.
   *
   * <p>If this method is not called, the builder will use {@link PickFirstBalancerFactory}
   * for the channel.
   *
   * <p>This method is implemented by all stock channel builders that
   * are shipped with gRPC, but may not be implemented by custom channel builders, in which case
   * this method will throw.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
  public abstract T loadBalancerFactory(LoadBalancer.Factory loadBalancerFactory);

  /**
   * Set the decompression registry for use in the channel.  This is an advanced API call and
   * shouldn't be used unless you are using custom message encoding.   The default supported
   * decompressors are in {@link DecompressorRegistry#getDefaultInstance}.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public abstract T decompressorRegistry(DecompressorRegistry registry);

  /**
   * Set the compression registry for use in the channel.  This is an advanced API call and
   * shouldn't be used unless you are using custom message encoding.   The default supported
   * compressors are in {@link CompressorRegistry#getDefaultInstance}.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public abstract T compressorRegistry(CompressorRegistry registry);

  /**
   * Set the duration without ongoing RPCs before going to idle mode.
   *
   * <p>In idle mode the channel shuts down all connections, the NameResolver and the
   * LoadBalancer. A new RPC would take the channel out of idle mode. A channel starts in idle mode.
   *
   * <p>By default the channel will never go to idle mode after it leaves the initial idle
   * mode.
   *
   * <p>This is an advisory option. Do not rely on any specific behavior related to this option.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2022")
  public abstract T idleTimeout(long value, TimeUnit unit);

  /**
   * Sets the maximum message size allowed to be received on the channel. If not called,
   * defaults to 4 MiB. The default provides protection to clients who haven't considered the
   * possibility of receiving large messages while trying to be large enough to not be hit in normal
   * usage.
   *
   * <p>This method is advisory, and implementations may decide to not enforce this.  Currently,
   * the only known transport to not enforce this is {@code InProcessTransport}.
   *
   * @param max the maximum number of bytes a single message can be.
   * @throws IllegalArgumentException if max is negative.
   * @return this
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2307")
  public T maxInboundMessageSize(int max) {
    // intentional nop
    return thisT();
  }

  /**
   * Builds a channel using the given parameters.
   *
   * @since 1.0.0
   */
  public abstract ManagedChannel build();

  /**
   * Returns the correctly typed version of the builder.
   */
  private T thisT() {
    @SuppressWarnings("unchecked")
    T thisT = (T) this;
    return thisT;
  }
}
