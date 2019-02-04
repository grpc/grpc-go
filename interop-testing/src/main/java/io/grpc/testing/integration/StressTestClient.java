/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.testing.integration;

import static java.util.Arrays.asList;
import static java.util.Collections.shuffle;
import static java.util.Collections.singletonList;
import static java.util.concurrent.Executors.newFixedThreadPool;
import static java.util.concurrent.TimeUnit.SECONDS;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Joiner;
import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import com.google.common.base.Splitter;
import com.google.common.collect.Iterators;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.ListeningExecutorService;
import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.internal.testing.TestUtils;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.stub.StreamObserver;
import io.netty.handler.ssl.SslContext;
import java.io.IOException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.URI;
import java.net.URISyntaxException;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A stress test client following the
 * <a href="https://github.com/grpc/grpc/blob/master/tools/run_tests/stress_test/STRESS_CLIENT_SPEC.md">
 * specifications</a> of the gRPC stress testing framework.
 */
public class StressTestClient {

  private static final Logger log = Logger.getLogger(StressTestClient.class.getName());

  /**
   * The main application allowing this client to be launched from the command line.
   */
  public static void main(String... args) throws Exception {
    final StressTestClient client = new StressTestClient();
    client.parseArgs(args);

    // Attempt an orderly shutdown, if the JVM is shutdown via a signal.
    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        client.shutdown();
      }
    });

    try {
      client.startMetricsService();
      client.runStressTest();
      client.blockUntilStressTestComplete();
    } catch (Exception e) {
      log.log(Level.WARNING, "The stress test client encountered an error!", e);
    } finally {
      client.shutdown();
    }
  }

  private static final int WORKER_GRACE_PERIOD_SECS = 30;

  private List<InetSocketAddress> addresses =
      singletonList(new InetSocketAddress("localhost", 8080));
  private List<TestCaseWeightPair> testCaseWeightPairs = new ArrayList<>();

  private String serverHostOverride;
  private boolean useTls = false;
  private boolean useTestCa = false;
  private int durationSecs = -1;
  private int channelsPerServer = 1;
  private int stubsPerChannel = 1;
  private int metricsPort = 8081;

  private Server metricsServer;
  private final Map<String, Metrics.GaugeResponse> gauges =
      new ConcurrentHashMap<>();

  private volatile boolean shutdown;

  /**
   * List of futures that {@link #blockUntilStressTestComplete()} waits for.
   */
  private final List<ListenableFuture<?>> workerFutures =
      new ArrayList<>();
  private final List<ManagedChannel> channels = new ArrayList<>();
  private ListeningExecutorService threadpool;

  @VisibleForTesting
  void parseArgs(String[] args) {
    boolean usage = false;
    String serverAddresses = "";
    for (String arg : args) {
      if (!arg.startsWith("--")) {
        System.err.println("All arguments must start with '--': " + arg);
        usage = true;
        break;
      }
      String[] parts = arg.substring(2).split("=", 2);
      String key = parts[0];
      if ("help".equals(key)) {
        usage = true;
        break;
      }
      if (parts.length != 2) {
        System.err.println("All arguments must be of the form --arg=value");
        usage = true;
        break;
      }
      String value = parts[1];
      if ("server_addresses".equals(key)) {
        // May need to apply server host overrides to the addresses, so delay processing
        serverAddresses = value;
      } else if ("server_host_override".equals(key)) {
        serverHostOverride = value;
      } else if ("use_tls".equals(key)) {
        useTls = Boolean.parseBoolean(value);
      } else if ("use_test_ca".equals(key)) {
        useTestCa = Boolean.parseBoolean(value);
      } else if ("test_cases".equals(key)) {
        testCaseWeightPairs = parseTestCases(value);
      } else if ("test_duration_secs".equals(key)) {
        durationSecs = Integer.valueOf(value);
      } else if ("num_channels_per_server".equals(key)) {
        channelsPerServer = Integer.valueOf(value);
      } else if ("num_stubs_per_channel".equals(key)) {
        stubsPerChannel = Integer.valueOf(value);
      } else if ("metrics_port".equals(key)) {
        metricsPort = Integer.valueOf(value);
      } else {
        System.err.println("Unknown argument: " + key);
        usage = true;
        break;
      }
    }

    if (!usage && !serverAddresses.isEmpty()) {
      addresses = parseServerAddresses(serverAddresses);
      usage = addresses.isEmpty();
    }

    if (usage) {
      StressTestClient c = new StressTestClient();
      System.err.println(
          "Usage: [ARGS...]"
              + "\n"
              + "\n  --server_host_override=HOST    Claimed identification expected of server."
              + "\n                                 Defaults to server host"
              + "\n  --server_addresses=<name_1>:<port_1>,<name_2>:<port_2>...<name_N>:<port_N>"
              + "\n    Default: " + serverAddressesToString(c.addresses)
              + "\n  --test_cases=<testcase_1:w_1>,<testcase_2:w_2>...<testcase_n:w_n>"
              + "\n    List of <testcase,weight> tuples. Weight is the relative frequency at which"
              + " testcase is run."
              + "\n    Valid Testcases:"
              + validTestCasesHelpText()
              + "\n  --use_tls=true|false           Whether to use TLS. Default: " + c.useTls
              + "\n  --use_test_ca=true|false       Whether to trust our fake CA. Requires"
              + " --use_tls=true"
              + "\n                                 to have effect. Default: " + c.useTestCa
              + "\n  --test_duration_secs=SECONDS   '-1' for no limit. Default: " + c.durationSecs
              + "\n  --num_channels_per_server=INT  Number of connections to each server address."
              + " Default: " + c.channelsPerServer
              + "\n  --num_stubs_per_channel=INT    Default: " + c.stubsPerChannel
              + "\n  --metrics_port=PORT            Listening port of the metrics server."
              + " Default: " + c.metricsPort
      );
      System.exit(1);
    }
  }

  @VisibleForTesting
  void startMetricsService() throws IOException {
    Preconditions.checkState(!shutdown, "client was shutdown.");

    metricsServer = ServerBuilder.forPort(metricsPort)
        .addService(new MetricsServiceImpl())
        .build()
        .start();
  }

  @VisibleForTesting
  void runStressTest() throws Exception {
    Preconditions.checkState(!shutdown, "client was shutdown.");
    if (testCaseWeightPairs.isEmpty()) {
      return;
    }

    int numChannels = addresses.size() * channelsPerServer;
    int numThreads = numChannels * stubsPerChannel;
    threadpool = MoreExecutors.listeningDecorator(newFixedThreadPool(numThreads));
    int serverIdx = -1;
    for (InetSocketAddress address : addresses) {
      serverIdx++;
      for (int i = 0; i < channelsPerServer; i++) {
        ManagedChannel channel = createChannel(address);
        channels.add(channel);
        for (int j = 0; j < stubsPerChannel; j++) {
          String gaugeName =
              String.format("/stress_test/server_%d/channel_%d/stub_%d/qps", serverIdx, i, j);
          Worker worker =
              new Worker(channel, testCaseWeightPairs, durationSecs, gaugeName);

          workerFutures.add(threadpool.submit(worker));
        }
      }
    }
  }

  @VisibleForTesting
  void blockUntilStressTestComplete() throws Exception {
    Preconditions.checkState(!shutdown, "client was shutdown.");

    ListenableFuture<?> f = Futures.allAsList(workerFutures);
    if (durationSecs == -1) {
      // '-1' indicates that the stress test runs until terminated by the user.
      f.get();
    } else {
      f.get(durationSecs + WORKER_GRACE_PERIOD_SECS, SECONDS);
    }
  }

  @VisibleForTesting
  void shutdown() {
    if (shutdown) {
      return;
    }
    shutdown = true;

    for (ManagedChannel ch : channels) {
      try {
        ch.shutdownNow();
        ch.awaitTermination(1, SECONDS);
      } catch (Throwable t) {
        log.log(Level.WARNING, "Error shutting down channel!", t);
      }
    }

    try {
      metricsServer.shutdownNow();
    } catch (Throwable t) {
      log.log(Level.WARNING, "Error shutting down metrics service!", t);
    }

    try {
      if (threadpool != null) {
        threadpool.shutdownNow();
      }
    } catch (Throwable t) {
      log.log(Level.WARNING, "Error shutting down threadpool.", t);
    }
  }

  @VisibleForTesting
  int getMetricServerPort() {
    return metricsServer.getPort();
  }

  private List<InetSocketAddress> parseServerAddresses(String addressesStr) {
    List<InetSocketAddress> addresses = new ArrayList<>();

    for (List<String> namePort : parseCommaSeparatedTuples(addressesStr)) {
      InetAddress address;
      String name = namePort.get(0);
      int port = Integer.valueOf(namePort.get(1));
      try {
        address = InetAddress.getByName(name);
        if (serverHostOverride != null) {
          // Force the hostname to match the cert the server uses.
          address = InetAddress.getByAddress(serverHostOverride, address.getAddress());
        }
      } catch (UnknownHostException ex) {
        throw new RuntimeException(ex);
      }
      addresses.add(new InetSocketAddress(address, port));
    }

    return addresses;
  }

  private static List<TestCaseWeightPair> parseTestCases(String testCasesStr) {
    List<TestCaseWeightPair> testCaseWeightPairs = new ArrayList<>();

    for (List<String> nameWeight : parseCommaSeparatedTuples(testCasesStr)) {
      TestCases testCase = TestCases.fromString(nameWeight.get(0));
      int weight = Integer.valueOf(nameWeight.get(1));
      testCaseWeightPairs.add(new TestCaseWeightPair(testCase, weight));
    }

    return testCaseWeightPairs;
  }

  private static List<List<String>> parseCommaSeparatedTuples(String str) {
    List<List<String>> tuples = new ArrayList<>();
    for (String tupleStr : Splitter.on(',').split(str)) {
      int splitIdx = tupleStr.lastIndexOf(':');
      if (splitIdx == -1) {
        throw new IllegalArgumentException("Illegal tuple format: '" + tupleStr + "'");
      }
      String part0 = tupleStr.substring(0, splitIdx);
      String part1 = tupleStr.substring(splitIdx + 1);
      tuples.add(asList(part0, part1));
    }
    return tuples;
  }

  private ManagedChannel createChannel(InetSocketAddress address) {
    SslContext sslContext = null;
    if (useTestCa) {
      try {
        sslContext = GrpcSslContexts.forClient().trustManager(
            TestUtils.loadCert("ca.pem")).build();
      } catch (Exception ex) {
        throw new RuntimeException(ex);
      }
    }
    return NettyChannelBuilder.forAddress(address)
        .negotiationType(useTls ? NegotiationType.TLS : NegotiationType.PLAINTEXT)
        .sslContext(sslContext)
        .build();
  }

  private static String serverAddressesToString(List<InetSocketAddress> addresses) {
    List<String> tmp = new ArrayList<>();
    for (InetSocketAddress address : addresses) {
      URI uri;
      try {
        uri = new URI(null, null, address.getHostName(), address.getPort(), null, null, null);
      } catch (URISyntaxException e) {
        throw new RuntimeException(e);
      }
      tmp.add(uri.getAuthority());
    }
    return Joiner.on(',').join(tmp);
  }

  private static String validTestCasesHelpText() {
    StringBuilder builder = new StringBuilder();
    for (TestCases testCase : TestCases.values()) {
      String strTestcase = testCase.name().toLowerCase();
      builder.append("\n      ")
          .append(strTestcase)
          .append(": ")
          .append(testCase.description());
    }
    return builder.toString();
  }

  /**
   * A stress test worker. Every stub has its own stress test worker.
   */
  private class Worker implements Runnable {

    // Interval at which the QPS stats of metrics service are updated.
    private static final long METRICS_COLLECTION_INTERVAL_SECS = 5;

    private final ManagedChannel channel;
    private final List<TestCaseWeightPair> testCaseWeightPairs;
    private final Integer durationSec;
    private final String gaugeName;

    Worker(ManagedChannel channel, List<TestCaseWeightPair> testCaseWeightPairs,
        int durationSec, String gaugeName) {
      Preconditions.checkArgument(durationSec >= -1, "durationSec must be gte -1.");
      this.channel = Preconditions.checkNotNull(channel, "channel");
      this.testCaseWeightPairs =
          Preconditions.checkNotNull(testCaseWeightPairs, "testCaseWeightPairs");
      this.durationSec = durationSec == -1 ? null : durationSec;
      this.gaugeName = Preconditions.checkNotNull(gaugeName, "gaugeName");
    }

    @Override
    public void run() {
      // Simplify debugging if the worker crashes / never terminates.
      Thread.currentThread().setName(gaugeName);

      Tester tester = new Tester();
      tester.setUp();
      WeightedTestCaseSelector testCaseSelector = new WeightedTestCaseSelector(testCaseWeightPairs);
      Long endTime = durationSec == null ? null : System.nanoTime() + SECONDS.toNanos(durationSecs);
      long lastMetricsCollectionTime = initLastMetricsCollectionTime();
      // Number of interop testcases run since the last time metrics have been updated.
      long testCasesSinceLastMetricsCollection = 0;

      while (!Thread.currentThread().isInterrupted() && !shutdown
          && (endTime == null || endTime - System.nanoTime() > 0)) {
        try {
          runTestCase(tester, testCaseSelector.nextTestCase());
        } catch (Exception e) {
          throw new RuntimeException(e);
        }

        testCasesSinceLastMetricsCollection++;

        double durationSecs = computeDurationSecs(lastMetricsCollectionTime);
        if (durationSecs >= METRICS_COLLECTION_INTERVAL_SECS) {
          long qps = (long) Math.ceil(testCasesSinceLastMetricsCollection / durationSecs);

          Metrics.GaugeResponse gauge = Metrics.GaugeResponse
              .newBuilder()
              .setName(gaugeName)
              .setLongValue(qps)
              .build();

          gauges.put(gaugeName, gauge);

          lastMetricsCollectionTime = System.nanoTime();
          testCasesSinceLastMetricsCollection = 0;
        }
      }
    }

    private long initLastMetricsCollectionTime() {
      return System.nanoTime() - SECONDS.toNanos(METRICS_COLLECTION_INTERVAL_SECS);
    }

    private double computeDurationSecs(long lastMetricsCollectionTime) {
      return (System.nanoTime() - lastMetricsCollectionTime) / 1000000000.0;
    }

    private void runTestCase(Tester tester, TestCases testCase) throws Exception {
      // TODO(buchgr): Implement tests requiring auth, once C++ supports it.
      switch (testCase) {
        case EMPTY_UNARY:
          tester.emptyUnary();
          break;

        case LARGE_UNARY:
          tester.largeUnary();
          break;

        case CLIENT_STREAMING:
          tester.clientStreaming();
          break;

        case SERVER_STREAMING:
          tester.serverStreaming();
          break;

        case PING_PONG:
          tester.pingPong();
          break;

        case EMPTY_STREAM:
          tester.emptyStream();
          break;

        case UNIMPLEMENTED_METHOD: {
          tester.unimplementedMethod();
          break;
        }

        case UNIMPLEMENTED_SERVICE: {
          tester.unimplementedService();
          break;
        }

        case CANCEL_AFTER_BEGIN: {
          tester.cancelAfterBegin();
          break;
        }

        case CANCEL_AFTER_FIRST_RESPONSE: {
          tester.cancelAfterFirstResponse();
          break;
        }

        case TIMEOUT_ON_SLEEPING_SERVER: {
          tester.timeoutOnSleepingServer();
          break;
        }

        default:
          throw new IllegalArgumentException("Unknown test case: " + testCase);
      }
    }

    class Tester extends AbstractInteropTest {
      @Override
      protected ManagedChannel createChannel() {
        return Worker.this.channel;
      }

      @Override
      protected int operationTimeoutMillis() {
        // Don't enforce a timeout when using the interop tests for the stress test client.
        // Fixes https://github.com/grpc/grpc-java/issues/1812
        return Integer.MAX_VALUE;
      }

      @Override
      protected boolean metricsExpected() {
        // TODO(zhangkun83): we may want to enable the real google Instrumentation implementation in
        // stress tests.
        return false;
      }
    }

    class WeightedTestCaseSelector {
      /**
       * Randomly shuffled and cyclic sequence that contains each testcase proportionally
       * to its weight.
       */
      final Iterator<TestCases> testCases;

      WeightedTestCaseSelector(List<TestCaseWeightPair> testCaseWeightPairs) {
        Preconditions.checkNotNull(testCaseWeightPairs, "testCaseWeightPairs");
        Preconditions.checkArgument(testCaseWeightPairs.size() > 0);

        List<TestCases> testCases = new ArrayList<>();
        for (TestCaseWeightPair testCaseWeightPair : testCaseWeightPairs) {
          for (int i = 0; i < testCaseWeightPair.weight; i++) {
            testCases.add(testCaseWeightPair.testCase);
          }
        }

        shuffle(testCases);

        this.testCases = Iterators.cycle(testCases);
      }

      TestCases nextTestCase() {
        return testCases.next();
      }
    }
  }

  /**
   * Service that exports the QPS metrics of the stress test.
   */
  private class MetricsServiceImpl extends MetricsServiceGrpc.MetricsServiceImplBase {

    @Override
    public void getAllGauges(Metrics.EmptyMessage request,
        StreamObserver<Metrics.GaugeResponse> responseObserver) {
      for (Metrics.GaugeResponse gauge : gauges.values()) {
        responseObserver.onNext(gauge);
      }
      responseObserver.onCompleted();
    }

    @Override
    public void getGauge(Metrics.GaugeRequest request,
        StreamObserver<Metrics.GaugeResponse> responseObserver) {
      String gaugeName = request.getName();
      Metrics.GaugeResponse gauge = gauges.get(gaugeName);
      if (gauge != null) {
        responseObserver.onNext(gauge);
        responseObserver.onCompleted();
      } else {
        responseObserver.onError(new StatusException(Status.NOT_FOUND));
      }
    }
  }

  @VisibleForTesting
  static class TestCaseWeightPair {
    final TestCases testCase;
    final int weight;

    TestCaseWeightPair(TestCases testCase, int weight) {
      Preconditions.checkArgument(weight >= 0, "weight must be positive.");
      this.testCase = Preconditions.checkNotNull(testCase, "testCase");
      this.weight = weight;
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof TestCaseWeightPair)) {
        return false;
      }
      TestCaseWeightPair that = (TestCaseWeightPair) other;
      return testCase.equals(that.testCase) && weight == that.weight;
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(testCase, weight);
    }
  }

  @VisibleForTesting
  List<InetSocketAddress> addresses() {
    return Collections.unmodifiableList(addresses);
  }

  @VisibleForTesting
  String serverHostOverride() {
    return serverHostOverride;
  }

  @VisibleForTesting
  boolean useTls() {
    return useTls;
  }

  @VisibleForTesting
  boolean useTestCa() {
    return useTestCa;
  }

  @VisibleForTesting
  List<TestCaseWeightPair> testCaseWeightPairs() {
    return testCaseWeightPairs;
  }

  @VisibleForTesting
  int durationSecs() {
    return durationSecs;
  }

  @VisibleForTesting
  int channelsPerServer() {
    return channelsPerServer;
  }

  @VisibleForTesting
  int stubsPerChannel() {
    return stubsPerChannel;
  }

  @VisibleForTesting
  int metricsPort() {
    return metricsPort;
  }
}
