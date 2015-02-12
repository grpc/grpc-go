/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.testing.integration;

import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.ServerImpl;
import io.grpc.ServerInterceptors;
import io.grpc.testing.TestUtils;
import io.grpc.transport.netty.NettyServerBuilder;
import io.netty.handler.ssl.SslContext;

import java.io.File;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

/**
 * Server that manages startup/shutdown of a single {@code TestService}.
 */
public class TestServiceServer {
  /**
   * The main application allowing this server to be launched from the command line.
   */
  public static void main(String[] args) throws Exception {
    final TestServiceServer server = new TestServiceServer();
    server.parseArgs(args);
    if (server.useTls) {
      System.out.println(
          "\nUsing fake CA for TLS certificate. Test clients should expect host\n"
          + "*.test.google.fr and our test CA. For the Java test client binary, use:\n"
          + "--server_host_override=foo.test.google.fr --use_test_ca=true\n");
    }

    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        try {
          System.out.println("Shutting down");
          server.stop();
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });
    server.start();
    System.out.println("Server started on port " + server.port);
  }

  private int port = 8080;
  private boolean useTls = true;

  private ScheduledExecutorService executor;
  private ServerImpl server;

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
      } else if ("use_tls".equals(key)) {
        useTls = Boolean.parseBoolean(value);
      } else if ("grpc_version".equals(key)) {
        if (!"2".equals(value)) {
          System.err.println("Only grpc version 2 is supported");
          usage = true;
          break;
        }
      } else {
        System.err.println("Unknown argument: " + key);
        usage = true;
        break;
      }
    }
    if (usage) {
      TestServiceServer s = new TestServiceServer();
      System.out.println(
          "Usage: [ARGS...]"
          + "\n"
          + "\n  --port=PORT           Port to connect to. Default " + s.port
          + "\n  --use_tls=true|false  Whether to use TLS. Default " + s.useTls
          );
      System.exit(1);
    }
  }

  private void start() throws Exception {
    executor = Executors.newSingleThreadScheduledExecutor();
    SslContext sslContext = null;
    if (useTls) {
      sslContext = SslContext.newServerContext(Util.loadCert("server1.pem"),
                                               Util.loadCert("server1.key"));
    }
    server = NettyServerBuilder.forPort(port)
        .sslContext(sslContext)
        .addService(ServerInterceptors.intercept(
            TestServiceGrpc.bindService(new TestServiceImpl(executor)),
            TestUtils.echoRequestHeadersInterceptor(Util.METADATA_KEY)))
        .build().start();
  }

  private void stop() throws Exception {
    server.shutdownNow();
    if (!server.awaitTerminated(5, TimeUnit.SECONDS)) {
      System.err.println("Timed out waiting for server shutdown");
    }
    MoreExecutors.shutdownAndAwaitTermination(executor, 5, TimeUnit.SECONDS);
  }
}
