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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import com.google.common.base.Stopwatch;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link FakeClock}. */
@RunWith(JUnit4.class)
public class FakeClockTest {

  @Test
  public void testScheduledExecutorService_sameInstance() {
    FakeClock fakeClock = new FakeClock();
    ScheduledExecutorService scheduledExecutorService1 = fakeClock.getScheduledExecutorService();
    ScheduledExecutorService scheduledExecutorService2 = fakeClock.getScheduledExecutorService();
    assertTrue(scheduledExecutorService1 == scheduledExecutorService2);
  }

  @Test
  public void testScheduledExecutorService_isDone() {
    FakeClock fakeClock = new FakeClock();
    ScheduledFuture<?> future = fakeClock.getScheduledExecutorService()
        .schedule(newRunnable(), 100L, TimeUnit.NANOSECONDS);

    fakeClock.forwardNanos(99L);
    assertFalse(future.isDone());

    fakeClock.forwardNanos(2L);
    assertTrue(future.isDone());
  }

  @Test
  public void testScheduledExecutorService_cancel() {
    FakeClock fakeClock = new FakeClock();
    ScheduledFuture<?> future = fakeClock.getScheduledExecutorService()
        .schedule(newRunnable(), 100L, TimeUnit.NANOSECONDS);

    fakeClock.forwardNanos(99L);
    future.cancel(false);

    fakeClock.forwardNanos(2);
    assertTrue(future.isCancelled());
  }

  @Test
  public void testScheduledExecutorService_getDelay() {
    FakeClock fakeClock = new FakeClock();
    ScheduledFuture<?> future = fakeClock.getScheduledExecutorService()
        .schedule(newRunnable(), 100L, TimeUnit.NANOSECONDS);

    fakeClock.forwardNanos(90L);
    assertEquals(10L, future.getDelay(TimeUnit.NANOSECONDS));
  }

  @Test
  public void testScheduledExecutorService_result() {
    FakeClock fakeClock = new FakeClock();
    final boolean[] result = new boolean[]{false};
    ScheduledFuture<?> unused = fakeClock.getScheduledExecutorService().schedule(
        new Runnable() {
          @Override
          public void run() {
            result[0] = true;
          }
        },
        100L,
        TimeUnit.NANOSECONDS);

    fakeClock.forwardNanos(100L);
    assertTrue(result[0]);
  }

  @Test
  public void testStopWatch() {
    FakeClock fakeClock = new FakeClock();
    Stopwatch stopwatch = fakeClock.getStopwatchSupplier().get();
    long expectedElapsedNanos = 0L;

    stopwatch.start();

    fakeClock.forwardNanos(100L);
    expectedElapsedNanos += 100L;
    assertEquals(expectedElapsedNanos, stopwatch.elapsed(TimeUnit.NANOSECONDS));

    fakeClock.forwardTime(10L, TimeUnit.MINUTES);
    expectedElapsedNanos += TimeUnit.MINUTES.toNanos(10L);
    assertEquals(expectedElapsedNanos, stopwatch.elapsed(TimeUnit.NANOSECONDS));

    stopwatch.stop();

    fakeClock.forwardNanos(1000L);
    assertEquals(expectedElapsedNanos, stopwatch.elapsed(TimeUnit.NANOSECONDS));

    stopwatch.reset();

    expectedElapsedNanos = 0L;
    assertEquals(expectedElapsedNanos, stopwatch.elapsed(TimeUnit.NANOSECONDS));
  }

  @Test
  @SuppressWarnings("FutureReturnValueIgnored")
  public void testPendingAndDueTasks() {
    FakeClock fakeClock = new FakeClock();
    ScheduledExecutorService scheduledExecutorService = fakeClock.getScheduledExecutorService();

    scheduledExecutorService.schedule(newRunnable(), 200L, TimeUnit.NANOSECONDS);
    scheduledExecutorService.execute(newRunnable());
    scheduledExecutorService.schedule(newRunnable(), 0L, TimeUnit.NANOSECONDS);
    scheduledExecutorService.schedule(newRunnable(), 80L, TimeUnit.NANOSECONDS);
    scheduledExecutorService.schedule(newRunnable(), 90L, TimeUnit.NANOSECONDS);
    scheduledExecutorService.schedule(newRunnable(), 100L, TimeUnit.NANOSECONDS);
    scheduledExecutorService.schedule(newRunnable(), 110L, TimeUnit.NANOSECONDS);
    scheduledExecutorService.schedule(newRunnable(), 120L, TimeUnit.NANOSECONDS);


    assertEquals(8, fakeClock.numPendingTasks());
    assertEquals(2, fakeClock.getDueTasks().size());

    fakeClock.runDueTasks();

    assertEquals(6, fakeClock.numPendingTasks());
    assertEquals(0, fakeClock.getDueTasks().size());

    fakeClock.forwardNanos(90L);

    assertEquals(4, fakeClock.numPendingTasks());
    assertEquals(0, fakeClock.getDueTasks().size());

    fakeClock.forwardNanos(20L);

    assertEquals(2, fakeClock.numPendingTasks());
    assertEquals(0, fakeClock.getDueTasks().size());
  }

  private Runnable newRunnable() {
    return new Runnable() {
      @Override
      public void run() {
      }
    };
  }
}
