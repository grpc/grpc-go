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

import io.grpc.ChannelImpl;
import io.grpc.transport.netty.NegotiationType;
import io.grpc.transport.netty.NettyChannelBuilder;
import io.grpc.transport.okhttp.OkHttpChannelBuilder;
import io.netty.handler.ssl.SslContext;

import java.io.File;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.UnknownHostException;

import javax.net.ssl.SSLException;

/**
 * Application that starts a client for the {@link TestServiceGrpc.TestService} and runs through a
 * series of tests.
 */
public class TestServiceClient {
  /**
   * The main application allowing this client to be launched from the command line.
   */
  public static void main(String[] args) throws Exception {
    final TestServiceClient client = new TestServiceClient();
    client.parseArgs(args);
    client.setup();

    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        System.out.println("Shutting down");
        try {
          client.teardown();
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });

    try {
      client.run();
    } finally {
      client.teardown();
    }
  }

  private String serverHost = "localhost";
  private String serverHostOverride;
  private int serverPort = 8080;
  private String testCase = "empty_unary";
  private boolean useTls = true;
  private boolean useTestCa;
  private boolean useOkHttp;

  private Tester tester = new Tester();

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
      if ("server_host".equals(key)) {
        serverHost = value;
      } else if ("server_host_override".equals(key)) {
        serverHostOverride = value;
      } else if ("server_port".equals(key)) {
        serverPort = Integer.parseInt(value);
      } else if ("test_case".equals(key)) {
        testCase = value;
      } else if ("use_tls".equals(key)) {
        useTls = Boolean.parseBoolean(value);
      } else if ("use_test_ca".equals(key)) {
        useTestCa = Boolean.parseBoolean(value);
      } else if ("use_okhttp".equals(key)) {
        useOkHttp = Boolean.parseBoolean(value);
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
      TestServiceClient c = new TestServiceClient();
      System.out.println(
          "Usage: [ARGS...]"
          + "\n"
          + "\n  --server_host=HOST          Server to connect to. Default " + c.serverHost
          + "\n  --server_host_override=HOST Claimed identification expected of server."
          + "\n                              Defaults to server host"
          + "\n  --server_port=PORT          Port to connect to. Default " + c.serverPort
          + "\n  --test_case=TESTCASE        Test case to run. Default " + c.testCase
          + "\n  --use_tls=true|false        Whether to use TLS. Default " + c.useTls
          + "\n  --use_test_ca=true|false    Whether to trust our fake CA. Default " + c.useTestCa
          + "\n  --use_okhttp=true|false     Whether to use OkHttp instead of Netty. Default "
            + c.useOkHttp
          );
      System.exit(1);
    }
  }

  private void setup() {
    tester.setup();
  }

  private synchronized void teardown() {
    try {
      tester.teardown();
    } catch (RuntimeException ex) {
      throw ex;
    } catch (Exception ex) {
      throw new RuntimeException(ex);
    }
  }

  private void run() {
    System.out.println("Running test " + testCase);
    try {
      runTest(testCase);
    } catch (RuntimeException ex) {
      throw ex;
    } catch (Exception ex) {
      throw new RuntimeException(ex);
    }
    System.out.println("Test completed.");
  }

  private void runTest(String testCase) throws Exception {
    if ("empty_unary".equals(testCase)) {
      tester.emptyUnary();
    } else if ("large_unary".equals(testCase)) {
      tester.largeUnary();
    } else if ("client_streaming".equals(testCase)) {
      tester.clientStreaming();
    } else if ("server_streaming".equals(testCase)) {
      tester.serverStreaming();
    } else if ("ping_pong".equals(testCase)) {
      tester.pingPong();
    } else if ("empty_stream".equals(testCase)) {
      tester.emptyStream();
    } else if ("cancel_after_begin".equals(testCase)) {
      tester.cancelAfterBegin();
    } else if ("cancel_after_first_response".equals(testCase)) {
      tester.cancelAfterFirstResponse();
    } else {
      throw new IllegalArgumentException("Unknown test case: " + testCase);
    }
  }

  private class Tester extends AbstractTransportTest {
    @Override
    protected ChannelImpl createChannel() {
      if (!useOkHttp) {
        InetAddress address;
        try {
          address = InetAddress.getByName(serverHost);
          if (serverHostOverride != null) {
            // Force the hostname to match the cert the server uses.
            address = InetAddress.getByAddress(serverHostOverride, address.getAddress());
          }
        } catch (UnknownHostException ex) {
          throw new RuntimeException(ex);
        }
        SslContext sslContext = null;
        if (useTestCa) {
          try {
            sslContext = SslContext.newClientContext(Util.loadCert("ca.pem"));
          } catch (Exception ex) {
            throw new RuntimeException(ex);
          }
        }
        return NettyChannelBuilder.forAddress(new InetSocketAddress(address, serverPort))
            .negotiationType(useTls ? NegotiationType.TLS : NegotiationType.PLAINTEXT)
            .sslContext(sslContext)
            .build();
      } else {
        if (serverHostOverride != null) {
          throw new IllegalStateException("Server host override unsupported with okhttp");
        }
        if (useTls) {
          throw new IllegalStateException("TLS unsupported with okhttp");
        }
        return OkHttpChannelBuilder.forAddress(serverHost, serverPort).build();
      }
    }
  }
}
