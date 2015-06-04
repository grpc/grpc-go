package io.grpc.benchmarks.netty;

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
        PayloadSize.SMALL,
        PayloadSize.SMALL,
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
    ClientCalls.blockingUnaryCall(channels[0].newCall(unaryMethod), Unpooled.EMPTY_BUFFER);
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
