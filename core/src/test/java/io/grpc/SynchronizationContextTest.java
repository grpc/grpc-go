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

package io.grpc.internal;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

import io.grpc.SynchronizationContext;
import io.grpc.SynchronizationContext.ScheduledHandle;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.InOrder;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Unit tests for {@link SynchronizationContext}.
 */
@RunWith(JUnit4.class)
public class SynchronizationContextTest {
  private final BlockingQueue<Throwable> uncaughtErrors = new LinkedBlockingQueue<>();
  private final SynchronizationContext syncContext = new SynchronizationContext(
      new Thread.UncaughtExceptionHandler() {
        @Override
        public void uncaughtException(Thread t, Throwable e) {
          uncaughtErrors.add(e);
        }
      });

  @Mock
  private Runnable task1;

  @Mock
  private Runnable task2;

  @Mock
  private Runnable task3;

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  @After public void tearDown() {
    assertThat(uncaughtErrors).isEmpty();
  }

  @Test
  public void singleThread() {
    syncContext.executeLater(task1);
    syncContext.executeLater(task2);
    InOrder inOrder = inOrder(task1, task2, task3);
    inOrder.verifyNoMoreInteractions();
    syncContext.drain();
    inOrder.verify(task1).run();
    inOrder.verify(task2).run();

    syncContext.executeLater(task3);
    inOrder.verifyNoMoreInteractions();
    syncContext.drain();
    inOrder.verify(task3).run();
  }

