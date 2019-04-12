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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer.CreateSubchannelArgs;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelStateListener;
import io.grpc.SynchronizationContext.ScheduledHandle;
import java.util.HashMap;
import java.util.concurrent.TimeUnit;

/**
 * A {@link SubchannelPool} that keeps returned {@link Subchannel}s for a given time before it's
 * shut down by the pool.
 */
final class CachedSubchannelPool implements SubchannelPool {
  private final HashMap<EquivalentAddressGroup, CacheEntry> cache =
      new HashMap<>();

  private Helper helper;

  @VisibleForTesting
  static final long SHUTDOWN_TIMEOUT_MS = 10000;

  @Override
  public void init(Helper helper) {
    this.helper = checkNotNull(helper, "helper");
  }

  @Override
  public Subchannel takeOrCreateSubchannel(
      EquivalentAddressGroup eag, Attributes defaultAttributes, SubchannelStateListener listener) {
    final CacheEntry entry = cache.get(eag);
    final Subchannel subchannel;
    if (entry == null) {
      final CacheEntry newEntry = new CacheEntry();
      subchannel = helper.createSubchannel(CreateSubchannelArgs.newBuilder()
          .setAddresses(eag)
          .setAttributes(defaultAttributes)
          .setStateListener(new StateListener(newEntry))
          .build());
      newEntry.init(subchannel);
      cache.put(eag, newEntry);
      newEntry.taken(listener);
    } else {
      subchannel = entry.subchannel;
      checkState(eag.equals(subchannel.getAddresses()),
          "Unexpected address change from %s to %s", eag, subchannel.getAddresses());
      entry.taken(listener);
      // Make the listener up-to-date with the latest state in case it has changed while it's in the
      // cache.
      helper.getSynchronizationContext().execute(new Runnable() {
          @Override
          public void run() {
            entry.maybeNotifyStateListener();
          }
        });
    }
    return subchannel;
  }

  @Override
  public void returnSubchannel(Subchannel subchannel) {
    CacheEntry entry = cache.get(subchannel.getAddresses());
    checkArgument(entry != null, "Cache record for %s not found", subchannel);
    checkArgument(entry.subchannel == subchannel,
        "Subchannel being returned (%s) doesn't match the cache (%s)",
        subchannel, entry.subchannel);
    entry.returned();
  }

  @Override
  public void clear() {
    for (CacheEntry entry : cache.values()) {
      entry.cancelShutdownTimer();
      entry.subchannel.shutdown();
    }
    cache.clear();
  }

  @VisibleForTesting
  final class ShutdownSubchannelTask implements Runnable {
    private final Subchannel subchannel;

    private ShutdownSubchannelTask(Subchannel subchannel) {
      this.subchannel = checkNotNull(subchannel, "subchannel");
    }

    // This runs in channelExecutor
    @Override
    public void run() {
      CacheEntry entry = cache.remove(subchannel.getAddresses());
      checkState(entry.subchannel == subchannel, "Inconsistent state");
      entry.cancelShutdownTimer();
      subchannel.shutdown();
    }
  }

  private class CacheEntry {
    Subchannel subchannel;
    ScheduledHandle shutdownTimer;
    ConnectivityStateInfo state;
    // Not null if outside of pool
    SubchannelStateListener stateListener;

    void init(Subchannel subchannel) {
      this.subchannel = checkNotNull(subchannel, "subchannel");
    }

    void cancelShutdownTimer() {
      if (shutdownTimer != null) {
        shutdownTimer.cancel();
        shutdownTimer = null;
      }
    }

    void taken(SubchannelStateListener listener) {
      checkState(stateListener == null, "Already out of pool");
      stateListener = checkNotNull(listener, "listener");
      cancelShutdownTimer();
    }

    void returned() {
      checkState(stateListener != null, "Already in pool");
      if (shutdownTimer == null) {
        shutdownTimer = helper.getSynchronizationContext().schedule(
            new ShutdownSubchannelTask(subchannel), SHUTDOWN_TIMEOUT_MS, TimeUnit.MILLISECONDS,
            helper.getScheduledExecutorService());
      } else {
        checkState(shutdownTimer.isPending());
      }
      stateListener = null;
    }

    void maybeNotifyStateListener() {
      if (stateListener != null && state != null) {
        stateListener.onSubchannelState(subchannel, state);
      }
    }
  }

  private static final class StateListener implements SubchannelStateListener {
    private final CacheEntry entry;

    StateListener(CacheEntry entry) {
      this.entry = checkNotNull(entry, "entry");
    }

    @Override
    public void onSubchannelState(Subchannel subchannel, ConnectivityStateInfo newState) {
      entry.state = newState;
      entry.maybeNotifyStateListener();
    }
  }
}
