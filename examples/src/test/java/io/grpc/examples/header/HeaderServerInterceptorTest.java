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

package io.grpc.examples.header;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerInterceptors;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.GreeterGrpc.GreeterBlockingStub;
import io.grpc.examples.helloworld.GreeterGrpc.GreeterImplBase;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcServerRule;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

/**
 * Unit tests for {@link HeaderClientInterceptor}.
 * For demonstrating how to write gRPC unit test only.
 * Not intended to provide a high code coverage or to test every major usecase.
 *
 * <p>For basic unit test examples see {@link io.grpc.examples.helloworld.HelloWorldClientTest} and
 * {@link io.grpc.examples.helloworld.HelloWorldServerTest}.
 */
@RunWith(JUnit4.class)
public class HeaderServerInterceptorTest {
  /**
   * This creates and starts an in-process server, and creates a client with an in-process channel.
   * When the test is done, it also shuts down the in-process client and server.
   */
  @Rule
  public final GrpcServerRule grpcServerRule = new GrpcServerRule().directExecutor();

  @Before
  public void setUp() throws Exception {
    GreeterImplBase greeterImplBase =
        new GreeterImplBase() {
          @Override
          public void sayHello(HelloRequest request, StreamObserver<HelloReply> responseObserver) {
            responseObserver.onNext(HelloReply.getDefaultInstance());
            responseObserver.onCompleted();
          }
        };
    grpcServerRule.getServiceRegistry()
        .addService(ServerInterceptors.intercept(greeterImplBase, new HeaderServerInterceptor()));
  }

  @Test
  public void serverHeaderDeliveredToClient() {
    class SpyingClientInterceptor implements ClientInterceptor {
      ClientCall.Listener<?> spyListener;

      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
        return new SimpleForwardingClientCall<ReqT, RespT>(next.newCall(method, callOptions)) {
          @Override
          public void start(Listener<RespT> responseListener, Metadata headers) {
            spyListener = responseListener =
                mock(ClientCall.Listener.class, delegatesTo(responseListener));
            super.start(responseListener, headers);
          }
        };
      }
    }

    SpyingClientInterceptor clientInterceptor = new SpyingClientInterceptor();
    GreeterBlockingStub blockingStub = GreeterGrpc.newBlockingStub(grpcServerRule.getChannel())
        .withInterceptors(clientInterceptor);
    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);

    blockingStub.sayHello(HelloRequest.getDefaultInstance());

    assertNotNull(clientInterceptor.spyListener);
    verify(clientInterceptor.spyListener).onHeaders(metadataCaptor.capture());
    assertEquals(
        "customRespondValue",
        metadataCaptor.getValue().get(HeaderServerInterceptor.CUSTOM_HEADER_KEY));
  }
}
