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

import static grpc.testing.TestServiceGrpc.TestServiceStub;
import static grpc.testing.Qpstest.SimpleRequest;
import static grpc.testing.Qpstest.SimpleResponse;
import static java.lang.Math.max;
import static io.grpc.testing.integration.Util.loadCert;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;

import grpc.testing.Qpstest.PayloadType;
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
import java.io.IOException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.logging.Logger;

/**
 * Runs lots of RPCs against a QPS Server to test for throughput and latency.
 * It's a Java clone of the C version at
 * https://github.com/grpc/grpc/blob/master/test/cpp/qps/client.cc
 */
public class QpsClient {
  private static final Logger log = Logger.getLogger(QpsClient.class.getName());

  // Can record values between 1 ns and 1 min (60 BILLION NS)
  private static final long HISTOGRAM_MAX_VALUE = 60000000000L;
  private static final int HISTOGRAM_PRECISION = 3;
  // How long (in ns) to do RPCs before it counts
  private static final long WARMUP_TIME = 5000000000L;

  private int clientChannels = 4;
  private int clientThreads  = 4;
  private int numRpcs        = 100000;
  private int payloadSize    = 1;
  private String serverHost  = "127.0.0.1";
  private int serverPort;
  private boolean okhttp;
  private boolean enableTls;
  private boolean useTestCa;

  public void run(String[] args) throws Exception {
    if (!parseArgs(args)) {
      return;
    }

    SimpleRequest req = SimpleRequest.newBuilder()
                                     .setResponseType(PayloadType.COMPRESSABLE)
                                     .setResponseSize(payloadSize)
                                     .build();

    List<Channel> channels = new ArrayList<Channel>(clientChannels);
    for (int i = 0; i < clientChannels; i++) {
      channels.add(newChannel());
    }

    long warmupEnd = System.nanoTime() + WARMUP_TIME;
    do {
      doRpcs(channels.get(0), req, 10000).get();
    } while (System.nanoTime() < warmupEnd);

    long startTime = System.nanoTime();

    List<Future<Histogram>> futures = new ArrayList<Future<Histogram>>(clientThreads);
    for (int i = 0; i < clientThreads; i++) {
      // The channel to thread assignment works a bit different than in the C++ version.
      // It's the same for the "interesting cases": clientThreads == clientChannels and
      // clientChannels == 1.
      // It however doesn't support "cache thrashing" as mentioned in the comments of the
      // C++ client. That's because it's my understanding that it doesn't make sense for our API
      // as we neither use fixed threads per call nor do we pin them to specific cores.
      Channel channel = channels.get(i % clientChannels);
      futures.add(doRpcs(channel, req, numRpcs));
    }

    List<Histogram> histograms = new ArrayList<Histogram>(futures.size());
    for (Future<Histogram> future : futures) {
      histograms.add(future.get());
    }

    long elapsedTime = System.nanoTime() - startTime;

    Histogram merged = merge(histograms);

    assert merged.getTotalCount() == numRpcs * clientThreads;

    printStats(merged, elapsedTime);

    // shutdown
    for (Channel channel : channels) {
      ((ChannelImpl) channel).shutdown();
    }
  }

  private Channel newChannel() throws IOException {
    if (okhttp) {
      if (enableTls) {
        throw new IllegalStateException("TLS unsupported with okhttp");
      }

      return OkHttpChannelBuilder.forAddress(serverHost, serverPort)
                                 // TODO(buchgr): Figure out what "server_threads" means in java
                                 .executor(MoreExecutors.newDirectExecutorService())
                                 .build();
    }

    SslContext context = null;
    InetAddress address = InetAddress.getByName(serverHost);
    NegotiationType negotiationType = enableTls ? NegotiationType.TLS : NegotiationType.PLAINTEXT;
    if (enableTls && useTestCa) {
        // Force the hostname to match the cert the server uses.
        address = InetAddress.getByAddress("foo.test.google.fr", address.getAddress());
        File cert = loadCert("ca.pem");
        context = SslContext.newClientContext(cert);
      }

    return NettyChannelBuilder.forAddress(new InetSocketAddress(address, serverPort))
                              .executor(MoreExecutors.newDirectExecutorService())
                              .negotiationType(negotiationType)
                              .sslContext(context)
                              .build();
  }

  private boolean parseArgs(String[] args) {
    try {
      boolean hasServerPort = false;

      for (String arg : args) {
        if (!arg.startsWith("--")) {
          System.err.println("All arguments must start with '--': " + arg);
          printUsage();
          return false;
        }

        String[] pair = arg.substring(2).split("=", 2);
        String key = pair[0];
        String value = "";
        if (pair.length == 2) {
          value = pair[1];
        }

        if ("client_channels".equals(key)) {
          clientChannels = max(Integer.parseInt(value), 1);
        } else if ("client_threads".equals(key)) {
          clientThreads = max(Integer.parseInt(value), 1);
        } else if ("num_rpcs".equals(key)) {
          numRpcs = max(Integer.parseInt(value), 1);
        } else if ("payload_size".equals(key)) {
          payloadSize = max(Integer.parseInt(value), 0);
        } else if ("server_host".equals(key)) {
          serverHost = value;
        } else if ("server_port".equals(key)) {
          serverPort = Integer.parseInt(value);
          hasServerPort = true;
        } else if ("okhttp".equals(key)) {
          okhttp = true;
        } else if ("enable_tls".equals(key)) {
          enableTls = true;
        } else if ("use_testca".equals(key)) {
          useTestCa = true;
        } else {
          System.err.println("Unrecognized argument '" + key + "'.");
        }
      }

      if (!hasServerPort) {
        System.err.println("'--server_port' was not specified.");
        printUsage();
        return false;
      }
    } catch (Exception e) {
      e.printStackTrace();
      printUsage();
      return false;
    }

    return true;
  }

