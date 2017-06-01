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

package io.grpc.examples.helloworld;

import static org.junit.Assert.assertEquals;

import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.examples.helloworld.HelloWorldServer.GreeterImpl;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link HelloWorldServer}.
 * For demonstrating how to write gRPC unit test only.
 * Not intended to provide a high code coverage or to test every major usecase.
 *
 * <p>For more unit test examples see {@link io.grpc.examples.routeguide.RouteGuideClientTest} and
 * {@link io.grpc.examples.routeguide.RouteGuideServerTest}.
 */
@RunWith(JUnit4.class)
public class HelloWorldServerTest {
  private static final String UNIQUE_SERVER_NAME =
      "in-process server for " + HelloWorldServerTest.class;
  private final Server inProcessServer = InProcessServerBuilder
      .forName(UNIQUE_SERVER_NAME).addService(new GreeterImpl()).directExecutor().build();
  private final ManagedChannel inProcessChannel =
      InProcessChannelBuilder.forName(UNIQUE_SERVER_NAME).directExecutor().build();

  /**
   * Creates and starts the server with the {@link InProcessServerBuilder},
   * and creates an in-process channel with the {@link InProcessChannelBuilder}.
   */
  @Before
  public void setUp() throws Exception {
    inProcessServer.start();
  }

  /**
   * Shuts down the in-process channel and server.
   */
  @After
  public void tearDown() {
    inProcessChannel.shutdownNow();
    inProcessServer.shutdownNow();
  }

  /**
   * To test the server, make calls with a real stub using the in-process channel, and verify
   * behaviors or state changes from the client side.
   */
  @Test
  public void greeterImpl_replyMessage() throws Exception {
    GreeterGrpc.GreeterBlockingStub blockingStub = GreeterGrpc.newBlockingStub(inProcessChannel);
    String testName = "test name";

    HelloReply reply = blockingStub.sayHello(HelloRequest.newBuilder().setName(testName).build());

    assertEquals("Hello " + testName, reply.getMessage());
  }
}
