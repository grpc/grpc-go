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

package io.grpc.benchmarks.qps;

import static com.google.common.base.Preconditions.checkNotNull;
import static io.grpc.benchmarks.Utils.HISTOGRAM_MAX_VALUE;
import static io.grpc.benchmarks.Utils.HISTOGRAM_PRECISION;
import static io.grpc.benchmarks.Utils.saveHistogram;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.ADDRESS;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.CLIENT_PAYLOAD;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.DURATION;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.FLOW_CONTROL_WINDOW;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.SAVE_HISTOGRAM;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.SERVER_PAYLOAD;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.TARGET_QPS;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.TESTCA;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.TLS;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.TRANSPORT;

import io.grpc.Channel;
import io.grpc.ManagedChannel;
import io.grpc.Status;
import io.grpc.benchmarks.proto.BenchmarkServiceGrpc;
import io.grpc.benchmarks.proto.Messages.SimpleRequest;
import io.grpc.benchmarks.proto.Messages.SimpleResponse;
import io.grpc.stub.StreamObserver;
import java.util.Random;
import java.util.concurrent.Callable;
import org.HdrHistogram.AtomicHistogram;
import org.HdrHistogram.Histogram;

/**
 * Tries to generate traffic that closely resembles user-generated RPC traffic. This is done using
 * a Poisson Process to average at a target QPS and the delays between calls are randomized using
 * an exponential variate.
 *
 * @see <a href="http://en.wikipedia.org/wiki/Poisson_process">Poisson Process</a>
 * @see <a href="http://en.wikipedia.org/wiki/Exponential_distribution">Exponential Distribution</a>
 */
public class OpenLoopClient {

  private final ClientConfiguration config;

  public OpenLoopClient(ClientConfiguration config) {
    this.config = config;
  }

  /**
   * Comment for checkstyle.
   */
  public static void main(String... args) throws Exception {
    ClientConfiguration.Builder configBuilder = ClientConfiguration.newBuilder(
        ADDRESS, TARGET_QPS, CLIENT_PAYLOAD, SERVER_PAYLOAD, TLS,
        TESTCA, TRANSPORT, DURATION, SAVE_HISTOGRAM, FLOW_CONTROL_WINDOW);
    ClientConfiguration config;
    try {
      config = configBuilder.build(args);
    } catch (Exception e) {
      System.out.println(e.getMessage());
      configBuilder.printUsage();
      return;
    }
    OpenLoopClient client = new OpenLoopClient(config);
    client.run();
  }

  /**
   * Start the open loop client.
   */
  public void run() throws Exception {
    if (config == null) {
      return;
    }
    config.channels = 1;
    config.directExecutor = true;
    ManagedChannel ch = config.newChannel();
    SimpleRequest req = config.newRequest();
    LoadGenerationWorker worker =
        new LoadGenerationWorker(ch, req, config.targetQps, config.duration);
    final long start = System.nanoTime();
    Histogram histogram = worker.call();
    final long end = System.nanoTime();
    printStats(histogram, end - start);
    if (config.histogramFile != null) {
      saveHistogram(histogram, config.histogramFile);
    }
    ch.shutdown();
  }

  private void printStats(Histogram histogram, long elapsedTime) {
    long latency50 = histogram.getValueAtPercentile(50);
    long latency90 = histogram.getValueAtPercentile(90);
    long latency95 = histogram.getValueAtPercentile(95);
    long latency99 = histogram.getValueAtPercentile(99);
    long latency999 = histogram.getValueAtPercentile(99.9);
    long latencyMax = histogram.getValueAtPercentile(100);
    long queriesPerSecond = histogram.getTotalCount() * 1000000000L / elapsedTime;

    StringBuilder values = new StringBuilder();
    values.append("Server Payload Size:            ").append(config.serverPayload).append('\n')
          .append("Client Payload Size:            ").append(config.clientPayload).append('\n')
          .append("50%ile Latency (in micros):     ").append(latency50).append('\n')
          .append("90%ile Latency (in micros):     ").append(latency90).append('\n')
          .append("95%ile Latency (in micros):     ").append(latency95).append('\n')
          .append("99%ile Latency (in micros):     ").append(latency99).append('\n')
          .append("99.9%ile Latency (in micros):   ").append(latency999).append('\n')
          .append("Maximum Latency (in micros):    ").append(latencyMax).append('\n')
          .append("Actual QPS:                     ").append(queriesPerSecond).append('\n')
          .append("Target QPS:                     ").append(config.targetQps).append('\n');
    System.out.println(values);
  }

  static class LoadGenerationWorker implements Callable<Histogram> {
    final Histogram histogram = new AtomicHistogram(HISTOGRAM_MAX_VALUE, HISTOGRAM_PRECISION);
    final BenchmarkServiceGrpc.BenchmarkServiceStub stub;
    final SimpleRequest request;
    final Random rnd;
    final int targetQps;
    final long numRpcs;

    LoadGenerationWorker(Channel channel, SimpleRequest request, int targetQps, int duration) {
      stub = BenchmarkServiceGrpc.newStub(checkNotNull(channel, "channel"));
      this.request = checkNotNull(request, "request");
      this.targetQps = targetQps;
      numRpcs = (long) targetQps * duration;
      rnd = new Random();
    }

    /**
     * Discuss waiting strategy between calls. Sleeping seems to be very inaccurate
     * (see below). On the other hand calling System.nanoTime() a lot (especially from
     * different threads seems to impact its accuracy
     * http://shipilev.net/blog/2014/nanotrusting-nanotime/
     * On my system the overhead of LockSupport.park(long) seems to average at ~55 micros.
     * // Try to sleep for 1 nanosecond and measure how long it actually takes.
     * long start = System.nanoTime();
     * int i = 0;
     * while (i < 10000) {
     *   LockSupport.parkNanos(1);
     *   i++;
     * }
     * long end = System.nanoTime();
     * System.out.println((end - start) / 10000);
     */
    @Override
    public Histogram call() throws Exception {
      long now = System.nanoTime();
      long nextRpc = now;
      long i = 0;
      while (i < numRpcs) {
        now = System.nanoTime();
        if (nextRpc - now <= 0) {
          // TODO: Add option to print how far we have been off from the target delay in micros.
          nextRpc += nextDelay(targetQps);
          newRpc(stub);
          i++;
        }
      }

      waitForRpcsToComplete(1);

      return histogram;
    }

    private void newRpc(BenchmarkServiceGrpc.BenchmarkServiceStub stub) {
      stub.unaryCall(request, new StreamObserver<SimpleResponse>() {

        private final long start = System.nanoTime();

        @Override
        public void onNext(SimpleResponse value) {
        }

        @Override
        public void onError(Throwable t) {
          Status status = Status.fromThrowable(t);
          System.err.println("Encountered an error in unaryCall. Status is " + status);
          t.printStackTrace();
        }

        @Override
        public void onCompleted() {
          final long end = System.nanoTime();
          histogram.recordValue((end - start) / 1000);
        }
      });
    }

    private void waitForRpcsToComplete(int duration) {
      long now = System.nanoTime();
      long end = now + duration * 1000 * 1000 * 1000;
      while (histogram.getTotalCount() < numRpcs && end - now > 0) {
        now = System.nanoTime();
      }
    }

    private static final double DELAY_EPSILON = Math.nextUp(0d);

    private long nextDelay(int targetQps) {
      double seconds = -Math.log(Math.max(rnd.nextDouble(), DELAY_EPSILON)) / targetQps;
      double nanos = seconds * 1000 * 1000 * 1000;
      return Math.round(nanos);
    }
  }
}
