/*
 * Copyright 2018, gRPC Authors All rights reserved.
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

import io.grpc.alts.AltsServerBuilder;
import io.grpc.Server;
import io.grpc.examples.helloworld.GreeterGrpc.GreeterImplBase;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.stub.StreamObserver;
import java.io.IOException;
import java.util.concurrent.Executors;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * An example gRPC server that uses ALTS. Shows how to do a Unary RPC. This example can only be run
 * on Google Cloud Platform.
 */
public final class HelloWorldAltsServer extends GreeterImplBase {
  private static final Logger logger = Logger.getLogger(HelloWorldAltsServer.class.getName());
  private Server server;
  private int port = 10001;

  public static void main(String[] args) throws IOException, InterruptedException {
    new HelloWorldAltsServer().start(args);
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
      if ("port".equals(key)) {
        port = Integer.parseInt(value);
      } else {
        System.err.println("Unknown argument: " + key);
        usage = true;
        break;
      }
    }
    if (usage) {
      HelloWorldAltsServer s = new HelloWorldAltsServer();
      System.out.println(
          "Usage: [ARGS...]"
              + "\n"
              + "\n  --port=PORT           Server port to bind to. Default "
              + s.port);
      System.exit(1);
    }
  }

  private void start(String[] args) throws IOException, InterruptedException {
    parseArgs(args);
    server =
        AltsServerBuilder.forPort(port)
            .addService(this)
            .executor(Executors.newFixedThreadPool(1))
            .build();
    server.start();
    logger.log(Level.INFO, "Started on {0}", port);
    server.awaitTermination();
  }

  @Override
  public void sayHello(HelloRequest request, StreamObserver<HelloReply> observer) {
    observer.onNext(HelloReply.newBuilder().setMessage("Hello, " + request.getName()).build());
    observer.onCompleted();
  }
}
