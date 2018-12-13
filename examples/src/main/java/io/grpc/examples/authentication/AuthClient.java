/*
 * Copyright 2018 The gRPC Authors
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

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.StatusRuntimeException;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;

import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * An authenticating client that requests a greeting from the {@link AuthServer}.
 * It uses a {@link JwtClientInterceptor} to inject a JWT credential.
 */
public class AuthClient {
  private static final Logger logger = Logger.getLogger(AuthClient.class.getName());

  private final ManagedChannel channel;
  private final GreeterGrpc.GreeterBlockingStub blockingStub;
  private final JwtClientInterceptor jwtClientInterceptor = new JwtClientInterceptor();

  /** Construct client connecting to AuthServer at {@code host:port} using a client-interceptor
    *  to inject the JWT credentials
    */
  public AuthClient(String host, int port) {
    this(ManagedChannelBuilder.forAddress(host, port)
        // Channels are secure by default (via SSL/TLS). For this example we disable TLS to avoid
        // needing certificates, but it is recommended to use a secure channel while passing
        // credentials.
        .usePlaintext());
  }

  /** Construct client for accessing GreeterGrpc server using the existing channel. */
  AuthClient(ManagedChannelBuilder builder) {
    this.channel = builder
        .intercept(jwtClientInterceptor)
        .build();
    blockingStub = GreeterGrpc.newBlockingStub(channel);
  }

  public void shutdown() throws InterruptedException {
    channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
  }

  public void setTokenValue(String tokenValue) {
    jwtClientInterceptor.setTokenValue(tokenValue);
  }

  /**
   * Say hello to server.
   *
   * @param name  name to set in HelloRequest
   * @return the message in the HelloReply from the server
   */
  public String greet(String name) {
    logger.info("Will try to greet " + name + " ...");
    HelloRequest request = HelloRequest.newBuilder().setName(name).build();
    HelloReply response;
    try {
      response = blockingStub.sayHello(request);
    } catch (StatusRuntimeException e) {
      logger.log(Level.WARNING, "RPC failed: {0}", e.getStatus());
      return e.toString();
    }
    logger.info("Greeting: " + response.getMessage());
    return response.getMessage();
  }

  /**
   * Greet server. If provided, the first element of {@code args} is the name to use in the
   * greeting.
   */
  public static void main(String[] args) throws Exception {

    AuthClient client = new AuthClient("localhost", 50051);
    try {
      /* Access a service running on the local machine on port 50051 */
      String user = "world";
      if (args.length > 0) {
        user = args[0]; /* Use the arg as the name to greet if provided */
      }
      if (args.length > 1) {
        client.setTokenValue(args[1]);
      }
      client.greet(user);
    } finally {
      client.shutdown();
    }
  }
}
