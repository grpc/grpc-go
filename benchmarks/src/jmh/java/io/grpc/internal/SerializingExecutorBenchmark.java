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

import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Phaser;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.annotations.TearDown;

/**
 * SerializingExecutor benchmark.
 *
 * <p>Since this is a microbenchmark, don't actually believe the numbers in a strict sense. Instead,
 * it is a gauge that the code is behaving roughly as expected, to increase confidence that our
 * understanding of the code is correct (and will behave as expected in other cases). Even more
 * helpfully it pushes the implementation, which should weed out many multithreading bugs.
 */
@State(Scope.Thread)
public class SerializingExecutorBenchmark {

  private ExecutorService executorService;
  private Executor executor = new SerializingExecutor(executorService);

  private static class IncrRunnable implements Runnable {
    int val;

    @Override
    public void run() {
      val++;
    }
  }

  private final IncrRunnable incrRunnable = new IncrRunnable();

  private final Phaser phaser = new Phaser(2);
  private final Runnable phaserRunnable = new Runnable() {
    @Override
    public void run() {
      phaser.arrive();
    }
  };

  @TearDown
  public void tearDown() throws Exception {
    executorService.shutdownNow();
    if (!executorService.awaitTermination(1, TimeUnit.SECONDS)) {
      throw new RuntimeException("executor failed to shut down in a timely fashion");
    }
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void oneRunnableLatency() throws Exception {
    executor.execute(phaserRunnable);
    phaser.arriveAndAwaitAdvance();
  }

  /**
   * Queue many runnables, to better see queuing/consumption cost instead of just context switch.
   */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void manyRunnables() throws Exception {
    incrRunnable.val = 0;
    for (int i = 0; i < 500; i++) {
      executor.execute(incrRunnable);
    }
    executor.execute(phaserRunnable);
    phaser.arriveAndAwaitAdvance();
    if (incrRunnable.val != 500) {
      throw new AssertionError();
    }
  }
}