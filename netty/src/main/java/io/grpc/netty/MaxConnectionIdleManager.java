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

package io.grpc.netty;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.internal.LogExceptionRunnable;
import io.netty.channel.ChannelHandlerContext;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import javax.annotation.CheckForNull;

/**
 * Monitors connection idle time; shutdowns the connection if the max connection idle is reached.
 */
abstract class MaxConnectionIdleManager {
  private static final Ticker systemTicker = new Ticker() {
    @Override
    public long nanoTime() {
      return System.nanoTime();
    }
  };

  private final long maxConnectionIdleInNanos;
  private final Ticker ticker;

  @CheckForNull
  private ScheduledFuture<?> shutdownFuture;
  private Runnable shutdownTask;
  private ScheduledExecutorService scheduler;
  private long nextIdleMonitorTime;
  private boolean shutdownDelayed;
  private boolean isActive;

  MaxConnectionIdleManager(long maxConnectionIdleInNanos) {
    this(maxConnectionIdleInNanos, systemTicker);
  }

  @VisibleForTesting
  MaxConnectionIdleManager(long maxConnectionIdleInNanos, Ticker ticker) {
    this.maxConnectionIdleInNanos = maxConnectionIdleInNanos;
    this.ticker = ticker;
  }

  /** A {@link NettyServerHandler} was added to the transport. */
  void start(ChannelHandlerContext ctx) {
    start(ctx, ctx.executor());
  }

  @VisibleForTesting
  void start(final ChannelHandlerContext ctx, final ScheduledExecutorService scheduler) {
    this.scheduler = scheduler;
    nextIdleMonitorTime = ticker.nanoTime() + maxConnectionIdleInNanos;

    shutdownTask = new LogExceptionRunnable(new Runnable() {
      @Override
      public void run() {
        if (shutdownDelayed) {
          if (!isActive) {
            // delay shutdown
            shutdownFuture = scheduler.schedule(
                shutdownTask, nextIdleMonitorTime - ticker.nanoTime(), TimeUnit.NANOSECONDS);
            shutdownDelayed = false;
          }
          // if isActive, exit. Will schedule a new shutdownFuture once onTransportIdle
        } else {
          close(ctx);
          shutdownFuture = null;
        }
      }
    });

    shutdownFuture =
        scheduler.schedule(shutdownTask, maxConnectionIdleInNanos, TimeUnit.NANOSECONDS);
  }

  /**
   * Closes the connection by sending GO_AWAY with status code NO_ERROR and ASCII debug data
   * max_idle and then doing the graceful connection termination.
   */
  abstract void close(ChannelHandlerContext ctx);

  /** There are outstanding RPCs on the transport. */
  void onTransportActive() {
    isActive = true;
    shutdownDelayed = true;
  }

  /** There are no outstanding RPCs on the transport. */
  void onTransportIdle() {
    isActive = false;
    if (shutdownFuture == null) {
      return;
    }
    if (shutdownFuture.isDone()) {
      shutdownDelayed = false;
      shutdownFuture = scheduler
          .schedule(shutdownTask, maxConnectionIdleInNanos, TimeUnit.NANOSECONDS);
    } else {
      nextIdleMonitorTime = ticker.nanoTime() + maxConnectionIdleInNanos;
    }
  }

  /** Transport is being terminated. */
  void onTransportTermination() {
    if (shutdownFuture != null) {
      shutdownFuture.cancel(false);
      shutdownFuture = null;
    }
  }

  @VisibleForTesting
  interface Ticker {
    long nanoTime();
  }
}
