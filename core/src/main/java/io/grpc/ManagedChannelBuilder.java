/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc;

import com.google.common.base.Preconditions;
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
   * <p>This method is intended for testing, but may safely be used outside of tests as an
   * alternative to DNS overrides.
   *
   * @return this
   * @since 1.0.0
   */
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
   * @deprecated Use {@link #usePlaintext()} instead.
   *
   * @throws UnsupportedOperationException if plaintext mode is not supported.
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1772")
  @Deprecated
  public T usePlaintext(boolean skipNegotiation) {
    throw new UnsupportedOperationException();
  }

  /**
   * Use of a plaintext connection to the server. By default a secure connection mechanism
   * such as TLS will be used.
   *
   * <p>Should only be used for testing or for APIs where the use of such API or the data
   * exchanged is not sensitive.
   *
   * <p>This assumes prior knowledge that the target of this channel is using plaintext.  It will
   * not perform HTTP/1.1 upgrades.
   *
   *
   * @throws UnsupportedOperationException if plaintext mode is not supported.
   * @return this
   * @since 1.11.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1772")
  @SuppressWarnings("deprecation")
  public T usePlaintext() {
    return usePlaintext(true);
  }

  /**
   * Makes the client use TLS.
   *
   * @return this
   * @throws UnsupportedOperationException if transport security is not supported.
   * @since 1.9.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/3713")
  public T useTransportSecurity() {
    throw new UnsupportedOperationException();
  }

  /**
   * Provides a custom {@link NameResolver.Factory} for the channel. If this method is not called,
   * the builder will try the providers listed by {@link NameResolverProvider#providers()} for the
   * given target.
   *
   * <p>This method should rarely be used, as name resolvers should provide a {@code
   * NameResolverProvider} and users rely on service loading to find implementations in the class
   * path. That allows application's configuration to easily choose the name resolver via the
   * 'target' string passed to {@link ManagedChannelBuilder#forTarget(String)}.
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
   * Enables full-stream decompression of inbound streams. This will cause the channel's outbound
   * headers to advertise support for GZIP compressed streams, and gRPC servers which support the
   * feature may respond with a GZIP compressed stream.
   *
   * <p>EXPERIMENTAL: This method is here to enable an experimental feature, and may be changed or
   * removed once the feature is stable.
   *
   * @throws UnsupportedOperationException if unsupported
   * @since 1.7.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/3399")
  public T enableFullStreamDecompression() {
    throw new UnsupportedOperationException();
  }

  /**
   * Set the decompression registry for use in the channel. This is an advanced API call and
   * shouldn't be used unless you are using custom message encoding. The default supported
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
   * @param bytes the maximum number of bytes a single message can be.
   * @return this
   * @throws IllegalArgumentException if bytes is negative.
   * @since 1.1.0
   */
  public T maxInboundMessageSize(int bytes) {
    // intentional noop rather than throw, this method is only advisory.
    Preconditions.checkArgument(bytes >= 0, "bytes must be >= 0");
    return thisT();
  }

  /**
   * Sets the time without read activity before sending a keepalive ping. An unreasonably small
   * value might be increased, and {@code Long.MAX_VALUE} nano seconds or an unreasonably large
   * value will disable keepalive. Defaults to infinite.
   *
   * <p>Clients must receive permission from the service owner before enabling this option.
   * Keepalives can increase the load on services and are commonly "invisible" making it hard to
   * notice when they are causing excessive load. Clients are strongly encouraged to use only as
   * small of a value as necessary.
   *
   * @throws UnsupportedOperationException if unsupported
   * @since 1.7.0
   */
  public T keepAliveTime(long keepAliveTime, TimeUnit timeUnit) {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets the time waiting for read activity after sending a keepalive ping. If the time expires
   * without any read activity on the connection, the connection is considered dead. An unreasonably
   * small value might be increased. Defaults to 20 seconds.
   *
   * <p>This value should be at least multiple times the RTT to allow for lost packets.
   *
   * @throws UnsupportedOperationException if unsupported
   * @since 1.7.0
   */
  public T keepAliveTimeout(long keepAliveTimeout, TimeUnit timeUnit) {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets whether keepalive will be performed when there are no outstanding RPC on a connection.
   * Defaults to {@code false}.
   *
   * <p>Clients must receive permission from the service owner before enabling this option.
   * Keepalives on unused connections can easilly accidentally consume a considerable amount of
   * bandwidth and CPU. {@link ManagedChannelBuilder#idleTimeout idleTimeout()} should generally be
   * used instead of this option.
   *
   * @throws UnsupportedOperationException if unsupported
   * @see #keepAliveTime(long, TimeUnit)
   * @since 1.7.0
   */
  public T keepAliveWithoutCalls(boolean enable) {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets max number of retry attempts. The total number of retry attempts for each RPC will not
   * exceed this number even if service config may allow a higher number. Setting this number to
   * zero is not effectively the same as {@code disableRetry()} because the former does not disable
   * <a
   * href="https://github.com/grpc/proposal/blob/master/A6-client-retries.md#transparent-retries">
   * transparent retry</a>.
   *
   * <p>This method may not work as expected for the current release because retry is not fully
   * implemented yet.
   *
   * @return this
   * @since 1.11.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/3982")
  public T maxRetryAttempts(int maxRetryAttempts) {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets max number of hedged attempts. The total number of hedged attempts for each RPC will not
   * exceed this number even if service config may allow a higher number.
   *
   * <p>This method may not work as expected for the current release because retry is not fully
   * implemented yet.
   *
   * @return this
   * @since 1.11.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/3982")
  public T maxHedgedAttempts(int maxHedgedAttempts) {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets the retry buffer size in bytes. If the buffer limit is exceeded, no RPC
   * could retry at the moment, and in hedging case all hedges but one of the same RPC will cancel.
   * The implementation may only estimate the buffer size being used rather than count the
   * exact physical memory allocated. The method does not have any effect if retry is disabled by
   * the client.
   *
   * <p>This method may not work as expected for the current release because retry is not fully
   * implemented yet.
   *
   * @return this
   * @since 1.10.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/3982")
  public T retryBufferSize(long bytes) {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets the per RPC buffer limit in bytes used for retry. The RPC is not retriable if its buffer
   * limit is exceeded. The implementation may only estimate the buffer size being used rather than
   * count the exact physical memory allocated. It does not have any effect if retry is disabled by
   * the client.
   *
   * <p>This method may not work as expected for the current release because retry is not fully
   * implemented yet.
   *
   * @return this
   * @since 1.10.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/3982")
  public T perRpcBufferLimit(long bytes) {
    throw new UnsupportedOperationException();
  }


  /**
   * Disables the retry and hedging mechanism provided by the gRPC library. This is designed for the
   * case when users have their own retry implementation and want to avoid their own retry taking
   * place simultaneously with the gRPC library layer retry.
   *
   * @return this
   * @since 1.11.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/3982")
  public T disableRetry() {
    throw new UnsupportedOperationException();
  }

  /**
   * Enables the retry and hedging mechanism provided by the gRPC library.
   *
   * <p>This method may not work as expected for the current release because retry is not fully
   * implemented yet.
   *
   * @return this
   * @since 1.11.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/3982")
  public T enableRetry() {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets the BinaryLog object that this channel should log to. The channel does not take
   * ownership of the object, and users are responsible for calling {@link BinaryLog#close()}.
   *
   * @param binaryLog the object to provide logging.
   * @return this
   * @since 1.13.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4017")
  public T setBinaryLog(BinaryLog binaryLog) {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets the maximum number of channel trace events to keep in the tracer for each channel or
   * subchannel. If set to 0, channel tracing is effectively disabled.
   *
   * @return this
   * @since 1.13.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4471")
  public T maxTraceEvents(int maxTraceEvents) {
    throw new UnsupportedOperationException();
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
