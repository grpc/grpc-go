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

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ThreadFactoryBuilder;

import io.grpc.SharedResourceHolder.Resource;
import io.grpc.transport.ClientTransportFactory;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import javax.annotation.Nullable;

/**
 * The base class for channel builders.
 *
 * @param <BuilderT> The concrete type of this builder.
 */
public abstract class AbstractChannelBuilder<BuilderT extends AbstractChannelBuilder<BuilderT>> {
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
   * <p>It's an optional parameter. If the user has not provided an executor when the channel is
   * built, the builder will use a static cached thread pool.
   *
   * <p>The channel won't take ownership of the given executor. It's caller's responsibility to
   * shut down the executor when it's desired.
   */
  @SuppressWarnings("unchecked")
  public final BuilderT executor(ExecutorService executor) {
    userExecutor = executor;
    return (BuilderT) this;
  }

  /**
   * Builds a channel using the given parameters.
   */
  public ChannelImpl build() {
    final ExecutorService executor;
    final boolean releaseExecutor;
    if (userExecutor != null) {
      executor = userExecutor;
      releaseExecutor = false;
    } else {
      executor = SharedResourceHolder.get(DEFAULT_EXECUTOR);
      releaseExecutor = true;
    }

    final ChannelEssentials essentials = buildEssentials();
    ChannelImpl channel = new ChannelImpl(essentials.transportFactory, executor);
    channel.setTerminationRunnable(new Runnable() {
      @Override
      public void run() {
        if (releaseExecutor) {
          SharedResourceHolder.release(DEFAULT_EXECUTOR, executor);
        }
        if (essentials.terminationRunnable != null) {
          essentials.terminationRunnable.run();
        }
      }
    });
    return channel;
  }

  /**
   * The essentials required for creating a channel.
   */
  protected static class ChannelEssentials {
    final ClientTransportFactory transportFactory;
    @Nullable final Runnable terminationRunnable;

    /**
     * @param transportFactory the created channel uses this factory to create transports
     * @param terminationRunnable will be called at the channel's life-cycle events
     */
    public ChannelEssentials(ClientTransportFactory transportFactory,
        @Nullable Runnable terminationRunnable) {
      this.transportFactory = Preconditions.checkNotNull(transportFactory);
      this.terminationRunnable = terminationRunnable;
    }
  }

  protected abstract ChannelEssentials buildEssentials();
}
