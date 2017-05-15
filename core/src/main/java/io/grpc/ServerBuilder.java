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

import java.io.File;
import java.util.concurrent.Executor;
import javax.annotation.Nullable;

/**
 * A builder for {@link Server} instances.
 *
 * @param <T> The concrete type of this builder.
 * @since 1.0.0
 */
public abstract class ServerBuilder<T extends ServerBuilder<T>> {

  /**
   * Static factory for creating a new ServerBuilder.
   *
   * @param port the port to listen on
   * @since 1.0.0
   */
  public static ServerBuilder<?> forPort(int port) {
    return ServerProvider.provider().builderForPort(port);
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
   * <p>It's an optional parameter. If the user has not provided an executor when the server is
   * built, the builder will use a static cached thread pool.
   *
   * <p>The server won't take ownership of the given executor. It's caller's responsibility to
   * shut down the executor when it's desired.
   *
   * @return this
   * @since 1.0.0
   */
  public abstract T executor(@Nullable Executor executor);

  /**
   * Adds a service implementation to the handler registry.
   *
   * @param service ServerServiceDefinition object
   * @return this
   * @since 1.0.0
   */
  public abstract T addService(ServerServiceDefinition service);

  /**
   * Adds a service implementation to the handler registry. If bindableService implements
   * {@link InternalNotifyOnServerBuild}, the service will receive a reference to the generated
   * server instance upon build().
   *
   * @param bindableService BindableService object
   * @return this
   * @since 1.0.0
   */
  public abstract T addService(BindableService bindableService);

  /**
   * Adds a {@link ServerTransportFilter}. The order of filters being added is the order they will
   * be executed.
   *
   * @return this
   * @since 1.2.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2132")
  public T addTransportFilter(ServerTransportFilter filter) {
    throw new UnsupportedOperationException();
  }

  /**
   * Adds a {@link ServerStreamTracer.Factory} to measure server-side traffic.  The order of
   * factories being added is the order they will be executed.
   *
   * @return this
   * @since 1.3.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2861")
  public T addStreamTracerFactory(ServerStreamTracer.Factory factory) {
    throw new UnsupportedOperationException();
  }

  /**
   * Sets a fallback handler registry that will be looked up in if a method is not found in the
   * primary registry. The primary registry (configured via {@code addService()}) is faster but
   * immutable. The fallback registry is more flexible and allows implementations to mutate over
   * time and load services on-demand.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/933")
  public abstract T fallbackHandlerRegistry(@Nullable HandlerRegistry fallbackRegistry);

  /**
   * Makes the server use TLS.
   *
   * @param certChain file containing the full certificate chain
   * @param privateKey file containing the private key
   *
   * @return this
   * @since 1.0.0
   */
  public abstract T useTransportSecurity(File certChain, File privateKey);

  /**
   * Set the decompression registry for use in the channel.  This is an advanced API call and
   * shouldn't be used unless you are using custom message encoding.   The default supported
   * decompressors are in {@code DecompressorRegistry.getDefaultInstance}.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public abstract T decompressorRegistry(@Nullable DecompressorRegistry registry);

  /**
   * Set the compression registry for use in the channel.  This is an advanced API call and
   * shouldn't be used unless you are using custom message encoding.   The default supported
   * compressors are in {@code CompressorRegistry.getDefaultInstance}.
   *
   * @return this
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public abstract T compressorRegistry(@Nullable CompressorRegistry registry);

  /**
   * Builds a server using the given parameters.
   *
   * <p>The returned service will not been started or be bound a port. You will need to start it
   * with {@link Server#start()}.
   *
   * @return a new Server
   * @since 1.0.0
   */
  public abstract Server build();
}
