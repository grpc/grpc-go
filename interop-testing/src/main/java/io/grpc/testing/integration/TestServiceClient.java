/*
 * Copyright 2014 The gRPC Authors
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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.io.Files;
import io.grpc.ManagedChannel;
import io.grpc.alts.AltsChannelBuilder;
import io.grpc.alts.GoogleDefaultChannelBuilder;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.testing.TestUtils;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.okhttp.OkHttpChannelBuilder;
import io.grpc.okhttp.internal.Platform;
import io.netty.handler.ssl.SslContext;
import java.io.File;
import java.io.FileInputStream;
import java.nio.charset.Charset;
import javax.net.ssl.SSLSocketFactory;

/**
 * Application that starts a client for the {@link TestServiceGrpc.TestServiceImplBase} and runs
 * through a series of tests.
 */
public class TestServiceClient {

  private static final Charset UTF_8 = Charset.forName("UTF-8");

  /**
   * The main application allowing this client to be launched from the command line.
   */
  public static void main(String[] args) throws Exception {
    // Let Netty or OkHttp use Conscrypt if it is available.
    TestUtils.installConscryptIfAvailable();
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
  private boolean useAlts = false;
  private String customCredentialsType;
  private boolean useTestCa;
  private boolean useOkHttp;
  private String defaultServiceAccount;
  private String serviceAccountKeyFile;
  private String oauthScope;
  private boolean fullStreamDecompression;

  private Tester tester = new Tester();

  @VisibleForTesting
  void parseArgs(String[] args) {
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
      } else if ("use_alts".equals(key)) {
        useAlts = Boolean.parseBoolean(value);
      } else if ("custom_credentials_type".equals(key)) {
        customCredentialsType = value;
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
      } else if ("full_stream_decompression".equals(key)) {
        fullStreamDecompression = Boolean.parseBoolean(value);
      } else {
        System.err.println("Unknown argument: " + key);
        usage = true;
        break;
      }
    }
    if (useAlts) {
      useTls = false;
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
          + validTestCasesHelpText()
          + "\n  --use_tls=true|false        Whether to use TLS. Default " + c.useTls
          + "\n  --use_alts=true|false       Whether to use ALTS. Enable ALTS will disable TLS."
          + "\n                              Default " + c.useAlts
          + "\n  --custom_credentials_type   Custom credentials type to use. Default "
            + c.customCredentialsType
          + "\n  --use_test_ca=true|false    Whether to trust our fake CA. Requires --use_tls=true "
          + "\n                              to have effect. Default " + c.useTestCa
          + "\n  --use_okhttp=true|false     Whether to use OkHttp instead of Netty. Default "
            + c.useOkHttp
          + "\n  --default_service_account   Email of GCE default service account. Default "
            + c.defaultServiceAccount
          + "\n  --service_account_key_file  Path to service account json key file."
            + c.serviceAccountKeyFile
          + "\n  --oauth_scope               Scope for OAuth tokens. Default " + c.oauthScope
          + "\n  --full_stream_decompression Enable full-stream decompression. Default "
            + c.fullStreamDecompression
      );
      System.exit(1);
    }
  }

  @VisibleForTesting
  void setUp() {
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
      runTest(TestCases.fromString(testCase));
    } catch (RuntimeException ex) {
      throw ex;
    } catch (Exception ex) {
      throw new RuntimeException(ex);
    }
    System.out.println("Test completed.");
  }

  private void runTest(TestCases testCase) throws Exception {
    switch (testCase) {
      case EMPTY_UNARY:
        tester.emptyUnary();
        break;

      case CACHEABLE_UNARY: {
        tester.cacheableUnary();
        break;
      }

      case LARGE_UNARY:
        tester.largeUnary();
        break;

      case CLIENT_COMPRESSED_UNARY:
        tester.clientCompressedUnary(true);
        break;

      case CLIENT_COMPRESSED_UNARY_NOPROBE:
        tester.clientCompressedUnary(false);
        break;

      case SERVER_COMPRESSED_UNARY:
        tester.serverCompressedUnary();
        break;

      case CLIENT_STREAMING:
        tester.clientStreaming();
        break;

      case CLIENT_COMPRESSED_STREAMING:
        tester.clientCompressedStreaming(true);
        break;

      case CLIENT_COMPRESSED_STREAMING_NOPROBE:
        tester.clientCompressedStreaming(false);
        break;

      case SERVER_STREAMING:
        tester.serverStreaming();
        break;

      case SERVER_COMPRESSED_STREAMING:
        tester.serverCompressedStreaming();
        break;

      case PING_PONG:
        tester.pingPong();
        break;

      case EMPTY_STREAM:
        tester.emptyStream();
        break;

      case COMPUTE_ENGINE_CREDS:
        tester.computeEngineCreds(defaultServiceAccount, oauthScope);
        break;

      case SERVICE_ACCOUNT_CREDS: {
        String jsonKey = Files.asCharSource(new File(serviceAccountKeyFile), UTF_8).read();
        FileInputStream credentialsStream = new FileInputStream(new File(serviceAccountKeyFile));
        tester.serviceAccountCreds(jsonKey, credentialsStream, oauthScope);
        break;
      }

      case JWT_TOKEN_CREDS: {
        FileInputStream credentialsStream = new FileInputStream(new File(serviceAccountKeyFile));
        tester.jwtTokenCreds(credentialsStream);
        break;
      }

      case OAUTH2_AUTH_TOKEN: {
        String jsonKey = Files.asCharSource(new File(serviceAccountKeyFile), UTF_8).read();
        FileInputStream credentialsStream = new FileInputStream(new File(serviceAccountKeyFile));
        tester.oauth2AuthToken(jsonKey, credentialsStream, oauthScope);
        break;
      }

      case PER_RPC_CREDS: {
        String jsonKey = Files.asCharSource(new File(serviceAccountKeyFile), UTF_8).read();
        FileInputStream credentialsStream = new FileInputStream(new File(serviceAccountKeyFile));
        tester.perRpcCreds(jsonKey, credentialsStream, oauthScope);
        break;
      }

      case CUSTOM_METADATA: {
        tester.customMetadata();
        break;
      }

      case STATUS_CODE_AND_MESSAGE: {
        tester.statusCodeAndMessage();
        break;
      }

      case SPECIAL_STATUS_MESSAGE:
        tester.specialStatusMessage();
        break;

      case UNIMPLEMENTED_METHOD: {
        tester.unimplementedMethod();
        break;
      }

      case UNIMPLEMENTED_SERVICE: {
        tester.unimplementedService();
        break;
      }

      case CANCEL_AFTER_BEGIN: {
        tester.cancelAfterBegin();
        break;
      }

      case CANCEL_AFTER_FIRST_RESPONSE: {
        tester.cancelAfterFirstResponse();
        break;
      }

      case TIMEOUT_ON_SLEEPING_SERVER: {
        tester.timeoutOnSleepingServer();
        break;
      }

      case VERY_LARGE_REQUEST: {
        tester.veryLargeRequest();
        break;
      }

      default:
        throw new IllegalArgumentException("Unknown test case: " + testCase);
    }
  }

  private class Tester extends AbstractInteropTest {
    @Override
    protected ManagedChannel createChannel() {
      if (customCredentialsType != null
          && customCredentialsType.equals("google_default_credentials")) {
        return GoogleDefaultChannelBuilder.forAddress(serverHost, serverPort).build();
      }
      if (useAlts) {
        return AltsChannelBuilder.forAddress(serverHost, serverPort).build();
      }
      AbstractManagedChannelImplBuilder<?> builder;
      if (!useOkHttp) {
        SslContext sslContext = null;
        if (useTestCa) {
          try {
            sslContext = GrpcSslContexts.forClient().trustManager(
                    TestUtils.loadCert("ca.pem")).build();
          } catch (Exception ex) {
            throw new RuntimeException(ex);
          }
        }
        NettyChannelBuilder nettyBuilder =
            NettyChannelBuilder.forAddress(serverHost, serverPort)
                .flowControlWindow(65 * 1024)
                .negotiationType(useTls ? NegotiationType.TLS : NegotiationType.PLAINTEXT)
                .sslContext(sslContext);
        if (serverHostOverride != null) {
          nettyBuilder.overrideAuthority(serverHostOverride);
        }
        if (fullStreamDecompression) {
          nettyBuilder.enableFullStreamDecompression();
        }
        builder = nettyBuilder;
      } else {
        OkHttpChannelBuilder okBuilder = OkHttpChannelBuilder.forAddress(serverHost, serverPort);
        if (serverHostOverride != null) {
          // Force the hostname to match the cert the server uses.
          okBuilder.overrideAuthority(
              GrpcUtil.authorityFromHostAndPort(serverHostOverride, serverPort));
        }
        if (useTls) {
          try {
            SSLSocketFactory factory = useTestCa
                ? TestUtils.newSslSocketFactoryForCa(Platform.get().getProvider(),
                    TestUtils.loadCert("ca.pem"))
                : (SSLSocketFactory) SSLSocketFactory.getDefault();
            okBuilder.sslSocketFactory(factory);
          } catch (Exception e) {
            throw new RuntimeException(e);
          }
        } else {
          okBuilder.usePlaintext();
        }
        if (fullStreamDecompression) {
          okBuilder.enableFullStreamDecompression();
        }
        builder = okBuilder;
      }
      io.grpc.internal.TestingAccessor.setStatsImplementation(
          builder, createClientCensusStatsModule());
      return builder.build();
    }

    @Override
    protected boolean metricsExpected() {
      // Exact message size doesn't match when testing with Go servers:
      // https://github.com/grpc/grpc-go/issues/1572
      // TODO(zhangkun83): remove this override once the said issue is fixed.
      return false;
    }
  }

  private static String validTestCasesHelpText() {
    StringBuilder builder = new StringBuilder();
    for (TestCases testCase : TestCases.values()) {
      String strTestcase = testCase.name().toLowerCase();
      builder.append("\n      ")
          .append(strTestcase)
          .append(": ")
          .append(testCase.description());
    }
    return builder.toString();
  }
}
