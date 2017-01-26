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
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

@RunWith(JUnit4.class)
public class WriteQueueTest {

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

  @Test(timeout = 10000)
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
