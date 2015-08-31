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

package io.grpc.internal;

import io.grpc.ClientInterceptor;
import io.grpc.Internal;
import io.grpc.ManagedChannelBuilder;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.ExecutorService;

import javax.annotation.Nullable;

/**
 * The base class for channel builders.
 *
 * @param <T> The concrete type of this builder.
 */
public abstract class AbstractManagedChannelImplBuilder
        <T extends AbstractManagedChannelImplBuilder<T>> extends ManagedChannelBuilder<T> {

  @Nullable
  private ExecutorService executor;
  private final List<ClientInterceptor> interceptors = new ArrayList<ClientInterceptor>();

  @Nullable
  private String userAgent;

  /**
   * Provides a custom executor.
   *
   * <p>It's an optional parameter. If the user has not provided an executor when the channel is
   * built, the builder will use a static cached thread pool.
   *
   * <p>The channel won't take ownership of the given executor. It's caller's responsibility to
   * shut down the executor when it's desired.
   */
  public final T executor(ExecutorService executor) {
    this.executor = executor;
    return thisT();
  }

  /**
   * Adds interceptors that will be called before the channel performs its real work. This is
   * functionally equivalent to using {@link io.grpc.ClientInterceptors#intercept(io.grpc.Channel,
   * List)}, but while still having access to the original {@code ChannelImpl}.
   */
  public final T intercept(List<ClientInterceptor> interceptors) {
    this.interceptors.addAll(interceptors);
    return thisT();
  }

  /**
   * Adds interceptors that will be called before the channel performs its real work. This is
   * functionally equivalent to using {@link io.grpc.ClientInterceptors#intercept(io.grpc.Channel,
   * ClientInterceptor...)}, but while still having access to the original {@code ChannelImpl}.
   */
  public final T intercept(ClientInterceptor... interceptors) {
    return intercept(Arrays.asList(interceptors));
  }

  private T thisT() {
    @SuppressWarnings("unchecked")
    T thisT = (T) this;
    return thisT;
  }

  /**
   * Provides a custom {@code User-Agent} for the application.
   *
   * <p>It's an optional parameter. If provided, the given agent will be prepended by the
   * grpc {@code User-Agent}.
   */
  public final T userAgent(String userAgent) {
    this.userAgent = userAgent;
    return thisT();
  }

  /**
   * Builds a channel using the given parameters.
   */
  public ManagedChannelImpl build() {
    ClientTransportFactory transportFactory = buildTransportFactory();
    return new ManagedChannelImpl(transportFactory, executor, userAgent, interceptors);
  }

  /**
   * Children of AbstractChannelBuilder should override this method to provide the
   * {@link ClientTransportFactory} appropriate for this channel.  This method is meant for
   * Transport implementors and should not be used by normal users.
   */
  @Internal
  protected abstract ClientTransportFactory buildTransportFactory();
}
