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

package io.grpc;

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import java.lang.Thread.UncaughtExceptionHandler;
import java.util.ArrayDeque;
import java.util.Queue;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A synchronization context is a queue of tasks that run in sequence.  It offers following
 * guarantees:
 *
 * <ul>
 *    <li>Ordering.  Tasks are run in the same order as they are submitted via {@link #execute}
 *        and {@link #executeLater}.</li>
 *    <li>Serialization.  Tasks are run in sequence and establish a happens-before relationship
 *        between them. </li>
 *    <li>Non-reentrancy.  If a task running in a synchronization context executes or schedules
 *        another task in the same synchronization context, the latter task will never run
 *        inline.  It will instead be queued and run only after the current task has returned.</li>
 * </ul>
 *
 * <p>It doesn't own any thread.  Tasks are run from caller's or caller-provided threads.
 *
 * <p>This class is thread-safe.
 *
 * @since 1.17.0
 */
@ThreadSafe
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4984")
public final class SynchronizationContext implements Executor {
  private final Object lock = new Object();
  private final UncaughtExceptionHandler uncaughtExceptionHandler;

  @GuardedBy("lock")
  private final Queue<Runnable> queue = new ArrayDeque<Runnable>();
  @GuardedBy("lock")
  private Thread drainingThread;

  /**
   * Creates a SynchronizationContext.
   *
   * @param uncaughtExceptionHandler handles exceptions thrown out of the tasks.  Different from
   *        what's documented on {@link UncaughtExceptionHandler#uncaughtException}, the thread is
   *        not terminated when the handler is called.
   */
  public SynchronizationContext(UncaughtExceptionHandler uncaughtExceptionHandler) {
    this.uncaughtExceptionHandler =
        checkNotNull(uncaughtExceptionHandler, "uncaughtExceptionHandler");
  }

  /**
   * Run all tasks in the queue in the current thread, if no other thread is running this method.
   * Otherwise do nothing.
   *
   * <p>Upon returning, it guarantees that all tasks submitted by {@code #executeLater} before it
   * have been or will eventually be run, while not requiring any more calls to {@code drain()}.
   */
  public final void drain() {
    boolean drainLeaseAcquired = false;
    while (true) {
      Runnable runnable;
      synchronized (lock) {
        if (!drainLeaseAcquired) {
          if (drainingThread != null) {
            return;
          }
          drainingThread = Thread.currentThread();
          drainLeaseAcquired = true;
        }
        runnable = queue.poll();
        if (runnable == null) {
          drainingThread = null;
          break;
        }
      }
      try {
        runnable.run();
      } catch (Throwable t) {
        uncaughtExceptionHandler.uncaughtException(Thread.currentThread(), t);
      }
    }
  }

  /**
   * Adds a task that will be run when {@link #drain} is called.
   *
   * <p>This is useful for cases where you want to enqueue a task while under a lock of your own,
   * but don't want the tasks to be run under your lock (for fear of deadlock).  You can call {@link
   * #executeLater} in the lock, and call {@link #drain} outside the lock.
   */
  public final void executeLater(Runnable runnable) {
    synchronized (lock) {
      queue.add(checkNotNull(runnable, "runnable is null"));
    }
  }

  /**
   * Adds a task and run it in this synchronization context as soon as poassible.  The task may run
   * inline.  If there are tasks that are previously queued by {@link #executeLater} but have not
   * been run, this method will trigger them to be run before the given task.  This is equivalent to
   * calling {@link #executeLater} immediately followed by {@link #drain}.
   */
  @Override
  public final void execute(Runnable task) {
    executeLater(task);
    drain();
  }

  /**
   * Throw {@link IllegalStateException} if this method is not called from this synchronization
   * context.
   */
  public void throwIfNotInThisSynchronizationContext() {
    synchronized (lock) {
      checkState(
          Thread.currentThread() == drainingThread,
          "Not called from the SynchronizationContext");
    }
  }

  /**
   * Schedules a task to be added and run via {@link #execute} after a delay.
   *
   * @param task the task being scheduled
   * @param delay the delay
   * @param unit the time unit for the delay
   * @param timerService the {@code ScheduledExecutorService} that provides delayed execution
   *
   * @return an object for checking the status and/or cancel the scheduled task
   */
  public final ScheduledHandle schedule(
      final Runnable task, long delay, TimeUnit unit, ScheduledExecutorService timerService) {
    final ManagedRunnable runnable = new ManagedRunnable(task);
    ScheduledFuture<?> future = timerService.schedule(new Runnable() {
        @Override
        public void run() {
          execute(runnable);
        }

        @Override
        public String toString() {
          return task.toString() + "(scheduled in SynchronizationContext)";
        }
      }, delay, unit);
    return new ScheduledHandle(runnable, future);
  }

  private static class ManagedRunnable implements Runnable {
    final Runnable task;
    boolean isCancelled;
    boolean hasStarted;

    ManagedRunnable(Runnable task) {
      this.task = checkNotNull(task, "task");
    }

    @Override
    public void run() {
      // The task may have been cancelled after timerService calls SynchronizationContext.execute()
      // but before the runnable is actually run.  We must guarantee that the task will not be run
      // in this case.
      if (!isCancelled) {
        hasStarted = true;
        task.run();
      }
    }
  }

  /**
   * Allows the user to check the status and/or cancel a task scheduled by {@link #schedule}.
   *
   * <p>This class is NOT thread-safe.  All methods must be run from the same {@link
   * SynchronizationContext} as which the task was scheduled in.
   */
  public static final class ScheduledHandle {
    private final ManagedRunnable runnable;
    private final ScheduledFuture<?> future;

    private ScheduledHandle(ManagedRunnable runnable, ScheduledFuture<?> future) {
      this.runnable = checkNotNull(runnable, "runnable");
      this.future = checkNotNull(future, "future");
    }

    /**
     * Cancel the task if it's still {@link #isPending pending}.
     */
    public void cancel() {
      runnable.isCancelled = true;
      future.cancel(false);
    }

    /**
     * Returns {@code true} if the task will eventually run, meaning that it has neither started
     * running nor been cancelled.
     */
    public boolean isPending() {
      return !(runnable.hasStarted || runnable.isCancelled);
    }
  }
}
