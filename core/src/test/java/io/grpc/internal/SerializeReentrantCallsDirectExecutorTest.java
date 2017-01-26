/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import static java.util.Arrays.asList;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class SerializeReentrantCallsDirectExecutorTest {

  SerializeReentrantCallsDirectExecutor executor;

  @Before
  public void setup() {
    executor = new SerializeReentrantCallsDirectExecutor();
  }

  @Test public void reentrantCallsShouldBeSerialized() {
    final List<Integer> callOrder = new ArrayList<Integer>(4);
    executor.execute(new Runnable() {
      @Override
      public void run() {
        executor.execute(new Runnable() {
          @Override
          public void run() {
            executor.execute(new Runnable() {
              @Override public void run() {
                callOrder.add(3);
              }
            });

            callOrder.add(2);

            executor.execute(new Runnable() {
              @Override public void run() {
                callOrder.add(4);
              }
            });
          }
        });
        callOrder.add(1);
      }
    });

    assertEquals(asList(1, 2, 3, 4), callOrder);
  }

  @Test
  public void exceptionShouldNotCancelQueuedTasks() {
    final AtomicBoolean executed1 = new AtomicBoolean();
    final AtomicBoolean executed2 = new AtomicBoolean();
    executor.execute(new Runnable() {
      @Override
      public void run() {
        executor.execute(new Runnable() {
          @Override
          public void run() {
            executed1.set(true);
            throw new RuntimeException("Two");
          }
        });
        executor.execute(new Runnable() {
          @Override
          public void run() {
            executed2.set(true);
          }
        });

        throw new RuntimeException("One");
      }
    });

    assertTrue(executed1.get());
    assertTrue(executed2.get());
  }

  @Test(expected = NullPointerException.class)
  public void executingNullShouldFail() {
    executor.execute(null);
  }

  @Test
  public void executeCanBeRepeated() {
    final List<Integer> executes = new ArrayList<Integer>();
    executor.execute(new Runnable() {
      @Override
      public void run() {
        executes.add(1);
      }
    });
    executor.execute(new Runnable() {
      @Override
      public void run() {
        executes.add(2);
      }
    });

    assertEquals(asList(1,2), executes);
  }

  @Test
  public void interruptDoesNotAffectExecution() {
    final AtomicInteger callsExecuted = new AtomicInteger();

    Thread.currentThread().interrupt();

    executor.execute(new Runnable() {
      @Override
      public void run() {
        Thread.currentThread().interrupt();
        callsExecuted.incrementAndGet();
      }
    });

    executor.execute(new Runnable() {
      @Override
      public void run() {
        callsExecuted.incrementAndGet();
      }
    });

    // clear interrupted flag
    Thread.interrupted();
  }
}
