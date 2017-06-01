/*
 * Copyright 2014, gRPC Authors All rights reserved.
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

import io.grpc.ManagedChannel;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.testing.TestUtils;
import io.netty.handler.ssl.ClientAuth;
import io.netty.handler.ssl.SslProvider;
import io.netty.handler.ssl.SupportedCipherSuiteFilter;
import java.io.IOException;
import org.junit.AfterClass;
import org.junit.BeforeClass;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Integration tests for GRPC over HTTP2 using the Netty framework.
 */
@RunWith(JUnit4.class)
public class Http2NettyTest extends AbstractInteropTest {

  /** Starts the server with HTTPS. */
  @BeforeClass
  public static void startServer() {
    try {
      startStaticServer(NettyServerBuilder.forPort(0)
          .flowControlWindow(65 * 1024)
          .maxMessageSize(AbstractInteropTest.MAX_MESSAGE_SIZE)
          .sslContext(GrpcSslContexts
              .forServer(TestUtils.loadCert("server1.pem"), TestUtils.loadCert("server1.key"))
              .clientAuth(ClientAuth.REQUIRE)
              .trustManager(TestUtils.loadCert("ca.pem"))
              .ciphers(TestUtils.preferredTestCiphers(), SupportedCipherSuiteFilter.INSTANCE)
              .sslProvider(SslProvider.OPENSSL)
              .build()));
    } catch (IOException ex) {
      throw new RuntimeException(ex);
    }
  }

  @AfterClass
  public static void stopServer() {
    stopStaticServer();
  }

  @Override
  protected ManagedChannel createChannel() {
    try {
      NettyChannelBuilder builder = NettyChannelBuilder
          .forAddress(TestUtils.testServerAddress(getPort()))
          .flowControlWindow(65 * 1024)
          .maxInboundMessageSize(AbstractInteropTest.MAX_MESSAGE_SIZE)
          .sslContext(GrpcSslContexts
              .forClient()
              .keyManager(TestUtils.loadCert("client.pem"), TestUtils.loadCert("client.key"))
              .trustManager(TestUtils.loadX509Cert("ca.pem"))
              .ciphers(TestUtils.preferredTestCiphers(), SupportedCipherSuiteFilter.INSTANCE)
              .sslProvider(SslProvider.OPENSSL)
              .build());
      io.grpc.internal.TestingAccessor.setStatsContextFactory(builder, getClientStatsFactory());
      return builder.build();
    } catch (Exception ex) {
      throw new RuntimeException(ex);
    }
  }

  @Test(timeout = 10000)
  public void remoteAddr() {
    assertRemoteAddr("/0:0:0:0:0:0:0:1");
  }

  @Test(timeout = 10000)
  public void tlsInfo() {
    assertX500SubjectDn("CN=testclient, O=Internet Widgits Pty Ltd, ST=Some-State, C=AU");
  }
}
