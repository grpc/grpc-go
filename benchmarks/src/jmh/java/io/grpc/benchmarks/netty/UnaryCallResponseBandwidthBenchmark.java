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

import org.openjdk.jmh.annotations.AuxCounters;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.Fork;
import org.openjdk.jmh.annotations.Level;
import org.openjdk.jmh.annotations.Param;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.annotations.TearDown;

import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Benchmark intended to test response bandwidth in bytes/sec for unary calls  by permuting
 * payload size and flow-control windows with number of concurrent calls. Async stubs are used
 * to avoid context-switching overheads.
 */
@State(Scope.Benchmark)
@Fork(1)
public class UnaryCallResponseBandwidthBenchmark extends AbstractBenchmark {

  @Param({"1", "10"})
  public int maxConcurrentStreams = 1;

  @Param({"LARGE", "JUMBO"})
  public MessageSize responseSize = MessageSize.JUMBO;

  @Param({"MEDIUM", "LARGE", "JUMBO"})
  public FlowWindowSize clientInboundFlowWindow = FlowWindowSize.MEDIUM;

  private static AtomicLong callCounter;
  private AtomicBoolean completed;

  /**
   * Use an AuxCounter so we can measure that calls as they occur without consuming CPU
   * in the benchmark method.
   */
  @AuxCounters
  @State(Scope.Thread)
  public static class AdditionalCounters {

    @Setup(Level.Iteration)
    public void clean() {
      callCounter.set(0);
    }

    public long megabitsPerSecond() {
      // Convert bytes into megabits
      return (callCounter.get() * 8) >> 20;
    }
  }

  /**
   * Setup with direct executors, small payloads and a large flow control window.
   */
  @Setup(Level.Trial)
  public void setup() throws Exception {
    super.setup(ExecutorType.DIRECT,
        ExecutorType.DIRECT,
        MessageSize.SMALL,
        responseSize,
        clientInboundFlowWindow,
        ChannelType.NIO,
        maxConcurrentStreams,
        1);
    callCounter = new AtomicLong();
    completed = new AtomicBoolean();
    startUnaryCalls(maxConcurrentStreams, callCounter, completed, responseSize.bytes());
  }

  /**
   * Stop the running calls then stop the server and client channels.
   */
  @Override
  @TearDown(Level.Trial)
  public void teardown() throws Exception {
    completed.set(true);
    Thread.sleep(5000);
    super.teardown();
  }

  /**
   * Measure bandwidth of unary call responses. The calls are already running, we just observe a
   * counter of received responses.
   */
  @Benchmark
  public void unary(AdditionalCounters counters) throws Exception {
    // No need to do anything, just sleep here.
    Thread.sleep(1001);
  }

  /**
   * Useful for triggering a subset of the benchmark in a profiler.
   */
  public static void main(String[] argv) throws Exception {
    UnaryCallResponseBandwidthBenchmark bench = new UnaryCallResponseBandwidthBenchmark();
    bench.setup();
    Thread.sleep(30000);
    bench.teardown();
    System.exit(0);
  }
}
