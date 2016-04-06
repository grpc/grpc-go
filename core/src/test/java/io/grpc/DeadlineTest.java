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

import static com.google.common.truth.Truth.assertAbout;
import static io.grpc.testing.DeadlineSubject.deadline;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import com.google.common.truth.Truth;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

import java.util.Random;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * Tests for {@link Context}.
 */
@RunWith(JUnit4.class)
public class DeadlineTest {
  private FakeTicker ticker = new FakeTicker();

  @Test
  public void defaultTickerIsSystemTicker() {
    Deadline d = Deadline.after(0, TimeUnit.SECONDS);
    ticker.reset(System.nanoTime());
    Deadline reference = Deadline.after(0, TimeUnit.SECONDS, ticker);
    // Allow inaccuracy to account for system time advancing during test.
    assertAbout(deadline()).that(d).isWithin(20, TimeUnit.MILLISECONDS).of(reference);
  }

  @Test
  public void immediateDeadlineIsExpired() {
    Deadline deadline = Deadline.after(0, TimeUnit.SECONDS, ticker);
    assertTrue(deadline.isExpired());
  }

  @Test
  public void shortDeadlineEventuallyExpires() throws Exception {
    Deadline d = Deadline.after(100, TimeUnit.MILLISECONDS, ticker);
    assertTrue(d.timeRemaining(TimeUnit.NANOSECONDS) > 0);
    assertFalse(d.isExpired());
    ticker.increment(101, TimeUnit.MILLISECONDS);

    assertTrue(d.isExpired());
    assertEquals(-1, d.timeRemaining(TimeUnit.MILLISECONDS));
  }

  @Test
  public void deadlineMatchesLongValue() {
    assertEquals(10, Deadline.after(10, TimeUnit.MINUTES, ticker).timeRemaining(TimeUnit.MINUTES));
  }

  @Test
  public void pastDeadlineIsExpired() {
    Deadline d = Deadline.after(-1, TimeUnit.SECONDS, ticker);
    assertTrue(d.isExpired());
    assertEquals(-1000, d.timeRemaining(TimeUnit.MILLISECONDS));
  }

  @Test
  public void deadlineDoesNotOverflowOrUnderflow() {
    Deadline after = Deadline.after(Long.MAX_VALUE, TimeUnit.NANOSECONDS, ticker);
    assertFalse(after.isExpired());

    Deadline before = Deadline.after(-Long.MAX_VALUE, TimeUnit.NANOSECONDS, ticker);
    assertTrue(before.isExpired());

    assertTrue(before.isBefore(after));
  }

  @Test
  public void beforeExpiredDeadlineIsExpired() {
    Deadline base = Deadline.after(0, TimeUnit.SECONDS, ticker);
    assertTrue(base.isExpired());
    assertTrue(base.offset(-1, TimeUnit.SECONDS).isExpired());
  }

  @Test
  public void afterExpiredDeadlineIsNotExpired() {
    Deadline base = Deadline.after(0, TimeUnit.SECONDS, ticker);
    assertTrue(base.isExpired());
    assertFalse(base.offset(100, TimeUnit.SECONDS).isExpired());
  }

  @Test
  public void zeroOffsetIsSameDeadline() {
    Deadline base = Deadline.after(0, TimeUnit.SECONDS, ticker);
    assertSame(base, base.offset(0, TimeUnit.SECONDS));
  }

  @Test
  public void runOnEventualExpirationIsExecuted() throws Exception {
    Deadline base = Deadline.after(50, TimeUnit.MICROSECONDS, ticker);
    ScheduledExecutorService mockScheduler = mock(ScheduledExecutorService.class);
    final AtomicBoolean executed = new AtomicBoolean();
    base.runOnExpiration(
        new Runnable() {
          @Override
          public void run() {
            executed.set(true);
          }
        }, mockScheduler);
    assertFalse(executed.get());
    ArgumentCaptor<Runnable> runnableCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(mockScheduler).schedule(runnableCaptor.capture(), eq(50000L), eq(TimeUnit.NANOSECONDS));
    runnableCaptor.getValue().run();
    assertTrue(executed.get());
  }

  @Test
  public void runOnAlreadyExpiredIsExecutedOnExecutor() throws Exception {
    Deadline base = Deadline.after(0, TimeUnit.MICROSECONDS, ticker);
    ScheduledExecutorService mockScheduler = mock(ScheduledExecutorService.class);
    final AtomicBoolean executed = new AtomicBoolean();
    base.runOnExpiration(
        new Runnable() {
          @Override
          public void run() {
            executed.set(true);
          }
        }, mockScheduler);
    assertFalse(executed.get());
    ArgumentCaptor<Runnable> runnableCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(mockScheduler).schedule(runnableCaptor.capture(), eq(0L), eq(TimeUnit.NANOSECONDS));
    runnableCaptor.getValue().run();
    assertTrue(executed.get());
  }

  @Test
  public void toString_exact() {
    Deadline d = Deadline.after(0, TimeUnit.MILLISECONDS, ticker);
    assertEquals("0 ns from now", d.toString());
  }

  @Test
  public void toString_after() {
    Deadline d = Deadline.after(-1, TimeUnit.MINUTES, ticker);
    assertEquals("-60000000000 ns from now", d.toString());
  }

  @Test
  public void compareTo_greater() {
    Deadline d1 = Deadline.after(10, TimeUnit.SECONDS, ticker);
    ticker.increment(1, TimeUnit.NANOSECONDS);
    Deadline d2 = Deadline.after(10, TimeUnit.SECONDS, ticker);
    Truth.assertThat(d2).isGreaterThan(d1);
  }

  @Test
  public void compareTo_less() {
    Deadline d1 = Deadline.after(10, TimeUnit.SECONDS, ticker);
    ticker.increment(1, TimeUnit.NANOSECONDS);
    Deadline d2 = Deadline.after(10, TimeUnit.SECONDS, ticker);
    Truth.assertThat(d1).isLessThan(d2);
  }

  @Test
  public void compareTo_same() {
    Deadline d1 = Deadline.after(10, TimeUnit.SECONDS, ticker);
    Deadline d2 = Deadline.after(10, TimeUnit.SECONDS, ticker);
    Truth.assertThat(d1).isEquivalentAccordingToCompareTo(d2);
  }

  @Test
  public void toString_before() {
    Deadline d = Deadline.after(12, TimeUnit.MICROSECONDS, ticker);
    assertEquals("12000 ns from now", d.toString());
  }

  static class FakeTicker extends Deadline.Ticker {
    private static final Random random = new Random();

    private long time = random.nextLong();

    @Override
    public long read() {
      return time;
    }

    public void reset(long time) {
      this.time = time;
    }

    public void increment(long period, TimeUnit unit) {
      if (period < 0) {
        throw new IllegalArgumentException();
      }
      this.time += unit.toNanos(period);
    }
  }
}
