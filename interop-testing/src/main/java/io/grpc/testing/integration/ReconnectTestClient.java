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

package io.grpc.testing.integration;

import static org.junit.Assert.assertTrue;

import io.grpc.ManagedChannel;
import io.grpc.StatusRuntimeException;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.okhttp.OkHttpChannelBuilder;
import io.grpc.testing.integration.EmptyProtos.Empty;
import io.grpc.testing.integration.Messages.ReconnectInfo;

/**
 * Verifies the client is reconnecting the server with correct backoffs
 *
 * <p>See the <a href="https://github.com/grpc/grpc/blob/master/doc/connection-backoff-interop-test-description.md">Test Spec</a>.
 */
public class ReconnectTestClient {
  private static final int TEST_TIME_MS = 540 * 1000;

  private int serverControlPort = 8080;
  private int serverRetryPort = 8081;
  private boolean useOkhttp = false;
  private ManagedChannel controlChannel;
  private ManagedChannel retryChannel;
  private ReconnectServiceGrpc.ReconnectServiceBlockingStub controlStub;
  private ReconnectServiceGrpc.ReconnectServiceBlockingStub retryStub;

  private void parseArgs(String[] args) {
    for (String arg : args) {
      if (!arg.startsWith("--")) {
        System.err.println("All arguments must start with '--': " + arg);
        System.exit(1);
      }
      String[] parts = arg.substring(2).split("=", 2);
      String key = parts[0];
      String value = parts[1];
      if ("server_control_port".equals(key)) {
        serverControlPort = Integer.parseInt(value);
      } else if ("server_retry_port".equals(key)) {
        serverRetryPort = Integer.parseInt(value);
      } else if ("use_okhttp".equals(key)) {
        useOkhttp = Boolean.parseBoolean(value);
      } else {
        System.err.println("Unknown argument: " + key);
        System.exit(1);
      }
    }
  }

  private void runTest() throws Exception {
    try {
      controlChannel = NettyChannelBuilder.forAddress("127.0.0.1", serverControlPort)
          .negotiationType(NegotiationType.PLAINTEXT).build();
      controlStub = ReconnectServiceGrpc.newBlockingStub(controlChannel);
      if (useOkhttp) {
        retryChannel =
            OkHttpChannelBuilder.forAddress("127.0.0.1", serverRetryPort)
                .useTransportSecurity()
                .build();
      } else {
        retryChannel = NettyChannelBuilder.forAddress("127.0.0.1", serverRetryPort)
            .negotiationType(NegotiationType.TLS).build();
      }
      retryStub = ReconnectServiceGrpc.newBlockingStub(retryChannel);
      controlStub.start(Empty.getDefaultInstance());

      long startTimeStamp = System.currentTimeMillis();
      while ((System.currentTimeMillis() - startTimeStamp) < TEST_TIME_MS) {
        try {
          retryStub.start(Empty.getDefaultInstance());
        } catch (StatusRuntimeException expected) {
          // Make CheckStyle happy.
        }
        Thread.sleep(50);
      }
      ReconnectInfo info = controlStub.stop(Empty.getDefaultInstance());
      assertTrue(info.getPassed());
    } finally {
      controlChannel.shutdownNow();
      retryChannel.shutdownNow();
    }
  }

  /**
   * The main application allowing this client to be launched from the command line.
   */
  public static void main(String[] args) {
    ReconnectTestClient client = new ReconnectTestClient();
    client.parseArgs(args);
    System.out.println("Starting test:");
    try {
      client.runTest();
      System.out.println("Finished successfully");
      System.exit(0);
    } catch (Throwable e) {
      e.printStackTrace();
      System.err.println("Test failed!");
      System.exit(1);
    }
  }
}
