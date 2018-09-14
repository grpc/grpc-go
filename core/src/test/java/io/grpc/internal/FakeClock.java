/*
 * Copyright 2015 The gRPC Authors
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

import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import com.google.common.base.Ticker;
import com.google.common.util.concurrent.AbstractFuture;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.concurrent.Delayed;
import java.util.concurrent.Future;
import java.util.concurrent.PriorityBlockingQueue;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

/**
 * A manipulated clock that exports a {@link Ticker} and a {@link ScheduledExecutorService}.
 *
 * <p>To simulate the locking scenario of using real executors, it never runs tasks within {@code
 * schedule()} or {@code execute()}. Instead, you should call {@link #runDueTasks} in your test
 * method to run all due tasks. {@link #forwardTime} and {@link #forwardNanos} call {@link
 * #runDueTasks} automatically.
 */
public final class FakeClock {

  private static final TaskFilter ACCEPT_ALL_FILTER = new TaskFilter() {
      @Override
      public boolean shouldAccept(Runnable command) {
        return true;
      }
    };

  private final ScheduledExecutorService scheduledExecutorService = new ScheduledExecutorImpl();

  private final PriorityBlockingQueue<ScheduledTask> tasks =
      new PriorityBlockingQueue<ScheduledTask>();

  private final Ticker ticker =
      new Ticker() {
        @Override public long read() {
          return currentTimeNanos;
        }
      };

  private final Supplier<Stopwatch> stopwatchSupplier =
      new Supplier<Stopwatch>() {
        @Override public Stopwatch get() {
          return Stopwatch.createUnstarted(ticker);
        }
      };

  private long currentTimeNanos;

  public class ScheduledTask extends AbstractFuture<Void> implements ScheduledFuture<Void> {
    public final Runnable command;
    public final long dueTimeNanos;

    ScheduledTask(long dueTimeNanos, Runnable command) {
      this.dueTimeNanos = dueTimeNanos;
      this.command = command;
    }

    @Override public boolean cancel(boolean mayInterruptIfRunning) {
      tasks.remove(this);
      return super.cancel(mayInterruptIfRunning);
    }

    @Override public long getDelay(TimeUnit unit) {
      return unit.convert(dueTimeNanos - currentTimeNanos, TimeUnit.NANOSECONDS);
    }

    @Override public int compareTo(Delayed other) {
      ScheduledTask otherTask = (ScheduledTask) other;
      if (dueTimeNanos > otherTask.dueTimeNanos) {
        return 1;
      } else if (dueTimeNanos < otherTask.dueTimeNanos) {
        return -1;
      } else {
        return 0;
      }
    }

    void complete() {
      set(null);
    }

    @Override
    public String toString() {
      return "[due=" + dueTimeNanos + ", task=" + command + "]";
    }
  }

  private class ScheduledExecutorImpl implements ScheduledExecutorService {
    @Override public <V> ScheduledFuture<V> schedule(
        Callable<V> callable, long delay, TimeUnit unit) {
      throw new UnsupportedOperationException();
    }

    @Override public ScheduledFuture<?> schedule(Runnable cmd, long delay, TimeUnit unit) {
      ScheduledTask task = new ScheduledTask(currentTimeNanos + unit.toNanos(delay), cmd);
      tasks.add(task);
      return task;
    }

    @Override public ScheduledFuture<?> scheduleAtFixedRate(
        Runnable command, long initialDelay, long period, TimeUnit unit) {
      throw new UnsupportedOperationException();
    }

    @Override public ScheduledFuture<?> scheduleWithFixedDelay(
        Runnable command, long initialDelay, long delay, TimeUnit unit) {
      throw new UnsupportedOperationException();
    }

    @Override public boolean awaitTermination(long timeout, TimeUnit unit) {
      throw new UnsupportedOperationException();
    }

    @Override public <T> List<Future<T>> invokeAll(Collection<? extends Callable<T>> tasks) {
      throw new UnsupportedOperationException();
    }

    @Override public <T> List<Future<T>> invokeAll(
        Collection<? extends Callable<T>> tasks, long timeout, TimeUnit unit) {
      throw new UnsupportedOperationException();
    }

    @Override public <T> T invokeAny(Collection<? extends Callable<T>> tasks) {
      throw new UnsupportedOperationException();
    }

