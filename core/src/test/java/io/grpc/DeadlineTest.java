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
import static java.util.concurrent.TimeUnit.NANOSECONDS;
import static java.util.concurrent.TimeUnit.SECONDS;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.truth.Truth;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Tests for {@link Context}.
 */
@RunWith(JUnit4.class)
public class DeadlineTest {

  // Allowed inaccuracy when comparing the remaining time of a deadline.
  private final long maxDelta = TimeUnit.MILLISECONDS.toNanos(20);

  @Test
  public void immediateDeadlineIsExpired() {
    Deadline deadline = Deadline.after(0, TimeUnit.SECONDS);
    assertTrue(deadline.isExpired());
  }

  @Test
  public void shortDeadlineEventuallyExpires() throws Exception {
    Deadline d = Deadline.after(100, TimeUnit.MILLISECONDS);
    assertTrue(d.timeRemaining(TimeUnit.NANOSECONDS) > 0);
    assertFalse(d.isExpired());
    Thread.sleep(101);

    assertTrue(d.isExpired());
    assertFalse(d.timeRemaining(TimeUnit.NANOSECONDS) > 0);
    assertAbout(deadline()).that(d).isWithin(maxDelta, NANOSECONDS).of(Deadline.after(0, SECONDS));
  }

  @Test
  public void deadlineMatchesLongValue() {
    long minutes = Deadline.after(10, TimeUnit.MINUTES).timeRemaining(TimeUnit.MINUTES);

    assertTrue(minutes + " != " + 10, Math.abs(minutes - 10) <= 1);
  }

  @Test
  public void pastDeadlineIsExpired() {
    Deadline d = Deadline.after(-1, TimeUnit.SECONDS);
    assertTrue(d.isExpired());

    assertAbout(deadline()).that(d).isWithin(maxDelta, NANOSECONDS).of(Deadline.after(-1, SECONDS));
  }

  @Test
  public void deadlineDoesNotOverflowOrUnderflow() {
    Deadline after = Deadline.after(Long.MAX_VALUE, TimeUnit.NANOSECONDS);
    assertFalse(after.isExpired());

    Deadline before = Deadline.after(-Long.MAX_VALUE, TimeUnit.NANOSECONDS);
    assertTrue(before.isExpired());

    assertTrue(before.isBefore(after));
  }

  @Test
  public void beforeExpiredDeadlineIsExpired() {
    Deadline base = Deadline.after(0, TimeUnit.SECONDS);
    assertTrue(base.isExpired());
    assertTrue(base.offset(-1, TimeUnit.SECONDS).isExpired());
  }

  @Test
  public void afterExpiredDeadlineIsNotExpired() {
    Deadline base = Deadline.after(0, TimeUnit.SECONDS);
    assertTrue(base.isExpired());
    assertFalse(base.offset(100, TimeUnit.SECONDS).isExpired());
  }

  @Test
  public void zeroOffsetIsSameDeadline() {
    Deadline base = Deadline.after(0, TimeUnit.SECONDS);
    assertSame(base, base.offset(0, TimeUnit.SECONDS));
  }

  @Test
  public void runOnEventualExpirationIsExecuted() throws Exception {
    Deadline base = Deadline.after(50, TimeUnit.MILLISECONDS);
    final CountDownLatch latch = new CountDownLatch(1);
    base.runOnExpiration(
        new Runnable() {
          @Override
          public void run() {
            latch.countDown();
          }
        }, Executors.newSingleThreadScheduledExecutor());
    if (!latch.await(70, TimeUnit.MILLISECONDS)) {
      fail("Deadline listener did not execute in time");
    }
  }

  @Test
  public void runOnAlreadyExpiredIsExecuted() throws Exception {
    Deadline base = Deadline.after(0, TimeUnit.MILLISECONDS);
    final CountDownLatch latch = new CountDownLatch(1);
    base.runOnExpiration(
        new Runnable() {
          @Override
          public void run() {
            latch.countDown();
          }
        }, Executors.newSingleThreadScheduledExecutor());
    if (!latch.await(10, TimeUnit.MILLISECONDS)) {
      fail("Deadline listener did not execute in time");
    }
  }

  @Test
  public void toString_exact() {
    Deadline d = Deadline.after(0, TimeUnit.MILLISECONDS);

    assertAbout(deadline()).that(extractRemainingTime(d.toString()))
        .isWithin(maxDelta, NANOSECONDS).of(d);
  }

  @Test
  public void toString_after() {
    Deadline d = Deadline.after(-1, TimeUnit.HOURS);

    assertAbout(deadline()).that(extractRemainingTime(d.toString()))
        .isWithin(maxDelta, NANOSECONDS).of(d);
  }

  @Test
  public void compareTo_greater() {
    Deadline d1 = Deadline.after(10, TimeUnit.SECONDS);
    Deadline d2 = Deadline.after(10, TimeUnit.SECONDS);
    // Assume that two calls take more than 1 ns.
    Truth.assertThat(d2).isGreaterThan(d1);
  }

  @Test
  public void compareTo_less() {
    Deadline d1 = Deadline.after(10, TimeUnit.SECONDS);
    Deadline d2 = Deadline.after(10, TimeUnit.SECONDS);
    // Assume that two calls take more than 1 ns.
    Truth.assertThat(d1).isLessThan(d2);
  }

  @Test
  public void compareTo_same() {
    Deadline d1 = Deadline.after(10, TimeUnit.SECONDS);
    Deadline d2 = d1.offset(0, TimeUnit.SECONDS);
    Truth.assertThat(d1).isEquivalentAccordingToCompareTo(d2);
  }

  @Test
  public void toString_before() {
    Deadline d = Deadline.after(10, TimeUnit.SECONDS);

    assertAbout(deadline()).that(extractRemainingTime(d.toString()))
        .isWithin(maxDelta, NANOSECONDS).of(d);
  }

  static Deadline extractRemainingTime(String deadlineStr) {
    final Pattern p = Pattern.compile(".*?(-?\\d+) ns from now.*");
    Matcher m = p.matcher(deadlineStr);
    assertTrue(deadlineStr, m.matches());
    assertEquals(deadlineStr, 1, m.groupCount());
    return Deadline.after(Long.valueOf(m.group(1)), NANOSECONDS);
  }
}
