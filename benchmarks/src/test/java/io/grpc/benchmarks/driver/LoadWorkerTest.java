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

package io.grpc.benchmarks.driver;

import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.grpc.ManagedChannel;
import io.grpc.benchmarks.Utils;
import io.grpc.benchmarks.proto.Control;
import io.grpc.benchmarks.proto.Stats;
import io.grpc.benchmarks.proto.WorkerServiceGrpc;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.stub.StreamObserver;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Basic tests for {@link io.grpc.benchmarks.driver.LoadWorker}
 */
@RunWith(JUnit4.class)
public class LoadWorkerTest {


  private static final int TIMEOUT = 5;
  private static final Control.ClientArgs MARK = Control.ClientArgs.newBuilder()
      .setMark(Control.Mark.newBuilder().setReset(true).build())
      .build();

  private LoadWorker worker;
  private ManagedChannel channel;
  private WorkerServiceGrpc.WorkerServiceStub workerServiceStub;
  private LinkedBlockingQueue<Stats.ClientStats> marksQueue;

  @Before
  public void setup() throws Exception {
    int port = Utils.pickUnusedPort();
    worker = new LoadWorker(port, 0);
    worker.start();
    channel = NettyChannelBuilder.forAddress("localhost", port).usePlaintext().build();
    workerServiceStub = WorkerServiceGrpc.newStub(channel);
    marksQueue = new LinkedBlockingQueue<>();
  }

  @Test
  public void runUnaryBlockingClosedLoop() throws Exception {
    Control.ServerArgs.Builder serverArgsBuilder = Control.ServerArgs.newBuilder();
    serverArgsBuilder.getSetupBuilder()
        .setServerType(Control.ServerType.ASYNC_SERVER)
        .setAsyncServerThreads(4)
        .setPort(0)
        .getPayloadConfigBuilder().getSimpleParamsBuilder().setRespSize(1000);
    int serverPort = startServer(serverArgsBuilder.build());

    Control.ClientArgs.Builder clientArgsBuilder = Control.ClientArgs.newBuilder();
    String serverAddress = "localhost:" + serverPort;
    clientArgsBuilder.getSetupBuilder()
        .setClientType(Control.ClientType.SYNC_CLIENT)
        .setRpcType(Control.RpcType.UNARY)
        .setClientChannels(2)
        .setOutstandingRpcsPerChannel(2)
        .addServerTargets(serverAddress);
    clientArgsBuilder.getSetupBuilder().getPayloadConfigBuilder().getSimpleParamsBuilder()
        .setReqSize(1000)
        .setRespSize(1000);
    clientArgsBuilder.getSetupBuilder().getHistogramParamsBuilder()
        .setResolution(0.01)
        .setMaxPossible(60000000000.0);
    StreamObserver<Control.ClientArgs> clientObserver = startClient(clientArgsBuilder.build());
    assertWorkOccurred(clientObserver);
  }

  @Test
  public void runUnaryAsyncClosedLoop() throws Exception {
    Control.ServerArgs.Builder serverArgsBuilder = Control.ServerArgs.newBuilder();
    serverArgsBuilder.getSetupBuilder()
        .setServerType(Control.ServerType.ASYNC_SERVER)
        .setAsyncServerThreads(4)
        .setPort(0)
        .getPayloadConfigBuilder().getSimpleParamsBuilder().setRespSize(1000);
    int serverPort = startServer(serverArgsBuilder.build());

    Control.ClientArgs.Builder clientArgsBuilder = Control.ClientArgs.newBuilder();
    String serverAddress = "localhost:" + serverPort;
    clientArgsBuilder.getSetupBuilder()
        .setClientType(Control.ClientType.ASYNC_CLIENT)
        .setClientChannels(2)
        .setRpcType(Control.RpcType.UNARY)
        .setOutstandingRpcsPerChannel(1)
        .setAsyncClientThreads(4)
        .addServerTargets(serverAddress);
    clientArgsBuilder.getSetupBuilder().getPayloadConfigBuilder().getSimpleParamsBuilder()
        .setReqSize(1000)
        .setRespSize(1000);
    clientArgsBuilder.getSetupBuilder().getHistogramParamsBuilder()
        .setResolution(0.01)
        .setMaxPossible(60000000000.0);
    StreamObserver<Control.ClientArgs> clientObserver = startClient(clientArgsBuilder.build());
    assertWorkOccurred(clientObserver);
  }

