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

import static io.grpc.benchmarks.Utils.HISTOGRAM_MAX_VALUE;
import static io.grpc.benchmarks.Utils.HISTOGRAM_PRECISION;
import static io.grpc.benchmarks.Utils.saveHistogram;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.ADDRESS;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.CHANNELS;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.CLIENT_PAYLOAD;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.DIRECTEXECUTOR;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.DURATION;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.FLOW_CONTROL_WINDOW;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.OUTSTANDING_RPCS;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.SAVE_HISTOGRAM;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.SERVER_PAYLOAD;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.STREAMING_RPCS;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.TESTCA;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.TLS;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.TRANSPORT;
import static io.grpc.benchmarks.qps.ClientConfiguration.ClientParam.WARMUP_DURATION;

import com.google.common.base.Preconditions;
import com.google.protobuf.ByteString;
import io.grpc.Channel;
import io.grpc.ManagedChannel;
import io.grpc.Status;
import io.grpc.benchmarks.proto.BenchmarkServiceGrpc;
import io.grpc.benchmarks.proto.BenchmarkServiceGrpc.BenchmarkServiceStub;
import io.grpc.benchmarks.proto.Messages.Payload;
import io.grpc.benchmarks.proto.Messages.SimpleRequest;
import io.grpc.benchmarks.proto.Messages.SimpleResponse;
import io.grpc.stub.StreamObserver;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CancellationException;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.HdrHistogram.Histogram;
import org.HdrHistogram.HistogramIterationValue;

/**
 * QPS Client using the non-blocking API.
 */
public class AsyncClient {

  private final ClientConfiguration config;

  public AsyncClient(ClientConfiguration config) {
    this.config = config;
  }

  /**
   * Start the QPS Client.
   */
  public void run() throws Exception {
    if (config == null) {
      return;
    }

    SimpleRequest req = newRequest();

    List<ManagedChannel> channels = new ArrayList<>(config.channels);
    for (int i = 0; i < config.channels; i++) {
      channels.add(config.newChannel());
    }

    // Do a warmup first. It's the same as the actual benchmark, except that
    // we ignore the statistics.
    warmup(req, channels);

    long startTime = System.nanoTime();
    long endTime = startTime + TimeUnit.SECONDS.toNanos(config.duration);
    List<Histogram> histograms = doBenchmark(req, channels, endTime);
    long elapsedTime = System.nanoTime() - startTime;

    Histogram merged = merge(histograms);

    printStats(merged, elapsedTime);
    if (config.histogramFile != null) {
      saveHistogram(merged, config.histogramFile);
    }
    shutdown(channels);
  }

  private SimpleRequest newRequest() {
    ByteString body = ByteString.copyFrom(new byte[config.clientPayload]);
    Payload payload = Payload.newBuilder().setType(config.payloadType).setBody(body).build();

    return SimpleRequest.newBuilder()
            .setResponseType(config.payloadType)
            .setResponseSize(config.serverPayload)
            .setPayload(payload)
            .build();
  }

  private void warmup(SimpleRequest req, List<? extends Channel> channels) throws Exception {
    long endTime = System.nanoTime() + TimeUnit.SECONDS.toNanos(config.warmupDuration);
    doBenchmark(req, channels, endTime);
    // I don't know if this helps, but it doesn't hurt trying. We sometimes run warmups
    // of several minutes at full load and it would be nice to start the actual benchmark
    // with a clean heap.
    System.gc();
  }

  private List<Histogram> doBenchmark(SimpleRequest req,
                                      List<? extends Channel> channels,
                                      long endTime) throws Exception {
    // Initiate the concurrent calls
    List<Future<Histogram>> futures =
        new ArrayList<>(config.outstandingRpcsPerChannel);
    for (int i = 0; i < config.channels; i++) {
      for (int j = 0; j < config.outstandingRpcsPerChannel; j++) {
        Channel channel = channels.get(i);
        futures.add(doRpcs(channel, req, endTime));
      }
    }
    // Wait for completion
    List<Histogram> histograms = new ArrayList<>(futures.size());
    for (Future<Histogram> future : futures) {
      histograms.add(future.get());
    }
    return histograms;
  }

  private Future<Histogram> doRpcs(Channel channel, SimpleRequest request, long endTime) {
    switch (config.rpcType) {
      case UNARY:
        return doUnaryCalls(channel, request, endTime);
      case STREAMING:
        return doStreamingCalls(channel, request, endTime);
      default:
        throw new IllegalStateException("unsupported rpc type");
    }
  }

  private Future<Histogram> doUnaryCalls(Channel channel, final SimpleRequest request,
                                         final long endTime) {
    final BenchmarkServiceStub stub = BenchmarkServiceGrpc.newStub(channel);
    final Histogram histogram = new Histogram(HISTOGRAM_MAX_VALUE, HISTOGRAM_PRECISION);
    final HistogramFuture future = new HistogramFuture(histogram);

    stub.unaryCall(request, new StreamObserver<SimpleResponse>() {
      long lastCall = System.nanoTime();

      @Override
      public void onNext(SimpleResponse value) {
      }

      @Override
      public void onError(Throwable t) {
        Status status = Status.fromThrowable(t);
        System.err.println("Encountered an error in unaryCall. Status is " + status);
        t.printStackTrace();

        future.cancel(true);
      }

      @Override
      public void onCompleted() {
        long now = System.nanoTime();
        // Record the latencies in microseconds
        histogram.recordValue((now - lastCall) / 1000);
        lastCall = now;

        if (endTime - now > 0) {
          stub.unaryCall(request, this);
        } else {
          future.done();
        }
      }
    });

    return future;
  }

