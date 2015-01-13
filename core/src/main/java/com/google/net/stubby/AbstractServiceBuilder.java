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

import com.google.common.base.Throwables;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.Service;
import com.google.common.util.concurrent.ThreadFactoryBuilder;
import com.google.net.stubby.SharedResourceHolder.Resource;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

import javax.annotation.Nullable;

/**
 * Base class for channel builders and server builders.
 *
 * <p>The ownership rule: a builder generally does not take ownership of any objects passed to it.
 * The caller is responsible for closing them if needed. The builder is only responsible for the
 * life-cycle of objects created inside.
 *
 * @param <ProductT> The product that is built by this builder.
 * @param <BuilderT> The concrete type of this builder.
 */
abstract class AbstractServiceBuilder<ProductT extends Service,
    BuilderT extends AbstractServiceBuilder<ProductT, BuilderT>> {

  static final Resource<ExecutorService> DEFAULT_EXECUTOR =
      new Resource<ExecutorService>() {
        private static final String name = "grpc-default-executor";
        @Override
        public ExecutorService create() {
          return Executors.newCachedThreadPool(new ThreadFactoryBuilder()
              .setNameFormat(name + "-%d").build());
        }

        @Override
        public void close(ExecutorService instance) {
          instance.shutdown();
        }

        @Override
        public String toString() {
          return name;
        }
      };

  @Nullable
  private ExecutorService userExecutor;

  /**
   * Provides a custom executor.
   *
   * <p>It's an optional parameter. If the user has not provided an executor when the service is
   * built, the builder will use a static cached thread pool.
   *
   * <p>The service won't take ownership of the given executor. It's caller's responsibility to
   * shut down the executor when it's desired.
   */
  @SuppressWarnings("unchecked")
  public final BuilderT executor(ExecutorService executor) {
    userExecutor = executor;
    return (BuilderT) this;
  }

  /**
   * Builds a service using the given parameters.
   *
   * <p>The returned service has not been started at this point. You will need to start it by
   * yourself or use {@link #buildAndStart()}.
   */
  public ProductT build() {
    final ExecutorService executor = (userExecutor == null)
        ?  SharedResourceHolder.get(DEFAULT_EXECUTOR) : userExecutor;
    ProductT service = buildImpl(executor);
    // We shut down the executor only if we created it.
    if (userExecutor == null) {
      service.addListener(new ClosureHook() {
        @Override
        protected void onClosed() {
          SharedResourceHolder.release(DEFAULT_EXECUTOR, executor);
        }
      }, MoreExecutors.directExecutor());
    }
    return service;
  }

  /**
   * Builds and starts a service.
   *
   * <p>The service may not be running when this method returns. If you want to wait until it's up
   * and running, either use {@link Service#awaitRunning()} or {@link #buildAndWaitForRunning()}.
   *
   * @return the service that has just been built and started
   */
  public final ProductT buildAndStart() {
    ProductT service = build();
    service.startAsync();
    return service;
  }

  /**
   * Builds and starts a service, and wait until it's up and running.
   *
   * @return the service that has just been built and is now running.
   */
  public final ProductT buildAndWaitForRunning() {
    ProductT service = buildAndStart();
    try {
      service.awaitRunning();
    } catch (Exception e) {
      service.stopAsync();
      throw Throwables.propagate(e);
    }
    return service;
  }

  /**
   * Builds and starts a service, and wait until it's up and running, with a timeout.
   *
   * @return the service that has just been built and is now running.
   * @throws TimeoutException if the service didn't become running within the given timeout.
   */
  public final ProductT buildAndWaitForRunning(long timeout, TimeUnit unit)
      throws TimeoutException {
    ProductT service = buildAndStart();
    try {
      service.awaitRunning(timeout, unit);
    } catch (Exception e) {
      service.stopAsync();
      if (e instanceof TimeoutException) {
        throw (TimeoutException) e;
      } else {
        throw Throwables.propagate(e);
      }
    }
    return service;
  }

  /**
   * Subclasses may use this as a convenient listener for cleaning up after the built service.
   */
  protected abstract static class ClosureHook extends Service.Listener {
    protected abstract void onClosed();

    @Override
    public void terminated(Service.State from) {
      onClosed();
    }

    @Override
    public void failed(Service.State from, Throwable failure) {
      onClosed();
    }
  }

  /**
   * Implemented by subclasses to build the actual service object. The given executor is owned by
   * this base class.
   */
  protected abstract ProductT buildImpl(ExecutorService executor);
}
