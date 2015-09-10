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
  public void blockingUnary() throws Exception {
    ClientCalls.blockingUnaryCall(
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
