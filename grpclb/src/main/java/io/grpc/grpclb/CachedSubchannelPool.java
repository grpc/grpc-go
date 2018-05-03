/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import java.util.HashMap;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

/**
 * A {@link SubchannelPool} that keeps returned {@link Subchannel}s for a given time before it's
 * shut down by the pool.
 */
final class CachedSubchannelPool implements SubchannelPool {
  private final HashMap<EquivalentAddressGroup, CacheEntry> cache =
      new HashMap<EquivalentAddressGroup, CacheEntry>();

  private Helper helper;
  private ScheduledExecutorService timerService;

  @VisibleForTesting
  static final long SHUTDOWN_TIMEOUT_MS = 10000;

  @Override
  public void init(Helper helper, ScheduledExecutorService timerService) {
    this.helper = checkNotNull(helper, "helper");
    this.timerService = checkNotNull(timerService, "timerService");
  }

  @Override
  public Subchannel takeOrCreateSubchannel(
      EquivalentAddressGroup eag, Attributes defaultAttributes) {
    CacheEntry entry = cache.remove(eag);
    Subchannel subchannel;
    if (entry == null) {
      subchannel = helper.createSubchannel(eag, defaultAttributes);
    } else {
      subchannel = entry.subchannel;
      entry.shutdownTimer.cancel(false);
    }
    return subchannel;
  }

  @Override
  public void returnSubchannel(Subchannel subchannel) {
    CacheEntry prev = cache.get(subchannel.getAddresses());
    if (prev != null) {
      // Returning the same Subchannel twice has no effect.
      // Returning a different Subchannel for an already cached EAG will cause the
      // latter Subchannel to be shutdown immediately.
      if (prev.subchannel != subchannel) {
        subchannel.shutdown();
      }
      return;
    }
    final ShutdownSubchannelTask shutdownTask = new ShutdownSubchannelTask(subchannel);
    ScheduledFuture<?> shutdownTimer =
        timerService.schedule(
            new ShutdownSubchannelScheduledTask(shutdownTask),
            SHUTDOWN_TIMEOUT_MS, TimeUnit.MILLISECONDS);
    shutdownTask.timer = shutdownTimer;
    CacheEntry entry = new CacheEntry(subchannel, shutdownTimer);
    cache.put(subchannel.getAddresses(), entry);
  }

  @Override
  public void clear() {
    for (CacheEntry entry : cache.values()) {
      entry.shutdownTimer.cancel(false);
      entry.subchannel.shutdown();
    }
    cache.clear();
  }

  @VisibleForTesting
  final class ShutdownSubchannelScheduledTask implements Runnable {
    private final ShutdownSubchannelTask task;

    ShutdownSubchannelScheduledTask(ShutdownSubchannelTask task) {
      this.task = checkNotNull(task, "task");
    }

    @Override
    public void run() {
      helper.runSerialized(task);
    }
  }

  @VisibleForTesting
  final class ShutdownSubchannelTask implements Runnable {
    private final Subchannel subchannel;
    private ScheduledFuture<?> timer;

    private ShutdownSubchannelTask(Subchannel subchannel) {
      this.subchannel = checkNotNull(subchannel, "subchannel");
    }

    // This runs in channelExecutor
    @Override
    public void run() {
      // getSubchannel() may have cancelled the timer after the timer has expired but before this
      // task is actually run in the channelExecutor.
      if (!timer.isCancelled()) {
        CacheEntry entry = cache.remove(subchannel.getAddresses());
        checkState(entry.subchannel == subchannel, "Inconsistent state");
        subchannel.shutdown();
      }
    }
  }

  private static class CacheEntry {
    final Subchannel subchannel;
    final ScheduledFuture<?> shutdownTimer;

    CacheEntry(Subchannel subchannel, ScheduledFuture<?> shutdownTimer) {
      this.subchannel = checkNotNull(subchannel, "subchannel");
      this.shutdownTimer = checkNotNull(shutdownTimer, "shutdownTimer");
    }
  }
}