  @Test
  public void runPingPongAsyncClosedLoop() throws Exception {
    Control.ServerArgs.Builder serverArgsBuilder = Control.ServerArgs.newBuilder();
    serverArgsBuilder.getSetupBuilder()
        .setServerType(Control.ServerType.ASYNC_SERVER)
        .setAsyncServerThreads(4)
        .setPort(0)
        .getPayloadConfigBuilder().getSimpleParamsBuilder().setRespSize(1000);
    int serverPort = startServer(serverArgsBuilder.build());

    Control.ClientArgs.Builder clientArgsBuilder = Control.ClientArgs.newBuilder();
    String serverAddress = "localhost:" + serverPort;
    clientArgsBuilder.getSetupBuilder()
        .setClientType(Control.ClientType.ASYNC_CLIENT)
        .setClientChannels(2)
        .setRpcType(Control.RpcType.STREAMING)
        .setOutstandingRpcsPerChannel(1)
        .setAsyncClientThreads(4)
        .addServerTargets(serverAddress);
    clientArgsBuilder.getSetupBuilder().getPayloadConfigBuilder().getSimpleParamsBuilder()
        .setReqSize(1000)
        .setRespSize(1000);
    clientArgsBuilder.getSetupBuilder().getHistogramParamsBuilder()
        .setResolution(0.01)
        .setMaxPossible(60000000000.0);
    StreamObserver<Control.ClientArgs> clientObserver = startClient(clientArgsBuilder.build());
    assertWorkOccurred(clientObserver);
  }

  @Test
  public void runGenericPingPongAsyncClosedLoop() throws Exception {
    Control.ServerArgs.Builder serverArgsBuilder = Control.ServerArgs.newBuilder();
    serverArgsBuilder.getSetupBuilder()
        .setServerType(Control.ServerType.ASYNC_GENERIC_SERVER)
        .setAsyncServerThreads(4)
        .setPort(0)
        .getPayloadConfigBuilder().getBytebufParamsBuilder().setReqSize(1000).setRespSize(1000);
    int serverPort = startServer(serverArgsBuilder.build());

    Control.ClientArgs.Builder clientArgsBuilder = Control.ClientArgs.newBuilder();
    String serverAddress = "localhost:" + serverPort;
    clientArgsBuilder.getSetupBuilder()
        .setClientType(Control.ClientType.ASYNC_CLIENT)
        .setClientChannels(2)
        .setRpcType(Control.RpcType.STREAMING)
        .setOutstandingRpcsPerChannel(1)
        .setAsyncClientThreads(4)
        .addServerTargets(serverAddress);
    clientArgsBuilder.getSetupBuilder().getPayloadConfigBuilder().getBytebufParamsBuilder()
        .setReqSize(1000)
        .setRespSize(1000);
    clientArgsBuilder.getSetupBuilder().getHistogramParamsBuilder()
        .setResolution(0.01)
        .setMaxPossible(60000000000.0);
    StreamObserver<Control.ClientArgs> clientObserver = startClient(clientArgsBuilder.build());
    assertWorkOccurred(clientObserver);
  }

  private void assertWorkOccurred(StreamObserver<Control.ClientArgs> clientObserver)
      throws InterruptedException {

    Stats.ClientStats stat = null;
    for (int i = 0; i < 3; i++) {
      // Poll until we get some stats
      Thread.sleep(300);
      clientObserver.onNext(MARK);
      stat = marksQueue.poll(TIMEOUT, TimeUnit.SECONDS);
      if (stat == null) {
        fail("Did not receive stats");
      }
      if (stat.getLatencies().getCount() > 10) {
        break;
      }
    }
    clientObserver.onCompleted();
    assertTrue(stat.hasLatencies());
    assertTrue(stat.getLatencies().getCount() < stat.getLatencies().getSum());
    double mean = stat.getLatencies().getSum() / stat.getLatencies().getCount();
    System.out.println("Mean " + mean + " us");
    assertTrue(mean > stat.getLatencies().getMinSeen());
    assertTrue(mean < stat.getLatencies().getMaxSeen());
  }

  private StreamObserver<Control.ClientArgs> startClient(Control.ClientArgs clientArgs)
      throws InterruptedException {
    final CountDownLatch clientReady = new CountDownLatch(1);
    StreamObserver<Control.ClientArgs> clientObserver = workerServiceStub.runClient(
        new StreamObserver<Control.ClientStatus>() {
          @Override
          public void onNext(Control.ClientStatus value) {
            clientReady.countDown();
            if (value.hasStats()) {
              marksQueue.add(value.getStats());
            }
          }

          @Override
          public void onError(Throwable t) {
          }

          @Override
          public void onCompleted() {
          }
        });

    // Start the client
    clientObserver.onNext(clientArgs);
    if (!clientReady.await(TIMEOUT, TimeUnit.SECONDS)) {
      fail("Client failed to start");
    }
    return clientObserver;
  }

  private int startServer(Control.ServerArgs serverArgs) throws InterruptedException {
    final AtomicInteger serverPort = new AtomicInteger();
    final CountDownLatch serverReady = new CountDownLatch(1);
    StreamObserver<Control.ServerArgs> serverObserver =
        workerServiceStub.runServer(new StreamObserver<Control.ServerStatus>() {
          @Override
          public void onNext(Control.ServerStatus value) {
            serverPort.set(value.getPort());
            serverReady.countDown();
          }

          @Override
          public void onError(Throwable t) {
          }

          @Override
          public void onCompleted() {
          }
        });
    // trigger server startup
    serverObserver.onNext(serverArgs);
    if (!serverReady.await(TIMEOUT, TimeUnit.SECONDS)) {
      fail("Server failed to start");
    }
    return serverPort.get();
  }
}
