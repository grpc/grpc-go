/*
 * Copyright 2017, Google Inc. All rights reserved.
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
