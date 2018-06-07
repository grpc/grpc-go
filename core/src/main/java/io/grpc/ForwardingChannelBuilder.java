/*
 * Copyright 2017 The gRPC Authors
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

import com.google.common.base.MoreObjects;
import java.util.List;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;

/**
 * A {@link ManagedChannelBuilder} that delegates all its builder method to another builder by
 * default.
 *
 * @param <T> The type of the subclass extending this abstract class.
 *
 * @since 1.7.0
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/3363")
public abstract class ForwardingChannelBuilder<T extends ForwardingChannelBuilder<T>>
    extends ManagedChannelBuilder<T> {

  /**
   * The default constructor.
   */
  protected ForwardingChannelBuilder() {}

  /**
   * This method serves to force sub classes to "hide" this static factory.
   */
  public static ManagedChannelBuilder<?> forAddress(String name, int port) {
    throw new UnsupportedOperationException("Subclass failed to hide static factory");
  }

  /**
   * This method serves to force sub classes to "hide" this static factory.
   */
  public static ManagedChannelBuilder<?> forTarget(String target) {
    throw new UnsupportedOperationException("Subclass failed to hide static factory");
  }

  /**
   * Returns the delegated {@code ManagedChannelBuilder}.
   */
  protected abstract ManagedChannelBuilder<?> delegate();

  @Override
  public T directExecutor() {
    delegate().directExecutor();
    return thisT();
  }

  @Override
  public T executor(Executor executor) {
    delegate().executor(executor);
    return thisT();
  }

  @Override
  public T intercept(List<ClientInterceptor> interceptors) {
    delegate().intercept(interceptors);
    return thisT();
  }

  @Override
  public T intercept(ClientInterceptor... interceptors) {
    delegate().intercept(interceptors);
    return thisT();
  }

  @Override
  public T userAgent(String userAgent) {
    delegate().userAgent(userAgent);
    return thisT();
  }

  @Override
  public T overrideAuthority(String authority) {
    delegate().overrideAuthority(authority);
    return thisT();
  }

  /**
   * @deprecated use {@link #usePlaintext()} instead.
   */
  @Override
  @Deprecated
  public T usePlaintext(boolean skipNegotiation) {
    delegate().usePlaintext(skipNegotiation);
    return thisT();
  }

  @Override
  public T usePlaintext() {
    delegate().usePlaintext();
    return thisT();
  }

  @Override
  public T useTransportSecurity() {
    delegate().useTransportSecurity();
    return thisT();
  }

  @Override
  public T nameResolverFactory(NameResolver.Factory resolverFactory) {
    delegate().nameResolverFactory(resolverFactory);
    return thisT();
  }

  @Override
  public T loadBalancerFactory(LoadBalancer.Factory loadBalancerFactory) {
    delegate().loadBalancerFactory(loadBalancerFactory);
    return thisT();
  }

  @Override
  public T enableFullStreamDecompression() {
    delegate().enableFullStreamDecompression();
    return thisT();
  }

  @Override
  public T decompressorRegistry(DecompressorRegistry registry) {
    delegate().decompressorRegistry(registry);
    return thisT();
  }

  @Override
  public T compressorRegistry(CompressorRegistry registry) {
    delegate().compressorRegistry(registry);
    return thisT();
  }

  @Override
  public T idleTimeout(long value, TimeUnit unit) {
    delegate().idleTimeout(value, unit);
    return thisT();
  }

  @Override
  public T maxInboundMessageSize(int max) {
    delegate().maxInboundMessageSize(max);
    return thisT();
  }

  @Override
  public T keepAliveTime(long keepAliveTime, TimeUnit timeUnit) {
    delegate().keepAliveTime(keepAliveTime, timeUnit);
    return thisT();
  }

  @Override
  public T keepAliveTimeout(long keepAliveTimeout, TimeUnit timeUnit) {
    delegate().keepAliveTimeout(keepAliveTimeout, timeUnit);
    return thisT();
  }

  @Override
  public T keepAliveWithoutCalls(boolean enable) {
    delegate().keepAliveWithoutCalls(enable);
    return thisT();
  }

  @Override
  public T maxRetryAttempts(int maxRetryAttempts) {
    delegate().maxRetryAttempts(maxRetryAttempts);
    return thisT();
  }

  @Override
  public T maxHedgedAttempts(int maxHedgedAttempts) {
    delegate().maxHedgedAttempts(maxHedgedAttempts);
    return thisT();
  }

  @Override
  public T retryBufferSize(long bytes) {
    delegate().retryBufferSize(bytes);
    return thisT();
  }

  @Override
  public T perRpcBufferLimit(long bytes) {
    delegate().perRpcBufferLimit(bytes);
    return thisT();
  }

  @Override
  public T disableRetry() {
    delegate().disableRetry();
    return thisT();
  }

  @Override
  public T enableRetry() {
    delegate().enableRetry();
    return thisT();
  }

  @Override
  public T setBinaryLog(BinaryLog binaryLog) {
    delegate().setBinaryLog(binaryLog);
    return thisT();
  }

  @Override
  public T maxTraceEvents(int maxTraceEvents) {
    delegate().maxTraceEvents(maxTraceEvents);
    return thisT();
  }

  /**
   * Returns the {@link ManagedChannel} built by the delegate by default. Overriding method can
   * return different value.
   */
  @Override
  public ManagedChannel build() {
    return delegate().build();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate()).toString();
  }

  /**
   * Returns the correctly typed version of the builder.
   */
  protected final T thisT() {
    @SuppressWarnings("unchecked")
    T thisT = (T) this;
    return thisT;
  }
}
