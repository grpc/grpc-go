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
import java.util.concurrent.Executor;

import javax.annotation.Nullable;

/**
 * The base class for channel builders.
 *
 * @param <T> The concrete type of this builder.
 */
public abstract class AbstractManagedChannelImplBuilder
        <T extends AbstractManagedChannelImplBuilder<T>> extends ManagedChannelBuilder<T> {

  @Nullable
  private Executor executor;
  private final List<ClientInterceptor> interceptors = new ArrayList<ClientInterceptor>();

  @Nullable
  private String userAgent;

  @Override
  public final T executor(Executor executor) {
    this.executor = executor;
    return thisT();
  }

  @Override
  public final T intercept(List<ClientInterceptor> interceptors) {
    this.interceptors.addAll(interceptors);
    return thisT();
  }

  @Override
  public final T intercept(ClientInterceptor... interceptors) {
    return intercept(Arrays.asList(interceptors));
  }

  private T thisT() {
    @SuppressWarnings("unchecked")
    T thisT = (T) this;
    return thisT;
  }

  @Override
  public final T userAgent(String userAgent) {
    this.userAgent = userAgent;
    return thisT();
  }

  @Override
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
