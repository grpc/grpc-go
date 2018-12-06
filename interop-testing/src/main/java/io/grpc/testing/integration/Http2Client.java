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

import static java.util.concurrent.Executors.newFixedThreadPool;

import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.ListeningExecutorService;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.ByteString;
import io.grpc.ManagedChannel;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.integration.Messages.Payload;
import io.grpc.testing.integration.Messages.SimpleRequest;
import io.grpc.testing.integration.Messages.SimpleResponse;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Client application for the {@link TestServiceGrpc.TestServiceImplBase} that runs through a series
 * of HTTP/2 interop tests. The tests are designed to simulate incorrect behavior on the part of the
 * server. Some of the test cases require server-side checks and do not have assertions within the
 * client code.
 */
public final class Http2Client {
  private static final Logger logger = Logger.getLogger(Http2Client.class.getName());

  /**
   * The main application allowing this client to be launched from the command line.
   */
  public static void main(String[] args) throws Exception {
    final Http2Client client = new Http2Client();
    client.parseArgs(args);
    client.setUp();

    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        try {
          client.shutdown();
        } catch (Exception e) {
          logger.log(Level.SEVERE, e.getMessage(), e);
        }
      }
    });

    try {
      client.run();
    } finally {
      client.shutdown();
    }
  }

  private String serverHost = "localhost";
  private int serverPort = 8080;
  private String testCase = Http2TestCases.RST_AFTER_DATA.name();

  private Tester tester = new Tester();
  private ListeningExecutorService threadpool;

  protected ManagedChannel channel;
  protected TestServiceGrpc.TestServiceBlockingStub blockingStub;
  protected TestServiceGrpc.TestServiceStub asyncStub;

  private void parseArgs(String[] args) {
    boolean usage = false;
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
      if ("server_host".equals(key)) {
        serverHost = value;
      } else if ("server_port".equals(key)) {
        serverPort = Integer.parseInt(value);
      } else if ("test_case".equals(key)) {
        testCase = value;
      } else {
        System.err.println("Unknown argument: " + key);
        usage = true;
        break;
      }
    }
    if (usage) {
      Http2Client c = new Http2Client();
      System.out.println(
          "Usage: [ARGS...]"
              + "\n"
              + "\n  --server_host=HOST          Server to connect to. Default " + c.serverHost
              + "\n  --server_port=PORT          Port to connect to. Default " + c.serverPort
              + "\n  --test_case=TESTCASE        Test case to run. Default " + c.testCase
              + "\n    Valid options:"
              + validTestCasesHelpText()
      );
      System.exit(1);
    }
  }

  private void setUp() {
    channel = createChannel();
    blockingStub = TestServiceGrpc.newBlockingStub(channel);
    asyncStub = TestServiceGrpc.newStub(channel);
  }

  private void shutdown() {
    try {
      if (channel != null) {
        channel.shutdownNow();
        channel.awaitTermination(1, TimeUnit.SECONDS);
      }
    } catch (Exception ex) {
      throw new RuntimeException(ex);
    }

    try {
      if (threadpool != null) {
        threadpool.shutdownNow();
      }
    } catch (Exception ex) {
      throw new RuntimeException(ex);
    }
  }

  private void run() {
    logger.info("Running test " + testCase);
    try {
      runTest(Http2TestCases.fromString(testCase));
    } catch (RuntimeException ex) {
      throw ex;
    } catch (Exception ex) {
      throw new RuntimeException(ex);
    }
    logger.info("Test completed.");
  }

  private void runTest(Http2TestCases testCase) throws Exception {
    switch (testCase) {
      case RST_AFTER_HEADER:
        tester.rstAfterHeader();
        break;
      case RST_AFTER_DATA:
        tester.rstAfterData();
        break;
      case RST_DURING_DATA:
        tester.rstDuringData();
        break;
      case GOAWAY:
        tester.goAway();
        break;
      case PING:
        tester.ping();
        break;
      case MAX_STREAMS:
        tester.maxStreams();
        break;
      default:
        throw new IllegalArgumentException("Unknown test case: " + testCase);
    }
  }

  private class Tester {
    private final int timeoutSeconds = 180;

    private final int responseSize = 314159;
    private final int payloadSize = 271828;
    private final SimpleRequest simpleRequest = SimpleRequest.newBuilder()
        .setResponseSize(responseSize)
        .setPayload(Payload.newBuilder().setBody(ByteString.copyFrom(new byte[payloadSize])))
        .build();
    final SimpleResponse goldenResponse = SimpleResponse.newBuilder()
        .setPayload(Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[responseSize])))
        .build();

    private void rstAfterHeader() throws Exception {
      try {
        blockingStub.unaryCall(simpleRequest);
        throw new AssertionError("Expected call to fail");
      } catch (StatusRuntimeException ex) {
        assertRstStreamReceived(ex.getStatus());
      }
    }

    private void rstAfterData() throws Exception {
      // Use async stub to verify data is received.
      RstStreamObserver responseObserver = new RstStreamObserver();
      asyncStub.unaryCall(simpleRequest, responseObserver);
      if (!responseObserver.awaitCompletion(timeoutSeconds, TimeUnit.SECONDS)) {
        throw new AssertionError("Operation timed out");
      }
      if (responseObserver.getError() == null) {
        throw new AssertionError("Expected call to fail");
      }
      assertRstStreamReceived(Status.fromThrowable(responseObserver.getError()));
      if (responseObserver.getResponses().size() != 1) {
        throw new AssertionError("Expected one response");
      }
    }

    private void rstDuringData() throws Exception {
      // Use async stub to verify no data is received.
      RstStreamObserver responseObserver = new RstStreamObserver();
      asyncStub.unaryCall(simpleRequest, responseObserver);
      if (!responseObserver.awaitCompletion(timeoutSeconds, TimeUnit.SECONDS)) {
        throw new AssertionError("Operation timed out");
      }
      if (responseObserver.getError() == null) {
        throw new AssertionError("Expected call to fail");
      }
      assertRstStreamReceived(Status.fromThrowable(responseObserver.getError()));
      if (responseObserver.getResponses().size() != 0) {
        throw new AssertionError("Expected zero responses");
      }
    }

    private void goAway() throws Exception {
      assertResponseEquals(blockingStub.unaryCall(simpleRequest), goldenResponse);
      TimeUnit.SECONDS.sleep(1);
      assertResponseEquals(blockingStub.unaryCall(simpleRequest), goldenResponse);
    }

    private void ping() throws Exception {
      assertResponseEquals(blockingStub.unaryCall(simpleRequest), goldenResponse);
    }

    private void maxStreams() throws Exception {
      final int numThreads = 10;

      // Preliminary call to ensure MAX_STREAMS setting is received by the client.
      assertResponseEquals(blockingStub.unaryCall(simpleRequest), goldenResponse);

      threadpool = MoreExecutors.listeningDecorator(newFixedThreadPool(numThreads));
      List<ListenableFuture<?>> workerFutures = new ArrayList<>();
      for (int i = 0; i < numThreads; i++) {
        workerFutures.add(threadpool.submit(new MaxStreamsWorker(i, simpleRequest)));
      }
      ListenableFuture<?> f = Futures.allAsList(workerFutures);
      f.get(timeoutSeconds, TimeUnit.SECONDS);
    }

    private class RstStreamObserver implements StreamObserver<SimpleResponse> {
      private final CountDownLatch latch = new CountDownLatch(1);
      private final List<SimpleResponse> responses = new ArrayList<>();
      private Throwable error;

      @Override
      public void onNext(SimpleResponse value) {
        responses.add(value);
      }

      @Override
      public void onError(Throwable t) {
        error = t;
        latch.countDown();
      }

      @Override
      public void onCompleted() {
        latch.countDown();
      }

      public List<SimpleResponse> getResponses() {
        return responses;
      }

      public Throwable getError() {
        return error;
      }

      public boolean awaitCompletion(long timeout, TimeUnit unit) throws Exception {
        return latch.await(timeout, unit);
      }
    }

    private class MaxStreamsWorker implements Runnable {
      int threadNum;
      SimpleRequest request;

      MaxStreamsWorker(int threadNum, SimpleRequest request) {
        this.threadNum = threadNum;
        this.request = request;
      }

      @Override
      public void run() {
        Thread.currentThread().setName("thread:" + threadNum);
        try {
          TestServiceGrpc.TestServiceBlockingStub blockingStub =
              TestServiceGrpc.newBlockingStub(channel);
          assertResponseEquals(blockingStub.unaryCall(simpleRequest), goldenResponse);
        } catch (Exception e) {
          throw new RuntimeException(e);
        }
      }
    }

    private void assertRstStreamReceived(Status status) {
      if (!status.getCode().equals(Status.Code.UNAVAILABLE)) {
        throw new AssertionError("Wrong status code. Expected: " + Status.Code.UNAVAILABLE
            + " Received: " + status.getCode());
      }
      String http2ErrorPrefix = "HTTP/2 error code: NO_ERROR";
      if (status.getDescription() == null
          || !status.getDescription().startsWith(http2ErrorPrefix)) {
        throw new AssertionError("Wrong HTTP/2 error code. Expected: " + http2ErrorPrefix
            + " Received: " + status.getDescription());
      }
    }

    private void assertResponseEquals(SimpleResponse response, SimpleResponse goldenResponse) {
      if (!response.equals(goldenResponse)) {
        throw new AssertionError("Incorrect response received");
      }
    }
  }

  private ManagedChannel createChannel() {
    InetAddress address;
    try {
      address = InetAddress.getByName(serverHost);
    } catch (UnknownHostException ex) {
      throw new RuntimeException(ex);
    }
    return NettyChannelBuilder.forAddress(new InetSocketAddress(address, serverPort))
        .negotiationType(NegotiationType.PLAINTEXT)
        .build();
  }

  private static String validTestCasesHelpText() {
    StringBuilder builder = new StringBuilder();
    for (Http2TestCases testCase : Http2TestCases.values()) {
      String strTestcase = testCase.name().toLowerCase(Locale.ROOT);
      builder.append("\n      ")
          .append(strTestcase)
          .append(": ")
          .append(testCase.description());
    }
    return builder.toString();
  }
}

