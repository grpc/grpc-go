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

import com.squareup.okhttp.ConnectionSpec;
import com.squareup.okhttp.TlsVersion;

import io.grpc.ManagedChannel;
import io.grpc.internal.GrpcUtil;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.okhttp.OkHttpChannelBuilder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.StreamRecorder;
import io.grpc.testing.TestUtils;
import io.netty.handler.ssl.SupportedCipherSuiteFilter;

import org.junit.AfterClass;
import org.junit.BeforeClass;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.IOException;

/**
 * Integration tests for GRPC over Http2 using the OkHttp framework.
 */
@RunWith(JUnit4.class)
public class Http2OkHttpTest extends AbstractTransportTest {
  private static int serverPort = TestUtils.pickUnusedPort();

  /** Starts the server with HTTPS. */
  @BeforeClass
  public static void startServer() throws Exception {
    try {
      startStaticServer(NettyServerBuilder.forPort(serverPort)
          .sslContext(GrpcSslContexts
              .forServer(TestUtils.loadCert("server1.pem"), TestUtils.loadCert("server1.key"))
              .ciphers(TestUtils.preferredTestCiphers(), SupportedCipherSuiteFilter.INSTANCE)
              .build()));
    } catch (IOException ex) {
      throw new RuntimeException(ex);
    }
  }

  @AfterClass
  public static void stopServer() throws Exception {
    stopStaticServer();
  }

  @Override
  protected ManagedChannel createChannel() {
    OkHttpChannelBuilder builder = OkHttpChannelBuilder.forAddress("127.0.0.1", serverPort)
        .connectionSpec(new ConnectionSpec.Builder(OkHttpChannelBuilder.DEFAULT_CONNECTION_SPEC)
            .cipherSuites(TestUtils.preferredTestCiphers().toArray(new String[0]))
            .tlsVersions(ConnectionSpec.MODERN_TLS.tlsVersions().toArray(new TlsVersion[0]))
            .build())
        .overrideAuthority(GrpcUtil.authorityFromHostAndPort(
            TestUtils.TEST_SERVER_HOST, serverPort));
    try {
      builder.sslSocketFactory(TestUtils.newSslSocketFactoryForCa(
          TestUtils.loadCert("ca.pem")));
    } catch (Exception e) {
      throw new RuntimeException(e);
    }
    return builder.build();
  }

  @Test(timeout = 10000)
  public void receivedDataForFinishedStream() throws Exception {
    Messages.ResponseParameters.Builder responseParameters =
        Messages.ResponseParameters.newBuilder()
        .setSize(1);
    Messages.StreamingOutputCallRequest.Builder requestBuilder =
        Messages.StreamingOutputCallRequest.newBuilder()
            .setResponseType(Messages.PayloadType.COMPRESSABLE);
    for (int i = 0; i < 10000; i++) {
      requestBuilder.addResponseParameters(responseParameters);
    }

    StreamRecorder<Messages.StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<Messages.StreamingOutputCallRequest> requestStream =
        asyncStub.fullDuplexCall(recorder);
    requestStream.onNext(requestBuilder.build());
    recorder.firstValue().get();
    requestStream.onError(new Exception("failed"));

    recorder.awaitCompletion();
    emptyUnary();
  }
}
