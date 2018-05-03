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

package io.grpc.netty;

import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.netty.WriteQueue.QueuedCommand;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelPromise;
import io.netty.channel.EventLoop;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.Timeout;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

@RunWith(JUnit4.class)
public class WriteQueueTest {

  @Rule
  public final Timeout globalTimeout = Timeout.seconds(10);

  private final Object lock = new Object();

  @Mock
  public Channel channel;

  @Mock
  public ChannelPromise promise;

  private long writeCalledNanos;
  private long flushCalledNanos = writeCalledNanos;

  /**
   * Set up for test.
   */
  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    when(channel.newPromise()).thenReturn(promise);

    EventLoop eventLoop = Mockito.mock(EventLoop.class);
    doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        Runnable r = (Runnable) invocation.getArguments()[0];
        r.run();
        return null;
      }
    }).when(eventLoop).execute(any(Runnable.class));
    when(eventLoop.inEventLoop()).thenReturn(true);
    when(channel.eventLoop()).thenReturn(eventLoop);

    when(channel.flush()).thenAnswer(new Answer<Channel>() {
      @Override
      public Channel answer(InvocationOnMock invocation) throws Throwable {
        synchronized (lock) {
          flushCalledNanos = System.nanoTime();
          if (flushCalledNanos == writeCalledNanos) {
            flushCalledNanos += 1;
          }
        }
        return channel;
      }
    });

    when(channel.write(any(QueuedCommand.class), eq(promise))).thenAnswer(
        new Answer<ChannelFuture>() {
          @Override
          public ChannelFuture answer(InvocationOnMock invocation) throws Throwable {
            synchronized (lock) {
              writeCalledNanos = System.nanoTime();
              if (writeCalledNanos == flushCalledNanos) {
                writeCalledNanos += 1;
              }
            }
            return promise;
          }
        });
  }

  @Test
  public void singleWriteShouldWork() {
    WriteQueue queue = new WriteQueue(channel);
    queue.enqueue(new CuteCommand(), true);

    verify(channel).write(isA(QueuedCommand.class), eq(promise));
    verify(channel).flush();
  }

  @Test
  public void multipleWritesShouldBeBatched() {
    WriteQueue queue = new WriteQueue(channel);
    for (int i = 0; i < 5; i++) {
      queue.enqueue(new CuteCommand(), false);
    }
    queue.scheduleFlush();

    verify(channel, times(5)).write(isA(QueuedCommand.class), eq(promise));
    verify(channel).flush();
  }

  @Test
  public void maxWritesBeforeFlushShouldBeEnforced() {
    WriteQueue queue = new WriteQueue(channel);
    int writes = WriteQueue.DEQUE_CHUNK_SIZE + 10;
    for (int i = 0; i < writes; i++) {
      queue.enqueue(new CuteCommand(), false);
    }
    queue.scheduleFlush();

    verify(channel, times(writes)).write(isA(QueuedCommand.class), eq(promise));
    verify(channel, times(2)).flush();
  }

  @Test
  public void concurrentWriteAndFlush() throws Throwable {
    final WriteQueue queue = new WriteQueue(channel);
    final CountDownLatch flusherStarted = new CountDownLatch(1);
    final AtomicBoolean doneWriting = new AtomicBoolean();
    Thread flusher = new Thread(new Runnable() {
      @Override
      public void run() {
        flusherStarted.countDown();
        while (!doneWriting.get()) {
          queue.scheduleFlush();
          assertFlushCalledAfterWrites();
        }
        // No more writes, so this flush should drain all writes from the queue
        queue.scheduleFlush();
        assertFlushCalledAfterWrites();
      }

      void assertFlushCalledAfterWrites() {
        synchronized (lock) {
          if (flushCalledNanos - writeCalledNanos <= 0) {
            fail("flush must be called after all writes");
          }
        }
      }
    });

    class ExceptionHandler implements Thread.UncaughtExceptionHandler {
      private Throwable throwable;

      @Override
      public void uncaughtException(Thread t, Throwable e) {
        throwable = e;
      }

      void checkException() throws Throwable {
        if (throwable != null) {
          throw throwable;
        }
      }
    }

    ExceptionHandler exHandler = new ExceptionHandler();
    flusher.setUncaughtExceptionHandler(exHandler);

    flusher.start();
    flusherStarted.await();
    int writes = 10 * WriteQueue.DEQUE_CHUNK_SIZE;
    for (int i = 0; i < writes; i++) {
      queue.enqueue(new CuteCommand(), false);
    }
    doneWriting.set(true);
    flusher.join();

    exHandler.checkException();
    verify(channel, times(writes)).write(isA(CuteCommand.class), eq(promise));
  }

  static class CuteCommand extends WriteQueue.AbstractQueuedCommand {

  }
}
