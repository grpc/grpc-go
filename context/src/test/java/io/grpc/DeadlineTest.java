/*
 * Copyright 2016 The gRPC Authors
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
import java.util.Arrays;
import java.util.concurrent.Future;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.Parameterized;
import org.junit.runners.Parameterized.Parameters;
import org.mockito.ArgumentCaptor;

/**
 * Tests for {@link Context}.
 */
@RunWith(Parameterized.class)
public class DeadlineTest {
  /** Ticker epochs to vary testing. */
  @Parameters
  public static Iterable<Object[]> data() {
    return Arrays.asList(new Object[][] {
      // MAX_VALUE / 2 is important because the signs are generally the same for past and future
      // deadlines.
      {Long.MAX_VALUE / 2}, {0}, {Long.MAX_VALUE}, {Long.MIN_VALUE}
    });
  }

  private FakeTicker ticker = new FakeTicker();

  public DeadlineTest(long epoch) {
    ticker.reset(epoch);
  }

  @Test
  public void defaultTickerIsSystemTicker() {
    Deadline d = Deadline.after(0, TimeUnit.SECONDS);
    ticker.reset(System.nanoTime());
    Deadline reference = Deadline.after(0, TimeUnit.SECONDS, ticker);
    // Allow inaccuracy to account for system time advancing during test.
    assertAbout(deadline()).that(d).isWithin(1, TimeUnit.SECONDS).of(reference);
  }

  @Test
  public void timeCanOverflow() {
    ticker.reset(Long.MAX_VALUE);
    Deadline d = Deadline.after(10, TimeUnit.DAYS, ticker);
    assertEquals(10, d.timeRemaining(TimeUnit.DAYS));
    assertTrue(Deadline.after(0, TimeUnit.DAYS, ticker).isBefore(d));
    assertFalse(d.isExpired());

    ticker.increment(10, TimeUnit.DAYS);
    assertTrue(d.isExpired());
  }

  @Test
  public void timeCanUnderflow() {
    ticker.reset(Long.MIN_VALUE);
    Deadline d = Deadline.after(-10, TimeUnit.DAYS, ticker);
    assertEquals(-10, d.timeRemaining(TimeUnit.DAYS));
    assertTrue(d.isBefore(Deadline.after(0, TimeUnit.DAYS, ticker)));
    assertTrue(d.isExpired());
  }

  @Test
  public void deadlineClamps() {
    Deadline d = Deadline.after(-300 * 365, TimeUnit.DAYS, ticker);
    Deadline d2 = Deadline.after(300 * 365, TimeUnit.DAYS, ticker);
    assertTrue(d.isBefore(d2));

    Deadline d3 = Deadline.after(-200 * 365, TimeUnit.DAYS, ticker);
    // d and d3 are equal
    assertFalse(d.isBefore(d3));
    assertFalse(d3.isBefore(d));
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
  public void beforeNotExpiredDeadlineMayBeExpired() {
    Deadline base = Deadline.after(10, TimeUnit.SECONDS, ticker);
    assertFalse(base.isExpired());
    assertFalse(base.offset(-1, TimeUnit.SECONDS).isExpired());
    assertTrue(base.offset(-11, TimeUnit.SECONDS).isExpired());
  }

  @Test
  public void afterExpiredDeadlineMayBeExpired() {
    Deadline base = Deadline.after(-10, TimeUnit.SECONDS, ticker);
    assertTrue(base.isExpired());
    assertTrue(base.offset(1, TimeUnit.SECONDS).isExpired());
    assertFalse(base.offset(11, TimeUnit.SECONDS).isExpired());
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
    Future<?> unused = base.runOnExpiration(
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
    Future<?> unused = base.runOnExpiration(
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

  private static class FakeTicker extends Deadline.Ticker {
    private long time;

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
