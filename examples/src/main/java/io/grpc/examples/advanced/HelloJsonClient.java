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

package io.grpc.examples.advanced;

import static io.grpc.stub.ClientCalls.blockingUnaryCall;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.MethodDescriptor;
import io.grpc.StatusRuntimeException;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.examples.helloworld.HelloWorldClient;
import io.grpc.protobuf.ProtoUtils;
import io.grpc.stub.AbstractStub;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Advanced example of how to swap out the serialization logic.  Normal users do not need to do
 * this.  This code is not intended to be a production-ready implementation, since JSON encoding
 * is slow.  Additionally, JSON serialization as implemented may be not resilient to malicious
 * input.
 *
 * <p>If you are considering implementing your own serialization logic, contact the grpc team at
 * https://groups.google.com/forum/#!forum/grpc-io
 */
public final class HelloJsonClient {
  private static final Logger logger = Logger.getLogger(HelloWorldClient.class.getName());

  private final ManagedChannel channel;
  private final HelloJsonStub blockingStub;

  /** Construct client connecting to HelloWorld server at {@code host:port}. */
  public HelloJsonClient(String host, int port) {
    channel = ManagedChannelBuilder.forAddress(host, port)
        .usePlaintext(true)
        .build();
    blockingStub = new HelloJsonStub(channel);
  }

  public void shutdown() throws InterruptedException {
    channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
  }

  /** Say hello to server. */
  public void greet(String name) {
    logger.info("Will try to greet " + name + " ...");
    HelloRequest request = HelloRequest.newBuilder().setName(name).build();
    HelloReply response;
    try {
      response = blockingStub.sayHello(request);
    } catch (StatusRuntimeException e) {
      logger.log(Level.WARNING, "RPC failed: {0}", e.getStatus());
      return;
    }
    logger.info("Greeting: " + response.getMessage());
  }

  /**
   * Greet server. If provided, the first element of {@code args} is the name to use in the
   * greeting.
   */
  public static void main(String[] args) throws Exception {
    HelloJsonClient client = new HelloJsonClient("localhost", 50051);
    try {
      /* Access a service running on the local machine on port 50051 */
      String user = "world";
      if (args.length > 0) {
        user = args[0]; /* Use the arg as the name to greet if provided */
      }
      client.greet(user);
    } finally {
      client.shutdown();
    }
  }

  static final class HelloJsonStub extends AbstractStub<HelloJsonStub> {

    static final MethodDescriptor<HelloRequest, HelloReply> METHOD_SAY_HELLO =
        GreeterGrpc.getSayHelloMethod()
            .toBuilder(
                ProtoUtils.jsonMarshaller(HelloRequest.getDefaultInstance()),
                ProtoUtils.jsonMarshaller(HelloReply.getDefaultInstance()))
            .build();

    protected HelloJsonStub(Channel channel) {
      super(channel);
    }

    protected HelloJsonStub(Channel channel, CallOptions callOptions) {
      super(channel, callOptions);
    }

    @Override
    protected HelloJsonStub build(Channel channel, CallOptions callOptions) {
      return new HelloJsonStub(channel, callOptions);
    }

    public HelloReply sayHello(HelloRequest request) {
      return blockingUnaryCall(
          getChannel(), METHOD_SAY_HELLO, getCallOptions(), request);
    }
  }
}

