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

package io.grpc.testing;

import static com.google.common.truth.Truth.assertThat;

import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.protobuf.SimpleRequest;
import io.grpc.testing.protobuf.SimpleResponse;
import io.grpc.testing.protobuf.SimpleServiceGrpc;
import java.util.Collection;
import java.util.concurrent.ConcurrentLinkedQueue;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.junit.runners.model.Statement;

/** Unit tests for {@link GrpcServerRule}. */
@RunWith(JUnit4.class)
public class GrpcServerRuleTest {

  @Rule public final GrpcServerRule grpcServerRule1 = new GrpcServerRule();
  @Rule public final GrpcServerRule grpcServerRule2 = new GrpcServerRule().directExecutor();

  @Test
  public void serverAndChannelAreStarted_withoutDirectExecutor() {
    assertThat(grpcServerRule1.getServer().isShutdown()).isFalse();
    assertThat(grpcServerRule1.getServer().isTerminated()).isFalse();

    assertThat(grpcServerRule1.getChannel().isShutdown()).isFalse();
    assertThat(grpcServerRule1.getChannel().isTerminated()).isFalse();

    assertThat(grpcServerRule1.getServerName()).isNotNull();
    assertThat(grpcServerRule1.getServiceRegistry()).isNotNull();
  }

  @Test
  public void serverAllowsServicesToBeAddedViaServiceRegistry_withoutDirectExecutor() {
    TestServiceImpl testService = new TestServiceImpl();

    grpcServerRule1.getServiceRegistry().addService(testService);

    SimpleServiceGrpc.SimpleServiceBlockingStub stub =
        SimpleServiceGrpc.newBlockingStub(grpcServerRule1.getChannel());

    SimpleRequest request1 = SimpleRequest.getDefaultInstance();

    SimpleRequest request2 = SimpleRequest.newBuilder().build();

    stub.unaryRpc(request1);
    stub.unaryRpc(request2);

    assertThat(testService.unaryCallRequests).containsExactly(request1, request2);
  }

  @Test
  public void serviceIsNotRunOnSameThreadAsTest_withoutDirectExecutor() {
    TestServiceImpl testService = new TestServiceImpl();

    grpcServerRule1.getServiceRegistry().addService(testService);

    SimpleServiceGrpc.SimpleServiceBlockingStub stub =
        SimpleServiceGrpc.newBlockingStub(grpcServerRule1.getChannel());

    stub.serverStreamingRpc(SimpleRequest.getDefaultInstance());

    assertThat(testService.lastServerStreamingRpcThread).isNotEqualTo(Thread.currentThread());
  }

  @Test(expected = IllegalStateException.class)
  public void callDirectExecutorNotAtRuleInstantiation_withoutDirectExecutor() {
    grpcServerRule1.directExecutor();
  }

  @Test
  public void serverAndChannelAreStarted_withDirectExecutor() {
    assertThat(grpcServerRule2.getServer().isShutdown()).isFalse();
    assertThat(grpcServerRule2.getServer().isTerminated()).isFalse();

    assertThat(grpcServerRule2.getChannel().isShutdown()).isFalse();
    assertThat(grpcServerRule2.getChannel().isTerminated()).isFalse();

    assertThat(grpcServerRule2.getServerName()).isNotNull();
    assertThat(grpcServerRule2.getServiceRegistry()).isNotNull();
  }

  @Test
  public void serverAllowsServicesToBeAddedViaServiceRegistry_withDirectExecutor() {
    TestServiceImpl testService = new TestServiceImpl();

    grpcServerRule2.getServiceRegistry().addService(testService);

    SimpleServiceGrpc.SimpleServiceBlockingStub stub =
        SimpleServiceGrpc.newBlockingStub(grpcServerRule2.getChannel());

    SimpleRequest request1 = SimpleRequest.getDefaultInstance();

    SimpleRequest request2 = SimpleRequest.newBuilder().build();

    stub.unaryRpc(request1);
    stub.unaryRpc(request2);

    assertThat(testService.unaryCallRequests).containsExactly(request1, request2);
  }

  @Test
  public void serviceIsRunOnSameThreadAsTest_withDirectExecutor() {
    TestServiceImpl testService = new TestServiceImpl();

    grpcServerRule2.getServiceRegistry().addService(testService);

    SimpleServiceGrpc.SimpleServiceBlockingStub stub =
        SimpleServiceGrpc.newBlockingStub(grpcServerRule2.getChannel());

    stub.serverStreamingRpc(SimpleRequest.getDefaultInstance());

    assertThat(testService.lastServerStreamingRpcThread).isEqualTo(Thread.currentThread());
  }

  @Test
  public void serverAndChannelAreShutdownAfterRule() throws Throwable {
    GrpcServerRule grpcServerRule = new GrpcServerRule();

    // Before the rule has been executed, all of its resources should be null.
    assertThat(grpcServerRule.getChannel()).isNull();
    assertThat(grpcServerRule.getServer()).isNull();
    assertThat(grpcServerRule.getServerName()).isNull();
    assertThat(grpcServerRule.getServiceRegistry()).isNull();

    // The TestStatement stores the channel and server instances so that we can inspect them after
    // the rule cleans up.
    TestStatement statement = new TestStatement(grpcServerRule);

    grpcServerRule.apply(statement, null).evaluate();

    // Ensure that the stored channel and server instances were shut down.
    assertThat(statement.channel.isShutdown()).isTrue();
    assertThat(statement.server.isShutdown()).isTrue();

    // All references to the resources that we created should be set to null.
    assertThat(grpcServerRule.getChannel()).isNull();
    assertThat(grpcServerRule.getServer()).isNull();
    assertThat(grpcServerRule.getServerName()).isNull();
    assertThat(grpcServerRule.getServiceRegistry()).isNull();
  }

  private static class TestStatement extends Statement {

    private final GrpcServerRule grpcServerRule;

    private ManagedChannel channel;
    private Server server;

    private TestStatement(GrpcServerRule grpcServerRule) {
      this.grpcServerRule = grpcServerRule;
    }

    @Override
    public void evaluate() throws Throwable {
      channel = grpcServerRule.getChannel();
      server = grpcServerRule.getServer();
    }
  }

  private static class TestServiceImpl extends SimpleServiceGrpc.SimpleServiceImplBase {

    private final Collection<SimpleRequest> unaryCallRequests =
        new ConcurrentLinkedQueue<>();

    private volatile Thread lastServerStreamingRpcThread;

    @Override
    public void serverStreamingRpc(
        SimpleRequest request, StreamObserver<SimpleResponse> responseObserver) {

      lastServerStreamingRpcThread = Thread.currentThread();

      responseObserver.onNext(SimpleResponse.getDefaultInstance());

      responseObserver.onCompleted();
    }

    @Override
    public void unaryRpc(
        SimpleRequest request, StreamObserver<SimpleResponse> responseObserver) {

      unaryCallRequests.add(request);

      responseObserver.onNext(SimpleResponse.getDefaultInstance());

      responseObserver.onCompleted();
    }
  }
}
