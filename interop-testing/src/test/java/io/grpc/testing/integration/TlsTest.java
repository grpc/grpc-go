/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;

import com.google.common.base.Throwables;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.EmptyProtos.Empty;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.testing.TestUtils;
import io.netty.handler.ssl.ClientAuth;
import io.netty.handler.ssl.OpenSsl;
import io.netty.handler.ssl.SslContext;
import io.netty.handler.ssl.SslContextBuilder;
import io.netty.handler.ssl.SslProvider;
import java.io.File;
import java.io.IOException;
import java.security.NoSuchAlgorithmException;
import java.security.cert.X509Certificate;
import java.util.Arrays;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import javax.net.ssl.SSLContext;
import org.junit.After;
import org.junit.Assume;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.Parameterized;
import org.junit.runners.Parameterized.Parameter;
import org.junit.runners.Parameterized.Parameters;


/**
 * Integration tests for GRPC's TLS support.
 */
@RunWith(Parameterized.class)
public class TlsTest {

  /**
   * Iterable of various configurations to use for tests.
   */
  @Parameters(name = "{0}")
  public static Iterable<Object[]> data() {
    return Arrays.asList(new Object[][] {
      {SslProvider.JDK}, {SslProvider.OPENSSL},
    });
  }

  @Parameter(value = 0)
  public SslProvider sslProvider;

  private ScheduledExecutorService executor;
  private Server server;
  private ManagedChannel channel;
  private SslContextBuilder clientContextBuilder;

  @Before
  public void setUp() throws NoSuchAlgorithmException {
    executor = Executors.newSingleThreadScheduledExecutor();
    if (sslProvider == SslProvider.OPENSSL) {
      Assume.assumeTrue(OpenSsl.isAvailable());
    }
    if (sslProvider == SslProvider.JDK) {
      Assume.assumeTrue(Arrays.asList(
          SSLContext.getDefault().getSupportedSSLParameters().getCipherSuites())
          .contains("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"));

    }
    clientContextBuilder = GrpcSslContexts.configure(SslContextBuilder.forClient(), sslProvider);
  }

  @After
  public void tearDown() {
    if (server != null) {
      server.shutdown();
    }
    if (channel != null) {
      channel.shutdown();
    }
    MoreExecutors.shutdownAndAwaitTermination(executor, 5, TimeUnit.SECONDS);
  }


  /**
   * Tests that a client and a server configured using GrpcSslContexts can successfully
   * communicate with each other.
   */
  @Test
  public void basicClientServerIntegrationTest() throws Exception {
    // Create & start a server.
    File serverCertFile = TestUtils.loadCert("server1.pem");
    File serverPrivateKeyFile = TestUtils.loadCert("server1.key");
    X509Certificate[] serverTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };
    server = serverBuilder(0, serverCertFile, serverPrivateKeyFile, serverTrustedCaCerts)
        .addService(new TestServiceImpl(executor))
        .build()
        .start();

