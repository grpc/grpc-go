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

import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

/**
 * An absolute point in time, generally for tracking when a task should be completed. A deadline is
 * immutable except for the passage of time causing it to expire.
 *
 * <p>Many systems use timeouts, which are relative to the start of the operation. However, being
 * relative causes them to be poorly suited for managing higher-level tasks where there are many
 * components and sub-operations that may not know the time of the initial "start of the operation."
 * However, a timeout can be converted to a {@code Deadline} at the start of the operation and then
 * passed to the various components unambiguously.
 */
public final class Deadline implements Comparable<Deadline> {
  private static final SystemTicker SYSTEM_TICKER = new SystemTicker();
  // nanoTime has a range of just under 300 years. Only allow up to 100 years in the past or future
  // to prevent wraparound as long as process runs for less than ~100 years.
  private static final long MAX_OFFSET = TimeUnit.DAYS.toNanos(100 * 365);
  private static final long MIN_OFFSET = -MAX_OFFSET;

  /**
   * Create a deadline that will expire at the specified offset from the current system clock.
   * @param duration A non-negative duration.
   * @param units The time unit for the duration.
   * @return A new deadline.
   */
  public static Deadline after(long duration, TimeUnit units) {
    return after(duration, units, SYSTEM_TICKER);
  }

  // For testing
  static Deadline after(long duration, TimeUnit units, Ticker ticker) {
    checkNotNull(units, "units");
    return new Deadline(ticker, units.toNanos(duration), true);
  }

  private final Ticker ticker;
  private final long deadlineNanos;
  private volatile boolean expired;

  private Deadline(Ticker ticker, long offset, boolean baseInstantAlreadyExpired) {
    this(ticker, ticker.read(), offset, baseInstantAlreadyExpired);
  }

  private Deadline(Ticker ticker, long baseInstant, long offset,
      boolean baseInstantAlreadyExpired) {
    this.ticker = ticker;
    // Clamp to range [MIN_OFFSET, MAX_OFFSET]
    offset = Math.min(MAX_OFFSET, Math.max(MIN_OFFSET, offset));
    deadlineNanos = baseInstant + offset;
    expired = baseInstantAlreadyExpired && offset <= 0;
  }

  /**
   * Has this deadline expired
   * @return {@code true} if it has, otherwise {@code false}.
   */
  public boolean isExpired() {
    if (!expired) {
      if (deadlineNanos - ticker.read() <= 0) {
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
    return this.deadlineNanos - other.deadlineNanos < 0;
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
  // TODO(ejona): This method can cause deadlines to grow too far apart. For example:
  // Deadline.after(100 * 365, DAYS).offset(100 * 365, DAYS) would be less than
  // Deadline.after(-100 * 365, DAYS)
  public Deadline offset(long offset, TimeUnit units) {
    // May already be expired
    if (offset == 0) {
      return this;
    }
    return new Deadline(ticker, deadlineNanos, units.toNanos(offset), isExpired());
  }

  /**
   * How much time is remaining in the specified time unit. Internal units are maintained as
   * nanoseconds and conversions are subject to the constraints documented for
   * {@link TimeUnit#convert}. If there is no time remaining, the returned duration is how
   * long ago the deadline expired.
   */
  public long timeRemaining(TimeUnit unit) {
    final long nowNanos = ticker.read();
    if (!expired && deadlineNanos - nowNanos <= 0) {
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
    checkNotNull(task, "task");
    checkNotNull(scheduler, "scheduler");
    return scheduler.schedule(task, deadlineNanos - ticker.read(), TimeUnit.NANOSECONDS);
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

  /** Time source representing nanoseconds since fixed but arbitrary point in time. */
  abstract static class Ticker {
    /** Returns the number of nanoseconds since this source's epoch. */
    public abstract long read();
  }

  private static class SystemTicker extends Ticker {
    @Override
    public long read() {
      return System.nanoTime();
    }
  }

  private static <T> T checkNotNull(T reference, Object errorMessage) {
    if (reference == null) {
      throw new NullPointerException(String.valueOf(errorMessage));
    }
    return reference;
  }
}
