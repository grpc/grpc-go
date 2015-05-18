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
 * Benchmark measuring messages per second received from a streaming server. The server
 * is obeying outbound flow-control.
 */
@State(Scope.Benchmark)
@Fork(1)
public class FlowControlledMessagesPerSecondBenchmark extends AbstractBenchmark {

  @Param({"1", "2", "4"})
  public int channelCount = 1;

  @Param({"1", "2", "10", "100"})
  public int maxConcurrentStreams = 1;

  @Param
  public ExecutorType clientExecutor = ExecutorType.DIRECT;

  @Param({"SMALL"})
  public PayloadSize responseSize = PayloadSize.SMALL;

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

    public long messagesPerSecond() {
      return callCounter.get();
    }
  }

  /**
   * Setup with direct executors, small payloads and the default flow-control window.
   */
  @Setup(Level.Trial)
  public void setup() throws Exception {
    super.setup(clientExecutor,
        ExecutorType.DIRECT,
        PayloadSize.SMALL,
        responseSize,
        FlowWindowSize.MEDIUM,
        ChannelType.NIO,
        maxConcurrentStreams,
        channelCount);
    callCounter = new AtomicLong();
    completed = new AtomicBoolean();
    startFlowControlledStreamingCalls(maxConcurrentStreams, callCounter, completed, 1);
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
   * Measure the rate of messages received. The calls are already running, we just observe a counter
   * of received responses.
   */
  @Benchmark
  public void stream(AdditionalCounters counters) throws Exception {
    // No need to do anything, just sleep here.
    Thread.sleep(1001);
  }

  /**
   * Useful for triggering a subset of the benchmark in a profiler.
   */
  public static void main(String[] argv) throws Exception {
    FlowControlledMessagesPerSecondBenchmark bench = new FlowControlledMessagesPerSecondBenchmark();
    bench.setup();
    Thread.sleep(30000);
    bench.teardown();
    System.exit(0);
  }
}