    // Create a client.
    File clientCertChainFile = TestUtils.loadCert("client.pem");
    File clientPrivateKeyFile = TestUtils.loadCert("client.key");
    X509Certificate[] clientTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };
    channel = clientChannel(server.getPort(), clientContextBuilder
        .keyManager(clientCertChainFile, clientPrivateKeyFile)
        .trustManager(clientTrustedCaCerts)
        .build());
    TestServiceGrpc.TestServiceBlockingStub client = TestServiceGrpc.newBlockingStub(channel);

    // Send an actual request, via the full GRPC & network stack, and check that a proper
    // response comes back.
    Empty request = Empty.getDefaultInstance();
    client.emptyCall(request);
  }


  /**
   * Tests that a server configured to require client authentication refuses to accept connections
   * from a client that has an untrusted certificate.
   */
  @Test
  public void serverRejectsUntrustedClientCert() throws Exception {
    // Create & start a server. It requires client authentication and trusts only the test CA.
    File serverCertFile = TestUtils.loadCert("server1.pem");
    File serverPrivateKeyFile = TestUtils.loadCert("server1.key");
    X509Certificate[] serverTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };
    server = serverBuilder(0, serverCertFile, serverPrivateKeyFile, serverTrustedCaCerts)
        .addService(new TestServiceImpl(executor))
        .build()
        .start();

    // Create a client. Its credentials come from a CA that the server does not trust. The client
    // trusts both test CAs, so we can be sure that the handshake failure is due to the server
    // rejecting the client's cert, not the client rejecting the server's cert.
    File clientCertChainFile = TestUtils.loadCert("badclient.pem");
    File clientPrivateKeyFile = TestUtils.loadCert("badclient.key");
    X509Certificate[] clientTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };
    channel = clientChannel(server.getPort(), clientContextBuilder
        .keyManager(clientCertChainFile, clientPrivateKeyFile)
        .trustManager(clientTrustedCaCerts)
        .build());
    TestServiceGrpc.TestServiceBlockingStub client = TestServiceGrpc.newBlockingStub(channel);

    // Check that the TLS handshake fails.
    Empty request = Empty.getDefaultInstance();
    try {
      client.emptyCall(request);
      fail("TLS handshake should have failed, but didn't; received RPC response");
    } catch (StatusRuntimeException e) {
      // GRPC reports this situation by throwing a StatusRuntimeException that wraps either a
      // javax.net.ssl.SSLHandshakeException or a java.nio.channels.ClosedChannelException.
      // Thus, reliably detecting the underlying cause is not feasible.
      assertEquals(
          Throwables.getStackTraceAsString(e),
          Status.Code.UNAVAILABLE, e.getStatus().getCode());
    }
  }


  /**
   * Tests that a server configured to require client authentication actually does require client
   * authentication.
   */
  @Test
  public void noClientAuthFailure() throws Exception {
    // Create & start a server.
    File serverCertFile = TestUtils.loadCert("server1.pem");
    File serverPrivateKeyFile = TestUtils.loadCert("server1.key");
    X509Certificate[] serverTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };
    server = serverBuilder(0, serverCertFile, serverPrivateKeyFile, serverTrustedCaCerts)
        .addService(new TestServiceImpl(executor))
        .build()
        .start();

    // Create a client. It has no credentials.
    X509Certificate[] clientTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };
    channel = clientChannel(server.getPort(), clientContextBuilder
        .trustManager(clientTrustedCaCerts)
        .build());
    TestServiceGrpc.TestServiceBlockingStub client = TestServiceGrpc.newBlockingStub(channel);

    // Check that the TLS handshake fails.
    Empty request = Empty.getDefaultInstance();
    try {
      client.emptyCall(request);
      fail("TLS handshake should have failed, but didn't; received RPC response");
    } catch (StatusRuntimeException e) {
      // GRPC reports this situation by throwing a StatusRuntimeException that wraps either a
      // javax.net.ssl.SSLHandshakeException or a java.nio.channels.ClosedChannelException.
      // Thus, reliably detecting the underlying cause is not feasible.
      assertEquals(
          Throwables.getStackTraceAsString(e),
          Status.Code.UNAVAILABLE, e.getStatus().getCode());
    }
  }


  /**
   * Tests that a client configured using GrpcSslContexts refuses to talk to a server that has an
   * an untrusted certificate.
   */
  @Test
  public void clientRejectsUntrustedServerCert() throws Exception {
    // Create & start a server.
    File serverCertFile = TestUtils.loadCert("badserver.pem");
    File serverPrivateKeyFile = TestUtils.loadCert("badserver.key");
    X509Certificate[] serverTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };
    server = serverBuilder(0, serverCertFile, serverPrivateKeyFile, serverTrustedCaCerts)
        .addService(new TestServiceImpl(executor))
        .build()
        .start();

    // Create a client.
    File clientCertChainFile = TestUtils.loadCert("client.pem");
    File clientPrivateKeyFile = TestUtils.loadCert("client.key");
    X509Certificate[] clientTrustedCaCerts = {
      TestUtils.loadX509Cert("ca.pem")
    };
    channel = clientChannel(server.getPort(), clientContextBuilder
        .keyManager(clientCertChainFile, clientPrivateKeyFile)
        .trustManager(clientTrustedCaCerts)
        .build());
    TestServiceGrpc.TestServiceBlockingStub client = TestServiceGrpc.newBlockingStub(channel);

    // Check that the TLS handshake fails.
    Empty request = Empty.getDefaultInstance();
    try {
      client.emptyCall(request);
      fail("TLS handshake should have failed, but didn't; received RPC response");
    } catch (StatusRuntimeException e) {
      // GRPC reports this situation by throwing a StatusRuntimeException that wraps either a
      // javax.net.ssl.SSLHandshakeException or a java.nio.channels.ClosedChannelException.
      // Thus, reliably detecting the underlying cause is not feasible.
      // TODO(carl-mastrangelo): eventually replace this with a hamcrest matcher.
      assertEquals(
          Throwables.getStackTraceAsString(e),
          Status.Code.UNAVAILABLE, e.getStatus().getCode());
    }
  }


  private ServerBuilder<?> serverBuilder(int port, File serverCertChainFile,
      File serverPrivateKeyFile, X509Certificate[] serverTrustedCaCerts) throws IOException {
    SslContextBuilder sslContextBuilder
        = SslContextBuilder.forServer(serverCertChainFile, serverPrivateKeyFile);
    GrpcSslContexts.configure(sslContextBuilder, sslProvider);
    sslContextBuilder.trustManager(serverTrustedCaCerts)
        .clientAuth(ClientAuth.REQUIRE);

    return NettyServerBuilder.forPort(port)
        .sslContext(sslContextBuilder.build());
  }


  private static ManagedChannel clientChannel(int port, SslContext sslContext) throws IOException {
    return NettyChannelBuilder.forAddress("localhost", port)
        .overrideAuthority(TestUtils.TEST_SERVER_HOST)
        .negotiationType(NegotiationType.TLS)
        .sslContext(sslContext)
        .build();
  }
}
