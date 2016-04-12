/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import com.google.common.base.Ticker;
import com.google.common.util.concurrent.AbstractFuture;

import java.util.Collection;
import java.util.List;
import java.util.PriorityQueue;
import java.util.concurrent.Callable;
import java.util.concurrent.Delayed;
import java.util.concurrent.Future;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

/**
 * A manipulated clock that exports a {@link Ticker} and a {@link ScheduledExecutorService}.
 *
 * <p>To simulate the locking scenario of using real executors, it never runs tasks within {@code
 * schedule()} or {@code execute()}. Instead, you should call {@link #runDueTasks} in your test
 * method to run all due tasks. {@link #forwardTime} and {@link #forwardMillis} call {@link
 * #runDueTasks} automatically.
 */
public final class FakeClock {

  public final ScheduledExecutorService scheduledExecutorService = new ScheduledExecutorImpl();
  final Ticker ticker = new Ticker() {
      @Override public long read() {
        return TimeUnit.MILLISECONDS.toNanos(currentTimeNanos);
      }
    };

  private final PriorityQueue<ScheduledTask> tasks = new PriorityQueue<ScheduledTask>();
  private long currentTimeNanos;

  private class ScheduledTask extends AbstractFuture<Void> implements ScheduledFuture<Void> {
    final Runnable command;
    final long dueTimeNanos;

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
      schedule(command, 0, TimeUnit.NANOSECONDS);
    }
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
      tasks.poll();
      task.command.run();
      task.complete();
      count++;
    }
    return count;
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
   * Forward the time by the given milliseconds and run all due tasks.
   *
   * @return the number of tasks run by this call
   */
  public int forwardMillis(long millis) {
    return forwardTime(millis, TimeUnit.MILLISECONDS);
  }

  /**
   * Return the number of queued tasks.
   */
  public int numPendingTasks() {
    return tasks.size();
  }
}
