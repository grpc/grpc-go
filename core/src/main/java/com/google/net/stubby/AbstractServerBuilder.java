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

package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.Service;
import com.google.net.stubby.transport.ServerListener;

import java.util.concurrent.ExecutorService;

/**
 * The base class for server builders.
 *
 * @param <BuilderT> The concrete type for this builder.
 */
public abstract class AbstractServerBuilder<BuilderT extends AbstractServerBuilder<BuilderT>>
    extends AbstractServiceBuilder<ServerImpl, BuilderT> {

  private final HandlerRegistry registry;

  /**
   * Constructs using a given handler registry.
   */
  protected AbstractServerBuilder(HandlerRegistry registry) {
    this.registry = Preconditions.checkNotNull(registry);
  }

  /**
   * Constructs with a MutableHandlerRegistry created internally.
   */
  protected AbstractServerBuilder() {
    this.registry = new MutableHandlerRegistryImpl();
  }

  /**
   * Adds a service implementation to the handler registry.
   *
   * <p>This is supported only if the user didn't provide a handler registry, or the provided one is
   * a {@link MutableHandlerRegistry}. Otherwise it throws an UnsupportedOperationException.
   */
  @SuppressWarnings("unchecked")
  public final BuilderT addService(ServerServiceDefinition service) {
    if (registry instanceof MutableHandlerRegistry) {
      ((MutableHandlerRegistry) registry).addService(service);
      return (BuilderT) this;
    }
    throw new UnsupportedOperationException("Underlying HandlerRegistry is not mutable");
  }

  @Override
  protected final ServerImpl buildImpl(ExecutorService executor) {
    ServerImpl server = new ServerImpl(executor, registry);
    server.setTransportServer(buildTransportServer(server.serverListener()));
    return server;
  }

  protected abstract Service buildTransportServer(ServerListener serverListener);
}
