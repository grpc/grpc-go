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

package io.grpc.benchmarks.qps;

import static grpc.testing.Qpstest.SimpleRequest;
import static grpc.testing.Qpstest.SimpleResponse;
import static grpc.testing.TestServiceGrpc.TestServiceStub;
import static io.grpc.testing.integration.Util.loadCert;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.ByteString;

import grpc.testing.Qpstest.Payload;
import grpc.testing.TestServiceGrpc;
import io.grpc.Channel;
import io.grpc.ChannelImpl;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import io.grpc.transport.netty.NegotiationType;
import io.grpc.transport.netty.NettyChannelBuilder;
import io.grpc.transport.okhttp.OkHttpChannelBuilder;
import io.netty.handler.ssl.SslContext;
import org.HdrHistogram.Histogram;
import org.HdrHistogram.HistogramIterationValue;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CancellationException;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * QPS Client using the non-blocking API.
 */
public class QpsAsyncClient {
  // The histogram can record values between 1 microsecond and 1 min.
  private static final long HISTOGRAM_MAX_VALUE = 60000000L;
  // Value quantization will be no larger than 1/10^3 = 0.1%.
  private static final int HISTOGRAM_PRECISION = 3;

  private final ClientConfiguration config;

  public QpsAsyncClient(ClientConfiguration config) {
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

    List<Channel> channels = new ArrayList<Channel>(config.channels);
    for (int i = 0; i < config.channels; i++) {
      channels.add(newChannel());
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
    dumpHistogram(merged);
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

  private void warmup(SimpleRequest req, List<Channel> channels) throws Exception {
    long endTime = System.nanoTime() + TimeUnit.SECONDS.toNanos(config.warmupDuration);
    doBenchmark(req, channels, endTime);
    // I don't know if this helps, but it doesn't hurt trying. We sometimes run warmups
    // of several minutes at full load and it would be nice to start the actual benchmark
    // with a clean heap.
    System.gc();
  }

  private List<Histogram> doBenchmark(SimpleRequest req,
                                      List<Channel> channels, long endTime) throws Exception {
    // Initiate the concurrent calls
    List<Future<Histogram>> futures =
        new ArrayList<Future<Histogram>>(config.outstandingRpcsPerChannel);
    for (int i = 0; i < config.channels; i++) {
      for (int j = 0; j < config.outstandingRpcsPerChannel; j++) {
        Channel channel = channels.get(i);
        futures.add(doRpcs(channel, req, endTime));
      }
    }
    // Wait for completion
    List<Histogram> histograms = new ArrayList<Histogram>(futures.size());
    for (Future<Histogram> future : futures) {
      histograms.add(future.get());
    }
    return histograms;
  }

  private Channel newChannel() throws IOException {
    if (config.okhttp) {
      if (config.tls) {
        throw new IllegalStateException("TLS unsupported with okhttp");
      }
      return OkHttpChannelBuilder
               .forAddress(config.host, config.port)
               .executor(config.directExecutor ? MoreExecutors.newDirectExecutorService() : null)
               .build();
    }
    SslContext context = null;
    InetAddress address = InetAddress.getByName(config.host);
    NegotiationType negotiationType = config.tls ? NegotiationType.TLS : NegotiationType.PLAINTEXT;
    if (config.tls && config.testca) {
      // Force the hostname to match the cert the server uses.
      address = InetAddress.getByAddress("foo.test.google.fr", address.getAddress());
      File cert = loadCert("ca.pem");
      context = SslContext.newClientContext(cert);
    }
    return NettyChannelBuilder
             .forAddress(new InetSocketAddress(address, config.port))
             .negotiationType(negotiationType)
             .executor(config.directExecutor ? MoreExecutors.newDirectExecutorService() : null)
             .sslContext(context)
             .connectionWindowSize(config.connectionWindow)
             .streamWindowSize(config.streamWindow)
             .build();
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
    final TestServiceStub stub = TestServiceGrpc.newStub(channel);
    final Histogram histogram = new Histogram(HISTOGRAM_MAX_VALUE, HISTOGRAM_PRECISION);
    final HistogramFuture future = new HistogramFuture(histogram);

    stub.unaryCall(request, new StreamObserver<SimpleResponse>() {
      long lastCall = System.nanoTime();

      @Override
      public void onValue(SimpleResponse value) {
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

        if (endTime > now) {
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
    final TestServiceStub stub = TestServiceGrpc.newStub(channel);
    final Histogram histogram = new Histogram(HISTOGRAM_MAX_VALUE, HISTOGRAM_PRECISION);
    final HistogramFuture future = new HistogramFuture(histogram);

    ThisIsAHackStreamObserver responseObserver =
        new ThisIsAHackStreamObserver(request, histogram, future, endTime);

    StreamObserver<SimpleRequest> requestObserver = stub.streamingCall(responseObserver);
    responseObserver.requestObserver = requestObserver;
    requestObserver.onValue(request);
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
    public void onValue(SimpleResponse value) {
      long now = System.nanoTime();
      // Record the latencies in microseconds
      histogram.recordValue((now - lastCall) / 1000);
      lastCall = now;

      if (endTime > now) {
        requestObserver.onValue(request);
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
          .append("QPS:                            ").append(queriesPerSecond).append('\n');
    System.out.println(values);
  }

  private void dumpHistogram(Histogram histogram) throws IOException {
    if (config.dumpHistogram) {
      File file;
      PrintStream log = null;
      try {
        file = new File(config.histogramFile);
        if (file.exists()) {
          file.delete();
        }
        log = new PrintStream(new FileOutputStream(file), false);
        histogram.outputPercentileDistribution(log, 1.0);
      } finally {
        if (log != null) {
          log.close();
        }
      }
    }
  }

  private static void shutdown(List<Channel> channels) {
    for (Channel channel : channels) {
      ((ChannelImpl) channel).shutdown();
    }
  }

  /**
   * checkstyle complains if there is no javadoc comment here.
   */
  public static void main(String... args) throws Exception {
    ClientConfiguration config;
    try {
      config = ClientConfiguration.parseArgs(args);
    } catch (RuntimeException e) {
      System.out.println(e.getMessage());
      ClientConfiguration.printUsage();
      return;
    }
    QpsAsyncClient client = new QpsAsyncClient(config);
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
    public synchronized Histogram get() throws InterruptedException, ExecutionException {
      while (!isDone() && !isCancelled()) {
        wait();
      }

      if (isCancelled()) {
        throw new CancellationException();
      }

      done = true;
      return histogram;
    }

    @Override
    public Histogram get(long timeout, TimeUnit unit) throws InterruptedException,
        ExecutionException,
        TimeoutException {
      throw new UnsupportedOperationException();
    }

    private synchronized void done() {
      done = true;
      notifyAll();
    }
  }
}
