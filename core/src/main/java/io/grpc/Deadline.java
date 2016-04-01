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

package io.grpc;

import com.google.common.base.Preconditions;

import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

/**
 * An absolute deadline in system time.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/262")
public final class Deadline implements Comparable<Deadline> {

  /**
   * Create a deadline that will expire at the specified offset from the current system clock.
   * @param duration A non-negative duration.
   * @param units The time unit for the duration.
   * @return A new deadline.
   */
  public static Deadline after(long duration, TimeUnit units) {
    Preconditions.checkNotNull(units);
    return new Deadline(System.nanoTime(), units.toNanos(duration), true);
  }

  private final long deadlineNanos;
  private volatile boolean expired;

  private Deadline(long baseInstant, long offset, boolean baseInstantAlreadyExpired) {
    if (offset > 0 && Long.MAX_VALUE - offset < baseInstant) {
      deadlineNanos = Long.MAX_VALUE;
      expired = false;
    } else if (offset < 0 && Long.MIN_VALUE - offset > baseInstant) {
      deadlineNanos = Long.MIN_VALUE;
      expired = true;
    } else {
      deadlineNanos = baseInstant + offset;
      expired = baseInstantAlreadyExpired && offset <= 0;
    }
  }

  /**
   * Has this deadline expired
   * @return {@code true} if it has, otherwise {@code false}.
   */
  public boolean isExpired() {
    if (!expired) {
      if (deadlineNanos <= System.nanoTime()) {
        expired = true;
      } else {
        return false;
      }
    }
    return true;
  }

  /**
   * Is {@code this} deadline before another.
   */
  public boolean isBefore(Deadline other) {
    return this.deadlineNanos < other.deadlineNanos;
  }

  /**
   * Return the minimum deadline of {@code this} or an other deadline.
   * @param other deadline to compare with {@code this}.
   */
  public Deadline minimum(Deadline other) {
    return isBefore(other) ? this : other;
  }

  /**
   * Create a new deadline that is offset from {@code this}.
   */
  public Deadline offset(long offset, TimeUnit units) {
    // May already be expired
    if (offset == 0) {
      return this;
    }
    return new Deadline(deadlineNanos, units.toNanos(offset), isExpired());
  }

  /**
   * How much time is remaining in the specified time unit. Internal units are maintained as
   * nanoseconds and conversions are subject to the constraints documented for
   * {@link TimeUnit#convert}. If there is no time remaining, the returned duration is how
   * long ago the deadline expired.
   */
  public long timeRemaining(TimeUnit unit) {
    final long nowNanos = System.nanoTime();
    if (!expired && deadlineNanos <= nowNanos) {
      expired = true;
    }
    return unit.convert(deadlineNanos - nowNanos, TimeUnit.NANOSECONDS);
  }

  /**
   * Schedule a task to be run when the deadline expires.
   * @param task to run on expiration
   * @param scheduler used to execute the task
   * @return {@link ScheduledFuture} which can be used to cancel execution of the task
   */
  public ScheduledFuture<?> runOnExpiration(Runnable task, ScheduledExecutorService scheduler) {
    Preconditions.checkNotNull(task, "task");
    Preconditions.checkNotNull(scheduler, "scheduler");
    return scheduler.schedule(task, deadlineNanos - System.nanoTime(), TimeUnit.NANOSECONDS);
  }

  @Override
  public String toString() {
    return timeRemaining(TimeUnit.NANOSECONDS) + " ns from now";
  }

  @Override
  public int compareTo(Deadline that) {
    long diff = this.deadlineNanos - that.deadlineNanos;
    if (diff < 0) {
      return -1;
    } else if (diff > 0) {
      return 1;
    }
    return 0;
  }
}
