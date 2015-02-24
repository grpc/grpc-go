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

import java.util.ArrayDeque;
import java.util.Queue;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.concurrent.GuardedBy;

/**
 * Executor ensuring that all {@link Runnable} tasks submitted are executed in order
 * using the provided {@link Executor}, and serially such that no two will ever be
 * running at the same time.
 */
// TODO(madongfly): figure out a way to not expose it or move it to transport package.
public final class SerializingExecutor implements Executor {
  private static final Logger log =
      Logger.getLogger(SerializingExecutor.class.getName());

  /** Underlying executor that all submitted Runnable objects are run on. */
  private final Executor executor;

  /** A list of Runnables to be run in order. */
  @GuardedBy("internalLock")
  private final Queue<Runnable> waitQueue = new ArrayDeque<Runnable>();

  /**
   * We explicitly keep track of if the TaskRunner is currently scheduled to
   * run.  If it isn't, we start it.  We can't just use
   * waitQueue.isEmpty() as a proxy because we need to ensure that only one
   * Runnable submitted is running at a time so even if waitQueue is empty
   * the isThreadScheduled isn't set to false until after the Runnable is
   * finished.
   */
  @GuardedBy("internalLock")
  private boolean isThreadScheduled = false;

  /** The object that actually runs the Runnables submitted, reused. */
  private final TaskRunner taskRunner = new TaskRunner();

  /**
   * Creates a SerializingExecutor, running tasks using {@code executor}.
   *
   * @param executor Executor in which tasks should be run. Must not be null.
   */
  public SerializingExecutor(Executor executor) {
    Preconditions.checkNotNull(executor, "'executor' must not be null.");
    this.executor = executor;
  }

  private final Object internalLock = new Object() {
    @Override public String toString() {
      return "SerializingExecutor lock: " + super.toString();
    }
  };

  /**
   * Runs the given runnable strictly after all Runnables that were submitted
   * before it, and using the {@code executor} passed to the constructor.     .
   */
  @Override
  public void execute(Runnable r) {
    Preconditions.checkNotNull(r, "'r' must not be null.");
    boolean scheduleTaskRunner = false;
    synchronized (internalLock) {
      waitQueue.add(r);

      if (!isThreadScheduled) {
        isThreadScheduled = true;
        scheduleTaskRunner = true;
      }
    }
    if (scheduleTaskRunner) {
      boolean threw = true;
      try {
        executor.execute(taskRunner);
        threw = false;
      } finally {
        if (threw) {
          synchronized (internalLock) {
            // It is possible that at this point that there are still tasks in
            // the queue, it would be nice to keep trying but the error may not
            // be recoverable.  So we update our state and propogate so that if
            // our caller deems it recoverable we won't be stuck.
            isThreadScheduled = false;
          }
        }
      }
    }
  }

  /**
   * Task that actually runs the Runnables.  It takes the Runnables off of the
   * queue one by one and runs them.  After it is done with all Runnables and
   * there are no more to run, puts the SerializingExecutor in the state where
   * isThreadScheduled = false and returns.  This allows the current worker
   * thread to return to the original pool.
   */
  private class TaskRunner implements Runnable {
    @Override
    public void run() {
      boolean stillRunning = true;
      try {
        while (true) {
          Preconditions.checkState(isThreadScheduled);
          Runnable nextToRun;
          synchronized (internalLock) {
            nextToRun = waitQueue.poll();
            if (nextToRun == null) {
              isThreadScheduled = false;
              stillRunning = false;
              break;
            }
          }

          // Always run while not holding the lock, to avoid deadlocks.
          try {
            nextToRun.run();
          } catch (RuntimeException e) {
            // Log it and keep going.
            log.log(Level.SEVERE, "Exception while executing runnable "
                + nextToRun, e);
          }
        }
      } finally {
        if (stillRunning) {
          // An Error is bubbling up, we should mark ourselves as no longer
          // running, that way if anyone tries to keep using us we won't be
          // corrupted.
          synchronized (internalLock) {
            isThreadScheduled = false;
          }
        }
      }
    }
  }
}
