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

/**
 * A builder for {@link ManagedChannel} instances.
 *
 * @param <T> The concrete type of this builder.
 */
public abstract class ManagedChannelBuilder<T extends ManagedChannelBuilder<T>> {
  public static ManagedChannelBuilder<?> forAddress(String name, int port) {
    return ManagedChannelProvider.provider().builderForAddress(name, port);
  }

  /**
   * Creates a channel with a target string, which can be either a valid {@link
   * NameResolver}-compliant URI, or a HOST:PORT string.
   *
   * <p>Example {@code NameResolver}-compliant URIs:
   * <ul>
   *   <li>{@code "dns:///foo.googleapis.com:8080"}</li>
   *   <li>{@code "dns:///foo.googleapis.com"}</li>
   *   <li>{@code "dns://8.8.8.8/foo.googleapis.com:8080"}</li>
   *   <li>{@code "dns://8.8.8.8/foo.googleapis.com"}</li>
   *   <li>{@code "zookeeper://zk.example.com:9900/example_service"}</li>
   * </ul>
   *
   * <p>Example HOST:PORT strings, which will be converted to {@code NameResolver}-compliant URIs by
   * prepending {@code "dns:///"}.
   * <ul>
   *   <li>{@code "localhost"}</li>
   *   <li>{@code "127.0.0.1"}</li>
   *   <li>{@code "localhost:8080"}</li>
   *   <li>{@code "foo.googleapis.com:8080"}</li>
   *   <li>{@code "127.0.0.1:8080"}</li>
   *   <li>{@code "[2001:db8:85a3:8d3:1319:8a2e:370:7348]"}</li>
   *   <li>{@code "[2001:db8:85a3:8d3:1319:8a2e:370:7348]:443"}</li>
   * </ul>
   */
  @ExperimentalApi
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
   */
  public abstract T executor(Executor executor);

  /**
   * Adds interceptors that will be called before the channel performs its real work. This is
   * functionally equivalent to using {@link ClientInterceptors#intercept(Channel, List)}, but while
   * still having access to the original {@code ManagedChannel}.
   */
  public abstract T intercept(List<ClientInterceptor> interceptors);

  /**
   * Adds interceptors that will be called before the channel performs its real work. This is
   * functionally equivalent to using {@link ClientInterceptors#intercept(Channel,
   * ClientInterceptor...)}, but while still having access to the original {@code ManagedChannel}.
   */
  public abstract T intercept(ClientInterceptor... interceptors);

  /**
   * Provides a custom {@code User-Agent} for the application.
   *
   * <p>It's an optional parameter. If provided, the given agent will be prepended by the
   * grpc {@code User-Agent}.
   */
  public abstract T userAgent(String userAgent);

  /**
   * Overrides the authority used with TLS and HTTP virtual hosting. It does not change what host is
   * actually connected to. Is commonly in the form {@code host:port}.
   *
   * <p>Should only used by tests.
   */
  @ExperimentalApi("primarily for testing")
  public abstract T overrideAuthority(String authority);

  /*
   * Use of a plaintext connection to the server. By default a secure connection mechanism
   * such as TLS will be used.
   *
   * <p>Should only be used for testing or for APIs where the use of such API or the data
   * exchanged is not sensitive.
   *
   * @param skipNegotiation @{code true} if there is a priori knowledge that the endpoint supports
   *                        plaintext, {@code false} if plaintext use must be negotiated.
   */
  @ExperimentalApi("primarily for testing")
  public abstract T usePlaintext(boolean skipNegotiation);

  /*
   * Provides a custom {@link NameResolver.Factory} for the channel.
   *
   * <p>If this method is not called, the builder will look up in the global resolver registry for
   * a factory for the provided target.
   */
  @ExperimentalApi
  public abstract T nameResolverFactory(NameResolver.Factory resolverFactory);

  /**
   * Provides a custom {@link LoadBalancer.Factory} for the channel.
   *
   * <p>If this method is not called, the builder will use {@link SimpleLoadBalancerFactory} for the
   * channel.
   */
  @ExperimentalApi
  public abstract T loadBalancerFactory(LoadBalancer.Factory loadBalancerFactory);

  /**
   * Builds a channel using the given parameters.
   */
  public abstract ManagedChannel build();
}