  @Test
  public void multiThread() throws Exception {
    InOrder inOrder = inOrder(task1, task2);

    final CountDownLatch task1Added = new CountDownLatch(1);
    final CountDownLatch task1Running = new CountDownLatch(1);
    final CountDownLatch task1Proceed = new CountDownLatch(1);
    final CountDownLatch sideThreadDone = new CountDownLatch(1);
    final AtomicReference<Thread> task1Thread = new AtomicReference<Thread>();
    final AtomicReference<Thread> task2Thread = new AtomicReference<Thread>();

    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) {
          task1Thread.set(Thread.currentThread());
          task1Running.countDown();
          try {
            assertTrue(task1Proceed.await(5, TimeUnit.SECONDS));
          } catch (InterruptedException e) {
            throw new RuntimeException(e);
          }
          return null;
        }
      }).when(task1).run();

    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) {
          task2Thread.set(Thread.currentThread());
          return null;
        }
      }).when(task2).run();

    Thread sideThread = new Thread() {
        @Override
        public void run() {
          syncContext.executeLater(task1);
          task1Added.countDown();
          syncContext.drain();
          sideThreadDone.countDown();
        }
      };
    sideThread.start();

    assertTrue(task1Added.await(5, TimeUnit.SECONDS));
    syncContext.executeLater(task2);
    assertTrue(task1Running.await(5, TimeUnit.SECONDS));
    // This will do nothing because task1 is running until task1Proceed is set
    syncContext.drain();

    inOrder.verify(task1).run();
    inOrder.verifyNoMoreInteractions();

    task1Proceed.countDown();
    // drain() on the side thread has returned, which runs task2
    assertTrue(sideThreadDone.await(5, TimeUnit.SECONDS));
    inOrder.verify(task2).run();

    assertSame(sideThread, task1Thread.get());
    assertSame(sideThread, task2Thread.get());
  }

  @Test
  public void taskThrows() {
    InOrder inOrder = inOrder(task1, task2, task3);
    final RuntimeException e = new RuntimeException("Simulated");
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) {
          throw e;
        }
      }).when(task2).run();
    syncContext.executeLater(task1);
    syncContext.executeLater(task2);
    syncContext.executeLater(task3);
    syncContext.drain();
    inOrder.verify(task1).run();
    inOrder.verify(task2).run();
    inOrder.verify(task3).run();
    assertThat(uncaughtErrors).containsExactly(e);
    uncaughtErrors.clear();
  }

  @Test
  public void schedule() {
    FakeClock clock = new FakeClock();
    ScheduledHandle handle =
        syncContext.schedule(task1, 110, TimeUnit.NANOSECONDS, clock.getScheduledExecutorService());
    assertThat(handle.isPending()).isTrue();
    assertThat(clock.runDueTasks()).isEqualTo(0);
    assertThat(clock.forwardNanos(109)).isEqualTo(0);
    assertThat(handle.isPending()).isTrue();
    verify(task1, never()).run();

    assertThat(clock.forwardNanos(1)).isEqualTo(1);
    assertThat(handle.isPending()).isFalse();
    verify(task1).run();
  }

  @Test
  public void scheduleDueImmediately() {
    FakeClock clock = new FakeClock();
    ScheduledHandle handle =
        syncContext.schedule(task1, -1, TimeUnit.NANOSECONDS, clock.getScheduledExecutorService());
    verify(task1, never()).run();
    assertThat(handle.isPending()).isTrue();

    assertThat(clock.runDueTasks()).isEqualTo(1);
    assertThat(handle.isPending()).isFalse();
    verify(task1).run();
  }

  @Test
  public void scheduleHandle_cancel() {
    FakeClock clock = new FakeClock();
    ScheduledHandle handle =
        syncContext.schedule(task1, 110, TimeUnit.NANOSECONDS, clock.getScheduledExecutorService());
    assertThat(handle.isPending()).isTrue();
    assertThat(clock.runDueTasks()).isEqualTo(0);
    assertThat(handle.isPending()).isTrue();

    handle.cancel();
    assertThat(handle.isPending()).isFalse();
    syncContext.drain();
    assertThat(clock.numPendingTasks()).isEqualTo(0);
    verify(task1, never()).run();
  }

  // Test that a scheduled task is cancelled after the timer has expired on the
  // ScheduledExecutorService, but before the task is run.
  @Test
  public void scheduledHandle_cancelRacesWithTimerExpiration() throws Exception {
    FakeClock clock = new FakeClock();

    final CountDownLatch task1Running = new CountDownLatch(1);
    final LinkedBlockingQueue<ScheduledHandle> task2HandleQueue = new LinkedBlockingQueue<>();
    final AtomicBoolean task1Done = new AtomicBoolean();
    final CountDownLatch sideThreadDone = new CountDownLatch(1);

    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) {
          task1Running.countDown();
          try {
            ScheduledHandle task2Handle;
            assertThat(task2Handle = task2HandleQueue.poll(5, TimeUnit.SECONDS)).isNotNull();
            task2Handle.cancel();
          } catch (InterruptedException e) {
            throw new RuntimeException(e);
          }
          task1Done.set(true);
          return null;
        }
      }).when(task1).run();

    Thread sideThread = new Thread() {
        @Override
        public void run() {
          syncContext.execute(task1);
          sideThreadDone.countDown();
        }
      };

    ScheduledHandle handle =
        syncContext.schedule(task2, 10, TimeUnit.NANOSECONDS, clock.getScheduledExecutorService());
    // This will execute and block in task1
    sideThread.start();

    // Make sure task1 is running and blocking the execution
    assertThat(task1Running.await(5, TimeUnit.SECONDS)).isTrue();

    // Timer expires. task2 will be enqueued, but blocked by task1
    assertThat(clock.forwardNanos(10)).isEqualTo(1);
    assertThat(clock.numPendingTasks()).isEqualTo(0);
    assertThat(handle.isPending()).isTrue();

    // Enqueue task3 following task2
    syncContext.executeLater(task3);

    // Let task1 proceed and cancel task2
    task2HandleQueue.add(handle);

    // Wait until sideThread is done, which would have finished task1 and task3, while skipping
    // task2.
    assertThat(sideThreadDone.await(5, TimeUnit.SECONDS)).isTrue();
    assertThat(task1Done.get()).isTrue();
    assertThat(handle.isPending()).isFalse();
    verify(task2, never()).run();
    verify(task3).run();
    assertThat(clock.numPendingTasks()).isEqualTo(0);
  }
}