  private void printUsage() {
    QpsClient c = new QpsClient();
    System.out.println(
      "Usage: [ARGS...]"
      + "\n"
      + "\n  --server_port=INT           Port of the server. Required. No default."
      + "\n  --server_host=STR           Hostname of the server. Default " + c.serverHost
      + "\n  --client_channels=INT       Number of client channels. Default " + c.clientChannels
      + "\n  --client_threads=INT        Number of client threads. Default " + c.clientThreads
      + "\n  --num_rpcs=INT              Number of RPCs per thread. Default " + c.numRpcs
      + "\n  --payload_size=INT          Payload size in bytes. Default " + c.payloadSize
      + "\n  --enable_tls                Enable TLS. Default disabled."
      + "\n  --use_testca                Use the provided test certificate for TLS."
    );
  }

  private Future<Histogram> doRpcs(Channel channel,
                                   final SimpleRequest request,
                                   final int numRpcs) {
    final TestServiceStub stub = TestServiceGrpc.newStub(channel);
    final CountDownLatch remainingRpcs = new CountDownLatch(numRpcs);
    final Histogram histogram = new Histogram(HISTOGRAM_MAX_VALUE, HISTOGRAM_PRECISION);
    final HistogramFuture future = new HistogramFuture(histogram, remainingRpcs);

    stub.unaryCall(request, new StreamObserver<SimpleResponse>() {
      long lastCall = System.nanoTime();

      @Override
      public void onValue(SimpleResponse value) {
        PayloadType type = value.getPayload().getType();
        int actualSize = value.getPayload().getBody().size();

        if (!PayloadType.COMPRESSABLE.equals(type)) {
          throw new RuntimeException("type was '" + type + "', expected '" +
                                     PayloadType.COMPRESSABLE + "'.");
        }

        if (payloadSize != actualSize) {
          throw new RuntimeException("size was '" + actualSize + "', expected '" +
                                     payloadSize + "'");
        }
      }

      @Override
      public void onError(Throwable t) {
        Status status = Status.fromThrowable(t);
        System.err.println("onError called: " + status);

        future.cancel(true);
      }

      @Override
      public void onCompleted() {
        long now = System.nanoTime();
        histogram.recordValue(now - lastCall);
        lastCall = now;

        remainingRpcs.countDown();

        if (remainingRpcs.getCount() > 0) {
          stub.unaryCall(request, this);
        }
      }
    });

    return future;
  }

  private Histogram merge(List<Histogram> histograms) {
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
    double percentiles[] = {50, 90, 95, 99, 99.9, 99.99};

    // Generate a comma-separated string of percentiles
    StringBuilder header = new StringBuilder();
    StringBuilder values = new StringBuilder();

    header.append("Threads, Channels, Payload Size, ");
    values.append(String.format("%s, %d, %d, ", clientThreads, clientChannels, payloadSize));

    for (double percentile : percentiles) {
      header.append(percentile).append("%ile").append(", ");
      values.append(histogram.getValueAtPercentile(percentile)).append(", ");
    }

    header.append("QPS");
    values.append((histogram.getTotalCount() * 1000000000L) / elapsedTime);

    System.out.println(header.toString());
    System.out.println(values.toString());
  }

  private static class HistogramFuture implements Future<Histogram> {
    private final Histogram histogram;
    private final CountDownLatch latch;

    private final AtomicBoolean canceled = new AtomicBoolean();

    HistogramFuture(Histogram histogram, CountDownLatch latch) {
      Preconditions.checkNotNull(histogram, "histogram");
      Preconditions.checkNotNull(histogram, "latch");

      this.histogram = histogram;
      this.latch = latch;
    }

    @Override
    public boolean cancel(boolean mayInterruptIfRunning) {
      if (latch.getCount() > 0 && canceled.compareAndSet(false, true)) {
        while (latch.getCount() > 0) {
          latch.countDown();
        }
        return true;
      }
      return false;
    }

    @Override
    public boolean isCancelled() {
      return canceled.get();
    }

    @Override
    public boolean isDone() {
      return latch.getCount() == 0 || canceled.get();
    }

    @Override
    public Histogram get() throws InterruptedException, ExecutionException {
      latch.await();

      if (canceled.get()) {
        throw new CancellationException();
      }

      return histogram;
    }

    @Override
    public Histogram get(long timeout, TimeUnit unit) throws InterruptedException,
                                                             ExecutionException,
                                                             TimeoutException {
      throw new UnsupportedOperationException();
    }
  }
}
