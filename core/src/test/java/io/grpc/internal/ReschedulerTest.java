/*
 * Copyright 2018 The gRPC Authors
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

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link Rescheduler}.
 */
@RunWith(JUnit4.class)
public class ReschedulerTest {

  private final Runner runner = new Runner();
  private final Exec exec = new Exec();
  private final FakeClock scheduler = new FakeClock();
  private final Rescheduler rescheduler = new Rescheduler(
      runner,
      exec,
      scheduler.getScheduledExecutorService(),
      scheduler.getStopwatchSupplier().get());

  @Test
  public void runs() {
    assertFalse(runner.ran);
    rescheduler.reschedule(1, TimeUnit.NANOSECONDS);
    assertFalse(runner.ran);

    scheduler.forwardNanos(1);

    assertTrue(runner.ran);
  }

  @Test
  public void cancels() {
    assertFalse(runner.ran);
    rescheduler.reschedule(1, TimeUnit.NANOSECONDS);
    assertFalse(runner.ran);
    rescheduler.cancel(/* permanent= */ false);

    scheduler.forwardNanos(1);

    assertFalse(runner.ran);
    assertTrue(exec.executed);
  }

  @Test
  public void cancelPermanently() {
    assertFalse(runner.ran);
    rescheduler.reschedule(1, TimeUnit.NANOSECONDS);
    assertFalse(runner.ran);
    rescheduler.cancel(/* permanent= */ true);

    scheduler.forwardNanos(1);

    assertFalse(runner.ran);
    assertFalse(exec.executed);
  }

  @Test
  public void reschedules() {
    assertFalse(runner.ran);
    rescheduler.reschedule(1, TimeUnit.NANOSECONDS);
    assertFalse(runner.ran);
    assertFalse(exec.executed);
    rescheduler.reschedule(50, TimeUnit.NANOSECONDS);
    assertFalse(runner.ran);
    assertFalse(exec.executed);

    scheduler.forwardNanos(1);
    assertFalse(runner.ran);
    assertTrue(exec.executed);

    scheduler.forwardNanos(50);

    assertTrue(runner.ran);
  }

  @Test
  public void reschedulesShortDelay() {
    assertFalse(runner.ran);
    rescheduler.reschedule(50, TimeUnit.NANOSECONDS);
    assertFalse(runner.ran);
    assertFalse(exec.executed);
    rescheduler.reschedule(1, TimeUnit.NANOSECONDS);
    assertFalse(runner.ran);
    assertFalse(exec.executed);

    scheduler.forwardNanos(1);
    assertTrue(runner.ran);
    assertTrue(exec.executed);
  }

  private static final class Exec implements Executor {
    boolean executed;

    @Override
    public void execute(Runnable command) {
      executed = true;

      command.run();
    }
  }

  private static final class Runner implements Runnable {
    boolean ran;

    @Override
    public void run() {
      ran = true;
    }
  }
}
