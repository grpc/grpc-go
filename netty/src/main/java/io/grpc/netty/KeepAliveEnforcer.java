/*
 * Copyright 2017, Google Inc. All rights reserved.
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

package io.grpc.netty;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import java.util.concurrent.TimeUnit;
import javax.annotation.CheckReturnValue;

/** Monitors the client's PING usage to make sure the rate is permitted. */
class KeepAliveEnforcer {
  @VisibleForTesting
  static final int MAX_PING_STRIKES = 2;
  @VisibleForTesting
  static final long IMPLICIT_PERMIT_TIME_NANOS = TimeUnit.HOURS.toNanos(2);

  private final boolean permitWithoutCalls;
  private final long minTimeNanos;
  private final Ticker ticker;
  private final long epoch;

  private long lastValidPingTime;
  private boolean hasOutstandingCalls;
  private int pingStrikes;

  public KeepAliveEnforcer(boolean permitWithoutCalls, long minTime, TimeUnit unit) {
    this(permitWithoutCalls, minTime, unit, SystemTicker.INSTANCE);
  }

  @VisibleForTesting
  KeepAliveEnforcer(boolean permitWithoutCalls, long minTime, TimeUnit unit, Ticker ticker) {
    Preconditions.checkArgument(minTime >= 0, "minTime must be non-negative");

    this.permitWithoutCalls = permitWithoutCalls;
    this.minTimeNanos = Math.min(unit.toNanos(minTime), IMPLICIT_PERMIT_TIME_NANOS);
    this.ticker = ticker;
    this.epoch = ticker.nanoTime();
    lastValidPingTime = epoch;
  }

  /** Returns {@code false} when client is misbehaving and should be disconnected. */
  @CheckReturnValue
  public boolean pingAcceptable() {
    long now = ticker.nanoTime();
    boolean valid;
    if (!hasOutstandingCalls && !permitWithoutCalls) {
      valid = compareNanos(lastValidPingTime + IMPLICIT_PERMIT_TIME_NANOS, now) <= 0;
    } else {
      valid = compareNanos(lastValidPingTime + minTimeNanos, now) <= 0;
    }
    if (!valid) {
      pingStrikes++;
      return !(pingStrikes > MAX_PING_STRIKES);
    } else {
      lastValidPingTime = now;
      return true;
    }
  }

  /**
   * Reset any counters because PINGs are allowed in response to something sent. Typically called
   * when sending HEADERS and DATA frames.
   */
  public void resetCounters() {
    lastValidPingTime = epoch;
    pingStrikes = 0;
  }

  /** There are outstanding RPCs on the transport. */
  public void onTransportActive() {
    hasOutstandingCalls = true;
  }

  /** There are no outstanding RPCs on the transport. */
  public void onTransportIdle() {
    hasOutstandingCalls = false;
  }

  /**
   * Positive when time1 is greater; negative when time2 is greater; 0 when equal. It is important
   * to use something like this instead of directly comparing nano times. See {@link
   * System#nanoTime}.
   */
  private static long compareNanos(long time1, long time2) {
    // Possibility of overflow/underflow is on purpose and necessary for correctness
    return time1 - time2;
  }

  @VisibleForTesting
  interface Ticker {
    long nanoTime();
  }

  @VisibleForTesting
  static class SystemTicker implements Ticker {
    public static final SystemTicker INSTANCE = new SystemTicker();

    @Override
    public long nanoTime() {
      return System.nanoTime();
    }
  }
}
