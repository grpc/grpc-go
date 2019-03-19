/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.examples.authentication;

import static org.junit.Assert.assertEquals;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.grpc.Metadata;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptors;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerInterceptor;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcCleanupRule;
import java.io.IOException;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.ArgumentMatchers;

/**
 * Unit tests for {@link AuthClient} testing the default and non-default tokens
 *
 *
 */
@RunWith(JUnit4.class)
public class AuthClientTest {
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

  private AuthClient client;


  @Before
  public void setUp() throws IOException {
    // Generate a unique in-process server name.
    String serverName = InProcessServerBuilder.generateName();

    // Create a server, add service, start, and register for automatic graceful shutdown.
    grpcCleanup.register(InProcessServerBuilder.forName(serverName).directExecutor()
        .addService(ServerInterceptors.intercept(
            new GreeterGrpc.GreeterImplBase() {

              @Override
              public void sayHello(
                  HelloRequest request, StreamObserver<HelloReply> responseObserver) {
                HelloReply reply = HelloReply.newBuilder()
                    .setMessage("AuthClientTest user=" + request.getName()).build();
                responseObserver.onNext(reply);
                responseObserver.onCompleted();
              }
            },
            mockServerInterceptor))
        .build().start());

    // Create an AuthClient using the in-process channel;
    client = new AuthClient(InProcessChannelBuilder.forName(serverName).directExecutor());
  }

  /**
   * Test default JWT token used.
   */
  @Test
  public void defaultTokenDeliveredToServer() {
    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
    String retVal = client.greet("default token test");

    verify(mockServerInterceptor).interceptCall(
        ArgumentMatchers.<ServerCall<HelloRequest, HelloReply>>any(),
        metadataCaptor.capture(),
        ArgumentMatchers.<ServerCallHandler<HelloRequest, HelloReply>>any());
    assertEquals(
        "my-default-token",
        metadataCaptor.getValue().get(Constant.JWT_METADATA_KEY));
    assertEquals("AuthClientTest user=default token test", retVal);
  }

  /**
   * Test non-default JWT token used.
   */
  @Test
  public void nonDefaultTokenDeliveredToServer() {
    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);

    client.setTokenValue("non-default-token");
    String retVal = client.greet("non default token test");

    verify(mockServerInterceptor).interceptCall(
        ArgumentMatchers.<ServerCall<HelloRequest, HelloReply>>any(),
        metadataCaptor.capture(),
        ArgumentMatchers.<ServerCallHandler<HelloRequest, HelloReply>>any());
    assertEquals(
        "non-default-token",
        metadataCaptor.getValue().get(Constant.JWT_METADATA_KEY));
    assertEquals("AuthClientTest user=non default token test", retVal);
  }
}
