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

import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.inOrder;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
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
 * Unit tests for {@link ChannelExecutor}.
 */
@RunWith(JUnit4.class)
public class ChannelExecutorTest {
  private final ChannelExecutor executor = new ChannelExecutor();

  @Mock
  private Runnable task1;

  @Mock
  private Runnable task2;

  @Mock
  private Runnable task3;

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  @Test
  public void singleThread() {
    executor.executeLater(task1);
    executor.executeLater(task2);
    InOrder inOrder = inOrder(task1, task2, task3);
    inOrder.verifyNoMoreInteractions();
    executor.drain();
    inOrder.verify(task1).run();
    inOrder.verify(task2).run();

    executor.executeLater(task3);
    inOrder.verifyNoMoreInteractions();
    executor.drain();
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
          executor.executeLater(task1);
          task1Added.countDown();
          executor.drain();
          sideThreadDone.countDown();
        }
      };
    sideThread.start();

    assertTrue(task1Added.await(5, TimeUnit.SECONDS));
    executor.executeLater(task2);
    assertTrue(task1Running.await(5, TimeUnit.SECONDS));
    // This will do nothing because task1 is running until task1Proceed is set
    executor.drain();

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
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) {
          throw new RuntimeException("Simulated");
        }
      }).when(task2).run();
    executor.executeLater(task1);
    executor.executeLater(task2);
    executor.executeLater(task3);
    executor.drain();
    inOrder.verify(task1).run();
    inOrder.verify(task2).run();
    inOrder.verify(task3).run();
  }
}
