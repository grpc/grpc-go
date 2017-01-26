/*
 * Copyright 2016, Google Inc. All rights reserved.
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
    channel = NettyChannelBuilder.forAddress("localhost", port).usePlaintext(true).build();
    workerServiceStub = WorkerServiceGrpc.newStub(channel);
    marksQueue = new LinkedBlockingQueue<Stats.ClientStats>();
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
