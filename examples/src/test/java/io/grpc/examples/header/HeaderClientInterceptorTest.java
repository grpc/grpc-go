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
import static org.junit.Assert.fail;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.verify;

import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Server;
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
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;

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

  private final ServerInterceptor mockServerInterceptor = spy(
      new ServerInterceptor() {
        @Override
        public <ReqT, RespT> Listener<ReqT> interceptCall(
            ServerCall<ReqT, RespT> call, Metadata headers, ServerCallHandler<ReqT, RespT> next) {
          return next.startCall(call, headers);
        }
      });

  private Server fakeServer;
  private ManagedChannel inProcessChannel;

  @Before
  public void setUp() throws Exception {
    String uniqueServerName = "fake server for " + getClass();
    fakeServer = InProcessServerBuilder.forName(uniqueServerName)
        .addService(ServerInterceptors.intercept(new GreeterImplBase() {}, mockServerInterceptor))
        .directExecutor()
        .build()
        .start();
    inProcessChannel = InProcessChannelBuilder.forName(uniqueServerName)
        .intercept(new HeaderClientInterceptor())
        .directExecutor()
        .build();
  }

  @After
  public void tearDown() {
    inProcessChannel.shutdownNow();
    fakeServer.shutdownNow();
  }

  @Test
  public void clientHeaderDeliveredToServer() {
    GreeterBlockingStub blockingStub = GreeterGrpc.newBlockingStub(inProcessChannel);
    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);

    try {
      blockingStub.sayHello(HelloRequest.getDefaultInstance());
      fail();
    } catch (StatusRuntimeException expected) {
      // expected because the method is not implemented at server side
    }

    verify(mockServerInterceptor).interceptCall(
        Matchers.<ServerCall<HelloRequest, HelloReply>>any(),
        metadataCaptor.capture(),
        Matchers.<ServerCallHandler<HelloRequest, HelloReply>>any());
    assertEquals(
        "customRequestValue",
        metadataCaptor.getValue().get(HeaderClientInterceptor.CUSTOM_HEADER_KEY));
  }
}
