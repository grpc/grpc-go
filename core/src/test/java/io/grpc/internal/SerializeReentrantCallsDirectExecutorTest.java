/*
 * Copyright 2015 The gRPC Authors
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
    final List<Integer> callOrder = new ArrayList<>(4);
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
    final List<Integer> executes = new ArrayList<>();
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
