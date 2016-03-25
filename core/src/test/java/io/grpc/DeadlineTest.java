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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

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
  private final long delta = TimeUnit.MILLISECONDS.toNanos(20);

  @Test
  public void immediateDeadlineIsExpired() {
    Deadline deadline = Deadline.after(0, TimeUnit.SECONDS);
    assertTrue(deadline.isExpired());
    assertEquals(0, deadline.timeRemaining(TimeUnit.NANOSECONDS), delta);
  }

  @Test
  public void shortDeadlineEventuallyExpires() throws Exception {
    Deadline deadline = Deadline.after(100, TimeUnit.MILLISECONDS);
    assertTrue(deadline.timeRemaining(TimeUnit.NANOSECONDS) > 0);
    assertFalse(deadline.isExpired());
    Thread.sleep(101);
    assertTrue(deadline.isExpired());
    assertEquals(0, deadline.timeRemaining(TimeUnit.NANOSECONDS), delta);
  }

  @Test
  public void pastDeadlineIsExpired() {
    Deadline deadline = Deadline.after(-1, TimeUnit.SECONDS);
    assertTrue(deadline.isExpired());
    assertEquals(TimeUnit.SECONDS.toNanos(-1), deadline.timeRemaining(TimeUnit.NANOSECONDS), delta);
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
  public void testToString() {
    Deadline d = Deadline.after(0, TimeUnit.MILLISECONDS);
    assertEquals(0, extractRemainingTime(d.toString()), delta);

    d = Deadline.after(-1, TimeUnit.HOURS);
    assertEquals(d.timeRemaining(TimeUnit.NANOSECONDS), extractRemainingTime(d.toString()), delta);

    d = Deadline.after(10, TimeUnit.SECONDS);
    assertEquals(d.timeRemaining(TimeUnit.NANOSECONDS), extractRemainingTime(d.toString()), delta);
  }

  private static long extractRemainingTime(String deadlineStr) {
    final Pattern p = Pattern.compile("(\\-?[0-9]+) ns from now");
    Matcher m = p.matcher(deadlineStr);
    assertTrue(m.matches());
    assertEquals(1, m.groupCount());

    return Long.valueOf(m.group(1));
  }
}
