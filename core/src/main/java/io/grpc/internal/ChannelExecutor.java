/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import java.util.ArrayDeque;
import java.util.Queue;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/**
 * The thread-less Channel Executor used to run the state mutation logic in {@link
 * ManagedChannelImpl}, {@link InternalSubchannel} and {@link io.grpc.LoadBalancer}s.
 *
 * <p>Tasks are queued until {@link #drain} is called.  Tasks are guaranteed to be run in the same
 * order as they are submitted.
 */
@ThreadSafe
class ChannelExecutor {
  private static final Logger log = Logger.getLogger(ChannelExecutor.class.getName());

  private final Object lock = new Object();

  @GuardedBy("lock")
  private final Queue<Runnable> queue = new ArrayDeque<Runnable>();
  @GuardedBy("lock")
  private boolean draining;

  /**
   * Run all tasks in the queue in the current thread, if no other thread is in this method.
   * Otherwise do nothing.
   *
   * <p>Upon returning, it guarantees that all tasks submitted by {@code executeLater()} before it
   * have been or will eventually be run, while not requiring any more calls to {@code drain()}.
   */
  final void drain() {
    boolean drainLeaseAcquired = false;
    while (true) {
      Runnable runnable;
      synchronized (lock) {
        if (!drainLeaseAcquired) {
          if (draining) {
            return;
          }
          draining = true;
          drainLeaseAcquired = true;
        }
        runnable = queue.poll();
        if (runnable == null) {
          draining = false;
          break;
        }
      }
      try {
        runnable.run();
      } catch (Throwable t) {
        handleUncaughtThrowable(t);
      }
    }
  }

  /**
   * Enqueues a task that will be run when {@link #drain} is called.
   *
   * @return this ChannelExecutor
   */
  final ChannelExecutor executeLater(Runnable runnable) {
    synchronized (lock) {
      queue.add(checkNotNull(runnable, "runnable is null"));
    }
    return this;
  }

  @VisibleForTesting
  final int numPendingTasks() {
    synchronized (lock) {
      return queue.size();
    }
  }

  /**
   * Handle a throwable from a task.
   *
   * <p>The default implementation logs a warning.
   */
  void handleUncaughtThrowable(Throwable t) {
    log.log(Level.WARNING, "Runnable threw exception in ChannelExecutor", t);
  }
}
