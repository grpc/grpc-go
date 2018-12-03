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

package io.grpc.examples.alts;

import io.grpc.alts.AltsChannelBuilder;
import io.grpc.ManagedChannel;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * An example gRPC client that uses ALTS. Shows how to do a Unary RPC. This example can only be run
 * on Google Cloud Platform.
 */
public final class HelloWorldAltsClient {
  private static final Logger logger = Logger.getLogger(HelloWorldAltsClient.class.getName());
  private String serverAddress = "localhost:10001";

  public static void main(String[] args) throws InterruptedException {
    new HelloWorldAltsClient().run(args);
  }

  private void parseArgs(String[] args) {
    boolean usage = false;
    for (String arg : args) {
      if (!arg.startsWith("--")) {
        System.err.println("All arguments must start with '--': " + arg);
        usage = true;
        break;
      }
      String[] parts = arg.substring(2).split("=", 2);
      String key = parts[0];
      if ("help".equals(key)) {
        usage = true;
        break;
      }
      if (parts.length != 2) {
        System.err.println("All arguments must be of the form --arg=value");
        usage = true;
        break;
      }
      String value = parts[1];
      if ("server".equals(key)) {
        serverAddress = value;
      } else {
        System.err.println("Unknown argument: " + key);
        usage = true;
        break;
      }
    }
    if (usage) {
      HelloWorldAltsClient c = new HelloWorldAltsClient();
      System.out.println(
          "Usage: [ARGS...]"
              + "\n"
              + "\n  --server=SERVER_ADDRESS        Server address to connect to. Default "
              + c.serverAddress);
      System.exit(1);
    }
  }

  private void run(String[] args) throws InterruptedException {
    parseArgs(args);
    ExecutorService executor = Executors.newFixedThreadPool(1);
    ManagedChannel channel = AltsChannelBuilder.forTarget(serverAddress).executor(executor).build();
    try {
      GreeterGrpc.GreeterBlockingStub stub = GreeterGrpc.newBlockingStub(channel);
      HelloReply resp = stub.sayHello(HelloRequest.newBuilder().setName("Waldo").build());

      logger.log(Level.INFO, "Got {0}", resp);
    } finally {
      channel.shutdown();
      channel.awaitTermination(1, TimeUnit.SECONDS);
      // Wait until the channel has terminated, since tasks can be queued after the channel is
      // shutdown.
      executor.shutdown();
    }
  }
}
