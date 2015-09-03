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

import com.google.common.base.Preconditions;

import io.grpc.HandlerRegistry;
import io.grpc.Internal;
import io.grpc.MutableHandlerRegistry;
import io.grpc.MutableHandlerRegistryImpl;
import io.grpc.ServerBuilder;
import io.grpc.ServerServiceDefinition;

import java.util.concurrent.Executor;

import javax.annotation.Nullable;

/**
 * The base class for server builders.
 *
 * @param <T> The concrete type for this builder.
 */
public abstract class AbstractServerImplBuilder<T extends AbstractServerImplBuilder<T>>
        extends ServerBuilder<T> {

  private final HandlerRegistry registry;
  @Nullable
  private Executor executor;

  /**
   * Constructs using a given handler registry.
   */
  protected AbstractServerImplBuilder(HandlerRegistry registry) {
    this.registry = Preconditions.checkNotNull(registry);
  }

  /**
   * Constructs with a MutableHandlerRegistry created internally.
   */
  protected AbstractServerImplBuilder() {
    this.registry = new MutableHandlerRegistryImpl();
  }

  @Override
  public final T executor(@Nullable Executor executor) {
    this.executor = executor;
    return thisT();
  }

  /**
   * Adds a service implementation to the handler registry.
   *
   * <p>This is supported only if the user didn't provide a handler registry, or the provided one is
   * a {@link MutableHandlerRegistry}. Otherwise it throws an UnsupportedOperationException.
   */
  @Override
  public final T addService(ServerServiceDefinition service) {
    if (registry instanceof MutableHandlerRegistry) {
      ((MutableHandlerRegistry) registry).addService(service);
      return thisT();
    }
    throw new UnsupportedOperationException("Underlying HandlerRegistry is not mutable");
  }

  @Override
  public ServerImpl build() {
    io.grpc.internal.Server transportServer = buildTransportServer();
    return new ServerImpl(executor, registry, transportServer);
  }

  /**
   * Children of AbstractServerBuilder should override this method to provide transport specific
   * information for the server.  This method is mean for Transport implementors and should not be
   * used by normal users.
   */
  @Internal
  protected abstract io.grpc.internal.Server buildTransportServer();

  private T thisT() {
    @SuppressWarnings("unchecked")
    T thisT = (T) this;
    return thisT;
  }
}
