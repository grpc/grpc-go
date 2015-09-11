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

import com.google.common.io.Files;

import io.grpc.ManagedChannel;
import io.grpc.internal.GrpcUtil;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.okhttp.OkHttpChannelBuilder;
import io.grpc.testing.TestUtils;
import io.netty.handler.ssl.SslContext;

import java.io.File;
import java.io.FileInputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.UnknownHostException;
import java.nio.charset.Charset;

import javax.net.ssl.SSLSocketFactory;

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
    client.setUp();

    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        System.out.println("Shutting down");
        try {
          client.tearDown();
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });

    try {
      client.run();
    } finally {
      client.tearDown();
    }
    System.exit(0);
  }

  private String serverHost = "localhost";
  private String serverHostOverride;
  private int serverPort = 8080;
  private String testCase = "empty_unary";
  private boolean useTls = true;
  private boolean useTestCa;
  private boolean useOkHttp;
  private String defaultServiceAccount;
  private String serviceAccountKeyFile;
  private String oauthScope;

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
      } else if ("default_service_account".equals(key)) {
        defaultServiceAccount = value;
      } else if ("service_account_key_file".equals(key)) {
        serviceAccountKeyFile = value;
      } else if ("oauth_scope".equals(key)) {
        oauthScope = value;
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
          + "\n    Valid options:"
          + "\n      empty_unary: empty (zero bytes) request and response"
          + "\n      large_unary: single request and (large) response"
          + "\n      client_streaming: request streaming with single response"
          + "\n      server_streaming: single request with response streaming"
          + "\n      ping_pong: full-duplex ping-pong streaming"
          + "\n      empty_stream: A stream that has zero-messages in both directions"
          + "\n      service_account_creds: large_unary with service_account auth"
          + "\n      compute_engine_creds: large_unary with compute engine auth"
          + "\n      jwt_token_creds: JWT-based auth"
          + "\n      oauth2_auth_token: raw oauth2 access token auth"
          + "\n      per_rpc_creds: per rpc raw oauth2 access token auth"
          + "\n      cancel_after_begin: cancel stream after starting it"
          + "\n      cancel_after_first_response: cancel on first response"
          + "\n  --use_tls=true|false        Whether to use TLS. Default " + c.useTls
          + "\n  --use_test_ca=true|false    Whether to trust our fake CA. Default " + c.useTestCa
          + "\n  --use_okhttp=true|false     Whether to use OkHttp instead of Netty. Default "
            + c.useOkHttp
          + "\n  --default_service_account   Email of GCE default service account. Default "
            + c.defaultServiceAccount
          + "\n  --service_account_key_file  Path to service account json key file."
            + c.serviceAccountKeyFile
          + "\n  --oauth_scope               Scope for OAuth tokens. Default " + c.oauthScope
      );
      System.exit(1);
    }
  }

  private void setUp() {
    tester.setUp();
  }

  private synchronized void tearDown() {
    try {
      tester.tearDown();
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
    } else if ("compute_engine_creds".equals(testCase)) {
      tester.computeEngineCreds(defaultServiceAccount, oauthScope);
    } else if ("service_account_creds".equals(testCase)) {
      String jsonKey = Files.toString(new File(serviceAccountKeyFile), Charset.forName("UTF-8"));
      FileInputStream credentialsStream = new FileInputStream(new File(serviceAccountKeyFile));
      tester.serviceAccountCreds(jsonKey, credentialsStream, oauthScope);
    } else if ("jwt_token_creds".equals(testCase)) {
      FileInputStream credentialsStream = new FileInputStream(new File(serviceAccountKeyFile));
      tester.jwtTokenCreds(credentialsStream);
    } else if ("oauth2_auth_token".equals(testCase)) {
      String jsonKey = Files.toString(new File(serviceAccountKeyFile), Charset.forName("UTF-8"));
      FileInputStream credentialsStream = new FileInputStream(new File(serviceAccountKeyFile));
      tester.oauth2AuthToken(jsonKey, credentialsStream, oauthScope);
    } else if ("per_rpc_creds".equals(testCase)) {
      String jsonKey = Files.toString(new File(serviceAccountKeyFile), Charset.forName("UTF-8"));
      FileInputStream credentialsStream = new FileInputStream(new File(serviceAccountKeyFile));
      tester.perRpcCreds(jsonKey, credentialsStream, oauthScope);
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
    protected ManagedChannel createChannel() {
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
            sslContext = GrpcSslContexts.forClient().trustManager(
                    TestUtils.loadCert("ca.pem")).build();
          } catch (Exception ex) {
            throw new RuntimeException(ex);
          }
        }
        return NettyChannelBuilder.forAddress(new InetSocketAddress(address, serverPort))
            .flowControlWindow(65 * 1024)
            .negotiationType(useTls ? NegotiationType.TLS : NegotiationType.PLAINTEXT)
            .sslContext(sslContext)
            .build();
      } else {
        OkHttpChannelBuilder builder = OkHttpChannelBuilder.forAddress(serverHost, serverPort);
        if (serverHostOverride != null) {
          // Force the hostname to match the cert the server uses.
          builder.overrideAuthority(
              GrpcUtil.authorityFromHostAndPort(serverHostOverride, serverPort));
        }
        if (useTls) {
          try {
            SSLSocketFactory factory = useTestCa
                ? TestUtils.newSslSocketFactoryForCa(TestUtils.loadCert("ca.pem"))
                : (SSLSocketFactory) SSLSocketFactory.getDefault();
            builder.sslSocketFactory(factory);
          } catch (Exception e) {
            throw new RuntimeException(e);
          }
        }
        return builder.build();
      }
    }
  }
}
