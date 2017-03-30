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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.Preconditions;
import java.util.Queue;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.Executor;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Executor ensuring that all {@link Runnable} tasks submitted are executed in order
 * using the provided {@link Executor}, and serially such that no two will ever be
 * running at the same time.
 */
// TODO(madongfly): figure out a way to not expose it or move it to transport package.
public final class SerializingExecutor implements Executor, Runnable {
  private static final Logger log =
      Logger.getLogger(SerializingExecutor.class.getName());

  /** Underlying executor that all submitted Runnable objects are run on. */
  private final Executor executor;

  /** A list of Runnables to be run in order. */
  private final Queue<Runnable> runQueue = new ConcurrentLinkedQueue<Runnable>();

  private final AtomicBoolean running = new AtomicBoolean();

  /**
   * Creates a SerializingExecutor, running tasks using {@code executor}.
   *
   * @param executor Executor in which tasks should be run. Must not be null.
   */
  public SerializingExecutor(Executor executor) {
    Preconditions.checkNotNull(executor, "'executor' must not be null.");
    this.executor = executor;
  }

  /**
   * Runs the given runnable strictly after all Runnables that were submitted
   * before it, and using the {@code executor} passed to the constructor.     .
   */
  @Override
  public void execute(Runnable r) {
    runQueue.add(checkNotNull(r, "'r' must not be null."));
    schedule(r);
  }

  private void schedule(@Nullable Runnable removable) {
    if (running.compareAndSet(false, true)) {
      boolean success = false;
      try {
        executor.execute(this);
        success = true;
      } finally {
        // It is possible that at this point that there are still tasks in
        // the queue, it would be nice to keep trying but the error may not
        // be recoverable.  So we update our state and propagate so that if
        // our caller deems it recoverable we won't be stuck.
        if (!success) {
          if (removable != null) {
            // This case can only be reached if 'this' was not currently running, and we failed to
            // reschedule.  The item should still be in the queue for removal.
            // ConcurrentLinkedQueue claims that null elements are not allowed, but seems to not
            // throw if the item to remove is null.  If removable is present in the queue twice,
            // the wrong one may be removed.  It doesn't seem possible for this case to exist today.
            // This is important to run in case of RejectedExectuionException, so that future calls
            // to execute don't succeed and accidentally run a previous runnable.
            runQueue.remove(removable);
          }
          running.set(false);
        }
      }
    }
  }

  @Override
  public void run() {
    Runnable r;
    try {
      while ((r = runQueue.poll()) != null) {
        try {
          r.run();
        } catch (RuntimeException e) {
          // Log it and keep going.
          log.log(Level.SEVERE, "Exception while executing runnable " + r, e);
        }
      }
    } finally {
      running.set(false);
    }
    if (!runQueue.isEmpty()) {
      // we didn't enqueue anything but someone else did.
      schedule(null);
    }
  }
}