  private static Future<Histogram> doStreamingCalls(Channel channel, final SimpleRequest request,
                                             final long endTime) {
    final BenchmarkServiceStub stub = BenchmarkServiceGrpc.newStub(channel);
    final Histogram histogram = new Histogram(HISTOGRAM_MAX_VALUE, HISTOGRAM_PRECISION);
    final HistogramFuture future = new HistogramFuture(histogram);

    ThisIsAHackStreamObserver responseObserver =
        new ThisIsAHackStreamObserver(request, histogram, future, endTime);

    StreamObserver<SimpleRequest> requestObserver = stub.streamingCall(responseObserver);
    responseObserver.requestObserver = requestObserver;
    requestObserver.onNext(request);
    return future;
  }

  /**
   * This seems necessary as we need to reference the requestObserver in the responseObserver.
   * The alternative would be to use the channel layer directly.
   */
  private static class ThisIsAHackStreamObserver implements StreamObserver<SimpleResponse> {

    final SimpleRequest request;
    final Histogram histogram;
    final HistogramFuture future;
    final long endTime;
    long lastCall = System.nanoTime();

    StreamObserver<SimpleRequest> requestObserver;

    ThisIsAHackStreamObserver(SimpleRequest request,
                              Histogram histogram,
                              HistogramFuture future,
                              long endTime) {
      this.request = request;
      this.histogram = histogram;
      this.future = future;
      this.endTime = endTime;
    }

    @Override
    public void onNext(SimpleResponse value) {
      long now = System.nanoTime();
      // Record the latencies in microseconds
      histogram.recordValue((now - lastCall) / 1000);
      lastCall = now;

      if (endTime - now > 0) {
        requestObserver.onNext(request);
      } else {
        requestObserver.onCompleted();
      }
    }

    @Override
    public void onError(Throwable t) {
      Status status = Status.fromThrowable(t);
      System.err.println("Encountered an error in streamingCall. Status is " + status);
      t.printStackTrace();

      future.cancel(true);
    }

    @Override
    public void onCompleted() {
      future.done();
    }
  }

  private static Histogram merge(List<Histogram> histograms) {
    Histogram merged = new Histogram(HISTOGRAM_MAX_VALUE, HISTOGRAM_PRECISION);
    for (Histogram histogram : histograms) {
      for (HistogramIterationValue value : histogram.allValues()) {
        long latency = value.getValueIteratedTo();
        long count = value.getCountAtValueIteratedTo();
        merged.recordValueWithCount(latency, count);
      }
    }
    return merged;
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
    values.append("Channels:                       ").append(config.channels).append('\n')
          .append("Outstanding RPCs per Channel:   ")
          .append(config.outstandingRpcsPerChannel).append('\n')
          .append("Server Payload Size:            ").append(config.serverPayload).append('\n')
          .append("Client Payload Size:            ").append(config.clientPayload).append('\n')
          .append("50%ile Latency (in micros):     ").append(latency50).append('\n')
          .append("90%ile Latency (in micros):     ").append(latency90).append('\n')
          .append("95%ile Latency (in micros):     ").append(latency95).append('\n')
          .append("99%ile Latency (in micros):     ").append(latency99).append('\n')
          .append("99.9%ile Latency (in micros):   ").append(latency999).append('\n')
          .append("Maximum Latency (in micros):    ").append(latencyMax).append('\n')
          .append("QPS:                            ").append(queriesPerSecond).append('\n');
    System.out.println(values);
  }

  private static void shutdown(List<ManagedChannel> channels) {
    for (ManagedChannel channel : channels) {
      channel.shutdown();
    }
  }

  /**
   * checkstyle complains if there is no javadoc comment here.
   */
  public static void main(String... args) throws Exception {
    ClientConfiguration.Builder configBuilder = ClientConfiguration.newBuilder(
        ADDRESS, CHANNELS, OUTSTANDING_RPCS, CLIENT_PAYLOAD, SERVER_PAYLOAD,
        TLS, TESTCA, TRANSPORT, DURATION, WARMUP_DURATION, DIRECTEXECUTOR,
        SAVE_HISTOGRAM, STREAMING_RPCS, FLOW_CONTROL_WINDOW);
    ClientConfiguration config;
    try {
      config = configBuilder.build(args);
    } catch (Exception e) {
      System.out.println(e.getMessage());
      configBuilder.printUsage();
      return;
    }
    AsyncClient client = new AsyncClient(config);
    client.run();
  }

  private static class HistogramFuture implements Future<Histogram> {
    private final Histogram histogram;
    private boolean canceled;
    private boolean done;

    HistogramFuture(Histogram histogram) {
      Preconditions.checkNotNull(histogram, "histogram");
      this.histogram = histogram;
    }

    @Override
    public synchronized boolean cancel(boolean mayInterruptIfRunning) {
      if (!done && !canceled) {
        canceled = true;
        notifyAll();
        return true;
      }
      return false;
    }

    @Override
    public synchronized boolean isCancelled() {
      return canceled;
    }

    @Override
    public synchronized boolean isDone() {
      return done || canceled;
    }

    @Override
    public synchronized Histogram get() throws InterruptedException {
      while (!isDone() && !isCancelled()) {
        wait();
      }

      if (isCancelled()) {
        throw new CancellationException();
      }

      return histogram;
    }

    @Override
    public Histogram get(long timeout, TimeUnit unit) {
      throw new UnsupportedOperationException();
    }

    private synchronized void done() {
      done = true;
      notifyAll();
    }
  }
}
