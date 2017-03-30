/*
 * Copyright 2017, Google Inc. All rights reserved.
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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.fail;

import com.google.common.util.concurrent.MoreExecutors;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.Executor;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class SerializingExecutorTest {
  private SingleExecutor singleExecutor = new SingleExecutor();
  private SerializingExecutor executor = new SerializingExecutor(singleExecutor);
  private List<Integer> runs = new ArrayList<Integer>();

  private class AddToRuns implements Runnable {
    private final int val;

    public AddToRuns(int val) {
      this.val = val;
    }

    @Override
    public void run() {
      runs.add(val);
    }
  }

  @Test
  public void resumable() {
    class CoyExecutor implements Executor {
      int runCount;

      @Override
      public void execute(Runnable command) {
        runCount++;
        if (runCount == 1) {
          throw new RuntimeException();
        }
        command.run();
      }
    }

    executor = new SerializingExecutor(new CoyExecutor());
    try {
      executor.execute(new AddToRuns(1));
      fail();
    } catch (RuntimeException expected) {
    }

    // Ensure that the runnable enqueued was actually removed on the failed execute above.
    executor.execute(new AddToRuns(2));

    assertThat(runs).containsExactly(2);
  }


  @Test
  public void serial() {
    executor.execute(new AddToRuns(1));
    assertEquals(Collections.<Integer>emptyList(), runs);
    singleExecutor.drain();
    assertEquals(Arrays.asList(1), runs);

    executor.execute(new AddToRuns(2));
    assertEquals(Arrays.asList(1), runs);
    singleExecutor.drain();
    assertEquals(Arrays.asList(1, 2), runs);
  }

  @Test
  public void parallel() {
    executor.execute(new AddToRuns(1));
    executor.execute(new AddToRuns(2));
    executor.execute(new AddToRuns(3));
    assertEquals(Collections.<Integer>emptyList(), runs);
    singleExecutor.drain();
    assertEquals(Arrays.asList(1, 2, 3), runs);
  }

  @Test
  public void reentrant() {
    executor.execute(new Runnable() {
      @Override
      public void run() {
        executor.execute(new AddToRuns(3));
        runs.add(1);
      }
    });
    executor.execute(new AddToRuns(2));
    singleExecutor.drain();
    assertEquals(Arrays.asList(1, 2, 3), runs);
  }

  @Test
  public void testFirstRunnableThrows() {
    final RuntimeException ex = new RuntimeException();
    executor.execute(new Runnable() {
      @Override
      public void run() {
        runs.add(1);
        throw ex;
      }
    });
    executor.execute(new AddToRuns(2));
    executor.execute(new AddToRuns(3));

    singleExecutor.drain();

    assertEquals(Arrays.asList(1, 2, 3), runs);
  }

  @Test
  public void lastRunnableThrows() {
    final RuntimeException ex = new RuntimeException();
    executor.execute(new AddToRuns(1));
    executor.execute(new AddToRuns(2));
    executor.execute(new Runnable() {
      @Override
      public void run() {
        runs.add(3);
        throw ex;
      }
    });

    singleExecutor.drain();
    assertEquals(Arrays.asList(1, 2, 3), runs);

    // Scheduling more still works
    executor.execute(new AddToRuns(4));
    assertEquals(Arrays.asList(1, 2, 3), runs);
    singleExecutor.drain();
    assertEquals(Arrays.asList(1, 2, 3, 4), runs);
  }

  @Test
  public void firstExecuteThrows() {
    final RuntimeException ex = new RuntimeException();
    ForwardingExecutor forwardingExecutor = new ForwardingExecutor(new Executor() {
      @Override
      public void execute(Runnable r) {
        throw ex;
      }
    });
    executor = new SerializingExecutor(forwardingExecutor);
    try {
      executor.execute(new AddToRuns(1));
      fail("expected exception");
    } catch (RuntimeException e) {
      assertSame(ex, e);
    }
    assertEquals(Collections.<Integer>emptyList(), runs);

    forwardingExecutor.executor = singleExecutor;
    executor.execute(new AddToRuns(2));
    executor.execute(new AddToRuns(3));
    assertEquals(Collections.<Integer>emptyList(), runs);
    singleExecutor.drain();
    assertEquals(Arrays.asList(2, 3), runs);
  }

  @Test
  public void direct() {
    executor = new SerializingExecutor(MoreExecutors.directExecutor());
    executor.execute(new AddToRuns(1));
    assertEquals(Arrays.asList(1), runs);
    executor.execute(new AddToRuns(2));
    assertEquals(Arrays.asList(1, 2), runs);
    executor.execute(new AddToRuns(3));
    assertEquals(Arrays.asList(1, 2, 3), runs);
  }

  @Test
  public void testDirectReentrant() {
    executor = new SerializingExecutor(MoreExecutors.directExecutor());
    executor.execute(new Runnable() {
      @Override
      public void run() {
        executor.execute(new AddToRuns(2));
        runs.add(1);
      }
    });
    assertEquals(Arrays.asList(1, 2), runs);
    executor.execute(new AddToRuns(3));
    assertEquals(Arrays.asList(1, 2, 3), runs);
  }

  private static class SingleExecutor implements Executor {
    private Runnable runnable;

    @Override
    public void execute(Runnable r) {
      if (runnable != null) {
        fail("Already have runnable scheduled");
      }
      runnable = r;
    }

    public void drain() {
      if (runnable != null) {
        Runnable r = runnable;
        runnable = null;
        r.run();
      }
    }
  }

  private static class ForwardingExecutor implements Executor {
    Executor executor;

    public ForwardingExecutor(Executor executor) {
      this.executor = executor;
    }

    @Override
    public void execute(Runnable r) {
      executor.execute(r);
    }
  }
}
