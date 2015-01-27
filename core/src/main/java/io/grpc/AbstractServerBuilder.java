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

package io.grpc;

import static io.grpc.AbstractChannelBuilder.DEFAULT_EXECUTOR;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.Service;

import io.grpc.transport.ServerListener;

import java.util.concurrent.ExecutorService;

import javax.annotation.Nullable;

/**
 * The base class for server builders.
 *
 * @param <BuilderT> The concrete type for this builder.
 */
public abstract class AbstractServerBuilder<BuilderT extends AbstractServerBuilder<BuilderT>> {

  private final HandlerRegistry registry;
  @Nullable
  private ExecutorService userExecutor;

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
   * Provides a custom executor.
   *
   * <p>It's an optional parameter. If the user has not provided an executor when the server is
   * built, the builder will use a static cached thread pool.
   *
   * <p>The server won't take ownership of the given executor. It's caller's responsibility to
   * shut down the executor when it's desired.
   */
  @SuppressWarnings("unchecked")
  public final BuilderT executor(ExecutorService executor) {
    userExecutor = executor;
    return (BuilderT) this;
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

  /**
   * Builds a server using the given parameters.
   *
   * <p>The returned service will not been started or be bound a port. You will need to start it
   * with {@link ServerImpl#start()}.
   */
  public ServerImpl build() {
    final ExecutorService executor;
    final boolean releaseExecutor;
    if (userExecutor != null) {
      executor = userExecutor;
      releaseExecutor = false;
    } else {
      executor = SharedResourceHolder.get(DEFAULT_EXECUTOR);
      releaseExecutor = true;
    }

    ServerImpl server = new ServerImpl(executor, registry);
    server.setTransportServer(buildTransportServer(server.serverListener()));
    server.setTerminationRunnable(new Runnable() {
      @Override
      public void run() {
        if (releaseExecutor) {
          SharedResourceHolder.release(DEFAULT_EXECUTOR, executor);
        }
      }
    });
    return server;
  }

  protected abstract Service buildTransportServer(ServerListener serverListener);
}
