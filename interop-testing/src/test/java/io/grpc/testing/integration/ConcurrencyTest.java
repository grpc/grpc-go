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

package io.grpc.testing.integration;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.TestUtils;
import io.grpc.testing.integration.Messages.PayloadType;
import io.grpc.testing.integration.Messages.ResponseParameters;
import io.grpc.testing.integration.Messages.StreamingOutputCallRequest;
import io.grpc.testing.integration.Messages.StreamingOutputCallResponse;
import io.netty.handler.ssl.ClientAuth;
import io.netty.handler.ssl.SslContext;
import java.io.File;
import java.io.IOException;
import java.security.cert.CertificateException;
import java.security.cert.X509Certificate;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;


/**
 * Tests that gRPC clients and servers can handle concurrent RPCs.
 *
 * <p>These tests use TLS to make them more realistic, and because we'd like to test the thread
 * safety of the TLS-related code paths as well.
 */
// TODO: Consider augmenting this class to perform non-streaming, client streaming, and
// bidirectional streaming requests also.
@RunWith(JUnit4.class)
public class ConcurrencyTest {
  /**
   * A response observer that signals a {@code CountDownLatch} when the proper number of responses
   * arrives and the server signals that the RPC is complete.
   */
  private static class SignalingResponseObserver
      implements StreamObserver<StreamingOutputCallResponse> {
    public SignalingResponseObserver(CountDownLatch responsesDoneSignal) {
      this.responsesDoneSignal = responsesDoneSignal;
    }

    @Override
    public void onCompleted() {
      Preconditions.checkState(numResponsesReceived == NUM_RESPONSES_PER_REQUEST);
      responsesDoneSignal.countDown();
    }

    @Override
    public void onError(Throwable error) {
      // This should never happen. If it does happen, ensure that the error is visible.
      error.printStackTrace();
    }

    @Override
    public void onNext(StreamingOutputCallResponse response) {
      numResponsesReceived++;
    }

    private final CountDownLatch responsesDoneSignal;
    private int numResponsesReceived = 0;
  }

  /**
   * A client worker task that waits until all client workers are ready, then sends a request for a
   * server-streaming RPC and arranges for a {@code CountDownLatch} to be signaled when the RPC is
   * complete.
   */
  private class ClientWorker implements Runnable {
    public ClientWorker(CyclicBarrier startBarrier, CountDownLatch responsesDoneSignal) {
      this.startBarrier = startBarrier;
      this.responsesDoneSignal = responsesDoneSignal;
    }

    @Override
    public void run() {
      try {
        // Prepare the request.
        StreamingOutputCallRequest.Builder requestBuilder = StreamingOutputCallRequest.newBuilder()
            .setResponseType(PayloadType.RANDOM);
        for (int i = 0; i < NUM_RESPONSES_PER_REQUEST; i++) {
          requestBuilder.addResponseParameters(ResponseParameters.newBuilder()
              .setSize(1000)
              .setIntervalUs(0));  // No delay between responses, for maximum concurrency.
        }
        StreamingOutputCallRequest request = requestBuilder.build();

        // Wait until all client worker threads are poised & ready, then send the request. This way
        // all clients send their requests at approximately the same time.
        startBarrier.await();
        clientStub.streamingOutputCall(request, new SignalingResponseObserver(responsesDoneSignal));
      } catch (Exception e) {
        throw e instanceof RuntimeException ? (RuntimeException) e : new RuntimeException(e);
      }
    }

    private final CyclicBarrier startBarrier;
    private final CountDownLatch responsesDoneSignal;
  }

  private static final int NUM_SERVER_THREADS = 10;
  private static final int NUM_CONCURRENT_REQUESTS = 100;
  private static final int NUM_RESPONSES_PER_REQUEST = 100;

  private Server server;
  private ManagedChannel clientChannel;
  private TestServiceGrpc.TestServiceStub clientStub;
  private ScheduledExecutorService serverExecutor;
  private ExecutorService clientExecutor;

  @Before
  public void setUp() throws Exception {
    serverExecutor = Executors.newScheduledThreadPool(NUM_SERVER_THREADS);
    clientExecutor = Executors.newFixedThreadPool(NUM_CONCURRENT_REQUESTS);

    server = newServer();

    // Create the client. Keep a reference to its channel so we can shut it down during tearDown().
    clientChannel = newClientChannel();
    clientStub = TestServiceGrpc.newStub(clientChannel);
  }

  @After
  public void tearDown() {
    if (server != null) {
      server.shutdown();
    }
    if (clientChannel != null) {
      clientChannel.shutdown();
    }

    MoreExecutors.shutdownAndAwaitTermination(serverExecutor, 5, TimeUnit.SECONDS);
    MoreExecutors.shutdownAndAwaitTermination(clientExecutor, 5, TimeUnit.SECONDS);
  }

  /**
   * Tests that gRPC can handle concurrent server-streaming RPCs.
   */
  @Test(timeout = 10000)
  public void serverStreamingTest() throws Exception {
    CyclicBarrier startBarrier = new CyclicBarrier(NUM_CONCURRENT_REQUESTS);
    CountDownLatch responsesDoneSignal = new CountDownLatch(NUM_CONCURRENT_REQUESTS);

    for (int i = 0; i < NUM_CONCURRENT_REQUESTS; i++) {
      clientExecutor.execute(new ClientWorker(startBarrier, responsesDoneSignal));
    }

    // Wait until the clients all receive their complete RPC response streams.
    responsesDoneSignal.await();
  }

  /**
   * Creates and starts a new {@link TestServiceImpl} server.
   */
  private Server newServer() throws CertificateException, IOException {
    File serverCertChainFile = TestUtils.loadCert("server1.pem");
    File serverPrivateKeyFile = TestUtils.loadCert("server1.key");
    X509Certificate[] serverTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };

    SslContext sslContext =
        GrpcSslContexts.forServer(serverCertChainFile, serverPrivateKeyFile)
                       .trustManager(serverTrustedCaCerts)
                       .clientAuth(ClientAuth.REQUIRE)
                       .build();

    return NettyServerBuilder.forPort(0)
        .sslContext(sslContext)
        .addService(new TestServiceImpl(serverExecutor))
        .build()
        .start();
  }

  private ManagedChannel newClientChannel() throws CertificateException, IOException {
    File clientCertChainFile = TestUtils.loadCert("client.pem");
    File clientPrivateKeyFile = TestUtils.loadCert("client.key");
    X509Certificate[] clientTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };

    SslContext sslContext =
        GrpcSslContexts.forClient()
                       .keyManager(clientCertChainFile, clientPrivateKeyFile)
                       .trustManager(clientTrustedCaCerts)
                       .build();

    return NettyChannelBuilder.forAddress("localhost", server.getPort())
        .overrideAuthority(TestUtils.TEST_SERVER_HOST)
        .negotiationType(NegotiationType.TLS)
        .sslContext(sslContext)
        .build();
  }
}
