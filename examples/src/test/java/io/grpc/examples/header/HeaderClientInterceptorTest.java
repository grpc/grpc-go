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

package io.grpc.examples.header;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.grpc.ClientInterceptors;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.ServerCall;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.StatusRuntimeException;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.GreeterGrpc.GreeterBlockingStub;
import io.grpc.examples.helloworld.GreeterGrpc.GreeterImplBase;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.testing.GrpcCleanupRule;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.ArgumentMatchers;

/**
 * Unit tests for {@link HeaderClientInterceptor}.
 * For demonstrating how to write gRPC unit test only.
 * Not intended to provide a high code coverage or to test every major usecase.
 *
 * <p>For basic unit test examples see {@link io.grpc.examples.helloworld.HelloWorldClientTest} and
 * {@link io.grpc.examples.helloworld.HelloWorldServerTest}.
 */
@RunWith(JUnit4.class)
public class HeaderClientInterceptorTest {
  /**
   * This rule manages automatic graceful shutdown for the registered servers and channels at the
   * end of test.
   */
  @Rule
  public final GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

  private final ServerInterceptor mockServerInterceptor = mock(ServerInterceptor.class, delegatesTo(
      new ServerInterceptor() {
        @Override
        public <ReqT, RespT> Listener<ReqT> interceptCall(
            ServerCall<ReqT, RespT> call, Metadata headers, ServerCallHandler<ReqT, RespT> next) {
          return next.startCall(call, headers);
        }
      }));

  @Test
  public void clientHeaderDeliveredToServer() throws Exception {
    // Generate a unique in-process server name.
    String serverName = InProcessServerBuilder.generateName();
    // Create a server, add service, start, and register for automatic graceful shutdown.
    grpcCleanup.register(InProcessServerBuilder.forName(serverName).directExecutor()
        .addService(ServerInterceptors.intercept(new GreeterImplBase() {}, mockServerInterceptor))
        .build().start());
    // Create a client channel and register for automatic graceful shutdown.
    ManagedChannel channel = grpcCleanup.register(
        InProcessChannelBuilder.forName(serverName).directExecutor().build());
    GreeterBlockingStub blockingStub = GreeterGrpc.newBlockingStub(
        ClientInterceptors.intercept(channel, new HeaderClientInterceptor()));
    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);

    try {
      blockingStub.sayHello(HelloRequest.getDefaultInstance());
      fail();
    } catch (StatusRuntimeException expected) {
      // expected because the method is not implemented at server side
    }

    verify(mockServerInterceptor).interceptCall(
        ArgumentMatchers.<ServerCall<HelloRequest, HelloReply>>any(),
        metadataCaptor.capture(),
        ArgumentMatchers.<ServerCallHandler<HelloRequest, HelloReply>>any());
    assertEquals(
        "customRequestValue",
        metadataCaptor.getValue().get(HeaderClientInterceptor.CUSTOM_HEADER_KEY));
  }
}