    @Override public <T> T invokeAny(
        Collection<? extends Callable<T>> tasks, long timeout, TimeUnit unit) {
      throw new UnsupportedOperationException();
    }

    @Override public boolean isShutdown() {
      throw new UnsupportedOperationException();
    }

    @Override public boolean isTerminated() {
      throw new UnsupportedOperationException();
    }

    @Override public void shutdown() {
      throw new UnsupportedOperationException();
    }

    @Override public List<Runnable> shutdownNow() {
      throw new UnsupportedOperationException();
    }

    @Override public <T> Future<T> submit(Callable<T> task) {
      throw new UnsupportedOperationException();
    }

    @Override public Future<?> submit(Runnable task) {
      throw new UnsupportedOperationException();
    }

    @Override public <T> Future<T> submit(Runnable task, T result) {
      throw new UnsupportedOperationException();
    }

    @Override public void execute(Runnable command) {
      // Since it is being enqueued immediately, no point in tracing the future for cancellation.
      Future<?> unused = schedule(command, 0, TimeUnit.NANOSECONDS);
    }
  }

  /**
   * Provides a partially implemented instance of {@link ScheduledExecutorService} that uses the
   * fake clock ticker for testing.
   */
  public ScheduledExecutorService getScheduledExecutorService() {
    return scheduledExecutorService;
  }

  /**
   * Provides a stopwatch instance that uses the fake clock ticker.
   */
  public Supplier<Stopwatch> getStopwatchSupplier() {
    return stopwatchSupplier;
  }

  /**
   * Ticker of the FakeClock.
   */
  public Ticker getTicker() {
    return ticker;
  }

  /**
   * Run all due tasks.
   *
   * @return the number of tasks run by this call
   */
  public int runDueTasks() {
    int count = 0;
    while (true) {
      ScheduledTask task = tasks.peek();
      if (task == null || task.dueTimeNanos > currentTimeNanos) {
        break;
      }
      if (tasks.remove(task)) {
        task.command.run();
        task.complete();
        count++;
      }
    }
    return count;
  }

  /**
   * Return all due tasks.
   */
  public Collection<ScheduledTask> getDueTasks() {
    ArrayList<ScheduledTask> result = new ArrayList<>();
    for (ScheduledTask task : tasks) {
      if (task.dueTimeNanos > currentTimeNanos) {
        continue;
      }
      result.add(task);
    }
    return result;
  }

  /**
   * Return all unrun tasks.
   */
  public Collection<ScheduledTask> getPendingTasks() {
    return getPendingTasks(ACCEPT_ALL_FILTER);
  }

  /**
   * Return all unrun tasks accepted by the given filter.
   */
  public Collection<ScheduledTask> getPendingTasks(TaskFilter filter) {
    ArrayList<ScheduledTask> result = new ArrayList<>();
    for (ScheduledTask task : tasks) {
      if (filter.shouldAccept(task.command)) {
        result.add(task);
      }
    }
    return result;
  }

  /**
   * Forward the time by the given duration and run all due tasks.
   *
   * @return the number of tasks run by this call
   */
  public int forwardTime(long value, TimeUnit unit) {
    currentTimeNanos += unit.toNanos(value);
    return runDueTasks();
  }

  /**
   * Forward the time by the given nanoseconds and run all due tasks.
   *
   * @return the number of tasks run by this call
   */
  public int forwardNanos(long nanos) {
    return forwardTime(nanos, TimeUnit.NANOSECONDS);
  }

  /**
   * Return the number of queued tasks.
   */
  public int numPendingTasks() {
    return tasks.size();
  }

  /**
   * Return the number of queued tasks accepted by the given filter.
   */
  public int numPendingTasks(TaskFilter filter) {
    int count = 0;
    for (ScheduledTask task : tasks) {
      if (filter.shouldAccept(task.command)) {
        count++;
      }
    }
    return count;
  }

  public long currentTimeMillis() {
    // Normally millis and nanos are of different epochs. Add an offset to simulate that.
    return TimeUnit.NANOSECONDS.toMillis(currentTimeNanos + 123456789L);
  }

  /**
   * A filter that allows us to have fine grained control over which tasks are accepted for certain
   * operation.
   */
  public interface TaskFilter {
    /**
     * Inspect the Runnable and returns true if it should be accepted.
     */
    boolean shouldAccept(Runnable runnable);
  }
}
