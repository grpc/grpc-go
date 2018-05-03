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

package io.grpc.benchmarks.netty;

import io.grpc.CallOptions;
import io.grpc.stub.ClientCalls;
import io.netty.buffer.Unpooled;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.Fork;
import org.openjdk.jmh.annotations.Level;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.annotations.TearDown;

/**
 * Benchmark showing performance of a linear sequence of blocking calls in a single thread which
 * is the worst case for throughput. The benchmark permutes response payload size and
 * client inbound flow-control window size.
 */
@State(Scope.Benchmark)
@Fork(1)
public class SingleThreadBlockingQpsBenchmark extends AbstractBenchmark {

  /**
   * Setup with direct executors, small payloads and the default flow control window.
   */
  @Setup(Level.Trial)
  public void setup() throws Exception {
    super.setup(ExecutorType.DIRECT,
        ExecutorType.DIRECT,
        MessageSize.SMALL,
        MessageSize.SMALL,
        FlowWindowSize.MEDIUM,
        ChannelType.NIO,
        1,
        1);
  }

  /**
   * Stop the server and client channels.
   */
  @Override
  @TearDown(Level.Trial)
  public void teardown() throws Exception {
    Thread.sleep(5000);
    super.teardown();
  }

  /**
   * Issue a unary call and wait for the response.
   */
  @Benchmark
  public Object blockingUnary() throws Exception {
    return ClientCalls.blockingUnaryCall(
        channels[0].newCall(unaryMethod, CallOptions.DEFAULT), Unpooled.EMPTY_BUFFER);
  }

  /**
   * Useful for triggering a subset of the benchmark in a profiler.
   */
  public static void main(String[] argv) throws Exception {
    SingleThreadBlockingQpsBenchmark bench = new SingleThreadBlockingQpsBenchmark();
    bench.setup();
    for  (int i = 0; i < 10000; i++) {
      bench.blockingUnary();
    }
    Thread.sleep(30000);
    bench.teardown();
    System.exit(0);
  }
}
