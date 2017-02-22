/*
 * Copyright 2016, Google Inc. All rights reserved.
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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import java.util.LinkedList;
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
final class ChannelExecutor {
  private static final Logger log = Logger.getLogger(ChannelExecutor.class.getName());

  private final Object lock = new Object();

  @GuardedBy("lock")
  private final LinkedList<Runnable> queue = new LinkedList<Runnable>();
  @GuardedBy("lock")
  private boolean draining;

  /**
   * Run all tasks in the queue in the current thread, if no other thread is in this method.
   * Otherwise do nothing.
   *
   * <p>Upon returning, it guarantees that all tasks submitted by {@code executeLater()} before it
   * have been or will eventually be run, while not requiring any more calls to {@code drain()}.
   */
  void drain() {
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
        log.log(Level.WARNING, "Runnable threw exception in ChannelExecutor", t);
      }
    }
  }

  /**
   * Enqueues a task that will be run when {@link #drain} is called.
   *
   * @return this ChannelExecutor
   */
  ChannelExecutor executeLater(Runnable runnable) {
    synchronized (lock) {
      queue.add(checkNotNull(runnable, "runnable is null"));
    }
    return this;
  }

  @VisibleForTesting
  int numPendingTasks() {
    synchronized (lock) {
      return queue.size();
    }
  }
}
