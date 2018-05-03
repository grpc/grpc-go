/*
 * Copyright 2017 The gRPC Authors
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

import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
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

  private ExecutorService executorService = Executors.newSingleThreadExecutor();
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
