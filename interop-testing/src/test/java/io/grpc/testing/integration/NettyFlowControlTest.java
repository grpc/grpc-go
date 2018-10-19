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

import static org.junit.Assert.assertTrue;

import com.google.common.collect.ImmutableList;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.netty.InternalHandlerSettings;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.integration.Messages.ResponseParameters;
import io.grpc.testing.integration.Messages.StreamingOutputCallRequest;
import io.grpc.testing.integration.Messages.StreamingOutputCallResponse;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.AfterClass;
import org.junit.Before;
import org.junit.BeforeClass;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class NettyFlowControlTest {

  // in bytes
  private static final int LOW_BAND = 2 * 1024 * 1024;
  private static final int HIGH_BAND = 30 * 1024 * 1024;

  // in milliseconds
  private static final int MED_LAT = 10;

  // in bytes
  private static final int TINY_WINDOW = 1;
  private static final int REGULAR_WINDOW = 64 * 1024;
  private static final int MAX_WINDOW = 8 * 1024 * 1024;

  private static ManagedChannel channel;
  private static Server server;
  private static TrafficControlProxy proxy;

  private int proxyPort;
  private int serverPort;

  private static final ThreadPoolExecutor executor =
      new ThreadPoolExecutor(1, 10, 1, TimeUnit.SECONDS, new LinkedBlockingQueue<Runnable>(),
          new DefaultThreadFactory("flowcontrol-test-pool", true));

  @BeforeClass
  public static void setUp() {
    InternalHandlerSettings.enable(true);
    InternalHandlerSettings.autoWindowOn(true);
  }

  @AfterClass
  public static void shutDownTests() {
    executor.shutdown();
  }

  @Before
  public void initTest() {
    startServer(REGULAR_WINDOW);
    serverPort = server.getPort();
  }

  @After
  public void endTest() throws IOException {
    proxy.shutDown();
    server.shutdown();
  }

  @Test
  public void largeBdp() throws InterruptedException, IOException {
    proxy = new TrafficControlProxy(serverPort, HIGH_BAND, MED_LAT, TimeUnit.MILLISECONDS);
    proxy.start();
    proxyPort = proxy.getPort();
    resetConnection(REGULAR_WINDOW);
    doTest(HIGH_BAND, MED_LAT);
  }

  @Test
  public void smallBdp() throws InterruptedException, IOException {
    proxy = new TrafficControlProxy(serverPort, LOW_BAND, MED_LAT, TimeUnit.MILLISECONDS);
    proxy.start();
    proxyPort = proxy.getPort();
    resetConnection(REGULAR_WINDOW);
    doTest(LOW_BAND, MED_LAT);
  }

  @Test
  public void verySmallWindowMakesProgress() throws InterruptedException, IOException {
    proxy = new TrafficControlProxy(serverPort, HIGH_BAND, MED_LAT, TimeUnit.MILLISECONDS);
    proxy.start();
    proxyPort = proxy.getPort();
    resetConnection(TINY_WINDOW);
    doTest(HIGH_BAND, MED_LAT);
  }

  /**
   * Main testing method. Streams 2 MB of data from a server and records the final window and
   * average bandwidth usage.
   */
  private void doTest(int bandwidth, int latency) throws InterruptedException {

    int streamSize = 1 * 1024 * 1024;
    long expectedWindow = latency * (bandwidth / TimeUnit.SECONDS.toMillis(1));

    TestServiceGrpc.TestServiceStub stub = TestServiceGrpc.newStub(channel);
    StreamingOutputCallRequest.Builder builder = StreamingOutputCallRequest.newBuilder()
        .addResponseParameters(ResponseParameters.newBuilder().setSize(streamSize / 16))
        .addResponseParameters(ResponseParameters.newBuilder().setSize(streamSize / 16))
        .addResponseParameters(ResponseParameters.newBuilder().setSize(streamSize / 8))
        .addResponseParameters(ResponseParameters.newBuilder().setSize(streamSize / 4))
        .addResponseParameters(ResponseParameters.newBuilder().setSize(streamSize / 2));
    StreamingOutputCallRequest request = builder.build();

    TestStreamObserver observer = new TestStreamObserver(expectedWindow);
    stub.streamingOutputCall(request, observer);
    int lastWindow = observer.waitFor();

    // deal with cases that either don't cause a window update or hit max window
    expectedWindow = Math.min(MAX_WINDOW, Math.max(expectedWindow, REGULAR_WINDOW));

    // Range looks large, but this allows for only one extra/missed window update
    // (one extra update causes a 2x difference and one missed update causes a .5x difference)
    assertTrue("Window was " + lastWindow + " expecting " + expectedWindow,
        lastWindow < 2 * expectedWindow);
    assertTrue("Window was " + lastWindow + " expecting " + expectedWindow,
        expectedWindow < 2 * lastWindow);
  }

  /**
   * Resets client/server and their flow control windows.
   */
  private void resetConnection(int clientFlowControlWindow)
      throws InterruptedException {
    if (channel != null) {
      if (!channel.isShutdown()) {
        channel.shutdown();
        channel.awaitTermination(100, TimeUnit.MILLISECONDS);
      }
    }
    channel = NettyChannelBuilder.forAddress(new InetSocketAddress("localhost", proxyPort))
        .flowControlWindow(clientFlowControlWindow)
        .negotiationType(NegotiationType.PLAINTEXT)
        .build();
  }

  private void startServer(int serverFlowControlWindow) {
    ServerBuilder<?> builder =
        NettyServerBuilder.forAddress(new InetSocketAddress("localhost", 0))
        .flowControlWindow(serverFlowControlWindow);
    builder.addService(ServerInterceptors.intercept(
        new TestServiceImpl(Executors.newScheduledThreadPool(2)),
        ImmutableList.<ServerInterceptor>of()));
    try {
      server = builder.build().start();
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }

  /**
   * Simple stream observer to measure elapsed time of the call.
   */
  private static class TestStreamObserver implements StreamObserver<StreamingOutputCallResponse> {

    long startRequestNanos;
    long endRequestNanos;
    private final CountDownLatch latch = new CountDownLatch(1);
    long expectedWindow;
    int lastWindow;

    public TestStreamObserver(long window) {
      startRequestNanos = System.nanoTime();
      expectedWindow = window;
    }

    @Override
    public void onNext(StreamingOutputCallResponse value) {
      lastWindow = InternalHandlerSettings.getLatestClientWindow();
      if (lastWindow >= expectedWindow) {
        onCompleted();
      }
    }

    @Override
    public void onError(Throwable t) {
      latch.countDown();
      throw new RuntimeException(t);
    }

    @Override
    public void onCompleted() {
      latch.countDown();
    }

    public long getElapsedTime() {
      return endRequestNanos - startRequestNanos;
    }

    public int waitFor() throws InterruptedException {
      latch.await();
      return lastWindow;
    }
  }
}
