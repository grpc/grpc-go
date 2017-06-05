/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

import com.google.protobuf.ByteString;
import com.google.protobuf.EmptyProtos;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.integration.Messages;
import io.grpc.testing.integration.TestServiceGrpc;
import java.util.Collection;
import java.util.UUID;
import java.util.concurrent.ConcurrentLinkedQueue;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.junit.runners.model.Statement;

public class GrpcServerRuleTest {

  @RunWith(JUnit4.class)
  public static class WithoutDirectExecutor {

    @Rule
    public final GrpcServerRule grpcServerRule = new GrpcServerRule();

    @Test
    public void serverAndChannelAreStarted() {
      assertThat(grpcServerRule.getServer().isShutdown()).isFalse();
      assertThat(grpcServerRule.getServer().isTerminated()).isFalse();

      assertThat(grpcServerRule.getChannel().isShutdown()).isFalse();
      assertThat(grpcServerRule.getChannel().isTerminated()).isFalse();

      assertThat(grpcServerRule.getServerName()).isNotNull();
      assertThat(grpcServerRule.getServiceRegistry()).isNotNull();
    }

    @Test
    public void serverAllowsServicesToBeAddedViaServiceRegistry() {
      TestServiceImpl testService = new TestServiceImpl();

      grpcServerRule.getServiceRegistry().addService(testService);

      TestServiceGrpc.TestServiceBlockingStub stub =
          TestServiceGrpc.newBlockingStub(grpcServerRule.getChannel());

      Messages.SimpleRequest request1 = Messages.SimpleRequest.newBuilder()
          .setPayload(Messages.Payload.newBuilder()
                          .setBody(ByteString.copyFromUtf8(UUID.randomUUID().toString())))
          .build();

      Messages.SimpleRequest request2 = Messages.SimpleRequest.newBuilder()
          .setPayload(Messages.Payload.newBuilder()
                          .setBody(ByteString.copyFromUtf8(UUID.randomUUID().toString())))
          .build();

      stub.unaryCall(request1);
      stub.unaryCall(request2);

      assertThat(testService.unaryCallRequests)
          .containsExactly(request1, request2);
    }

    @Test
    public void serviceIsNotRunOnSameThreadAsTest() {
      TestServiceImpl testService = new TestServiceImpl();

      grpcServerRule.getServiceRegistry().addService(testService);

      TestServiceGrpc.TestServiceBlockingStub stub =
          TestServiceGrpc.newBlockingStub(grpcServerRule.getChannel());

      // Make a garbage request first due to https://github.com/grpc/grpc-java/issues/2444.
      stub.emptyCall(EmptyProtos.Empty.newBuilder().build());
      stub.emptyCall(EmptyProtos.Empty.newBuilder().build());

      assertThat(testService.lastEmptyCallRequestThread).isNotEqualTo(Thread.currentThread());
    }

    @Test(expected = IllegalStateException.class)
    public void callDirectExecutorNotAtRuleInstantiation() {
      grpcServerRule.directExecutor();
    }
  }

  @RunWith(JUnit4.class)
  public static class WithDirectExecutor {

    @Rule
    public final GrpcServerRule grpcServerRule = new GrpcServerRule().directExecutor();

    @Test
    public void serverAndChannelAreStarted() {
      assertThat(grpcServerRule.getServer().isShutdown()).isFalse();
      assertThat(grpcServerRule.getServer().isTerminated()).isFalse();

      assertThat(grpcServerRule.getChannel().isShutdown()).isFalse();
      assertThat(grpcServerRule.getChannel().isTerminated()).isFalse();

      assertThat(grpcServerRule.getServerName()).isNotNull();
      assertThat(grpcServerRule.getServiceRegistry()).isNotNull();
    }

    @Test
    public void serverAllowsServicesToBeAddedViaServiceRegistry() {
      TestServiceImpl testService = new TestServiceImpl();

      grpcServerRule.getServiceRegistry().addService(testService);

      TestServiceGrpc.TestServiceBlockingStub stub =
          TestServiceGrpc.newBlockingStub(grpcServerRule.getChannel());

      Messages.SimpleRequest request1 = Messages.SimpleRequest.newBuilder()
          .setPayload(Messages.Payload.newBuilder()
                          .setBody(ByteString.copyFromUtf8(UUID.randomUUID().toString())))
          .build();

      Messages.SimpleRequest request2 = Messages.SimpleRequest.newBuilder()
          .setPayload(Messages.Payload.newBuilder()
                          .setBody(ByteString.copyFromUtf8(UUID.randomUUID().toString())))
          .build();

      stub.unaryCall(request1);
      stub.unaryCall(request2);

      assertThat(testService.unaryCallRequests)
          .containsExactly(request1, request2);
    }

    @Test
    public void serviceIsRunOnSameThreadAsTest() {
      TestServiceImpl testService = new TestServiceImpl();

      grpcServerRule.getServiceRegistry().addService(testService);

      TestServiceGrpc.TestServiceBlockingStub stub =
          TestServiceGrpc.newBlockingStub(grpcServerRule.getChannel());

      // Make a garbage request first due to https://github.com/grpc/grpc-java/issues/2444.
      stub.emptyCall(EmptyProtos.Empty.newBuilder().build());
      stub.emptyCall(EmptyProtos.Empty.newBuilder().build());

      assertThat(testService.lastEmptyCallRequestThread).isEqualTo(Thread.currentThread());
    }
  }

  @RunWith(JUnit4.class)
  public static class ResourceCleanup {

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
  }

  private static class TestServiceImpl extends TestServiceGrpc.TestServiceImplBase {

    private final Collection<Messages.SimpleRequest> unaryCallRequests =
        new ConcurrentLinkedQueue<Messages.SimpleRequest>();

    private volatile Thread lastEmptyCallRequestThread;

    @Override
    public void emptyCall(
        EmptyProtos.Empty request,
        StreamObserver<EmptyProtos.Empty> responseObserver) {

      lastEmptyCallRequestThread = Thread.currentThread();

      responseObserver.onNext(EmptyProtos.Empty.newBuilder().build());

      responseObserver.onCompleted();
    }

    @Override
    public void unaryCall(
        Messages.SimpleRequest request,
        StreamObserver<Messages.SimpleResponse> responseObserver) {

      unaryCallRequests.add(request);

      responseObserver.onNext(Messages.SimpleResponse.newBuilder().build());

      responseObserver.onCompleted();
    }
  }
}
