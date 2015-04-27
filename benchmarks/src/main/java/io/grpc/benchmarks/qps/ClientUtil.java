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

package io.grpc.benchmarks.qps;

import static io.grpc.testing.integration.Util.loadCert;

import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.ByteString;

import grpc.testing.Qpstest;
import grpc.testing.Qpstest.SimpleRequest;
import io.grpc.Channel;
import io.grpc.transport.netty.NegotiationType;
import io.grpc.transport.netty.NettyChannelBuilder;
import io.grpc.transport.okhttp.OkHttpChannelBuilder;
import io.netty.handler.ssl.SslContext;
import org.HdrHistogram.Histogram;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;

/**
 * Utility methods for client implementations.
 */
final class ClientUtil {

  private ClientUtil() {
  }

  static SimpleRequest newRequest(ClientConfiguration config) {
    ByteString body = ByteString.copyFrom(new byte[config.clientPayload]);
    Qpstest.Payload payload = Qpstest.Payload.newBuilder()
                                     .setType(config.payloadType)
                                     .setBody(body)
                                     .build();

    return SimpleRequest.newBuilder()
        .setResponseType(config.payloadType)
        .setResponseSize(config.serverPayload)
        .setPayload(payload)
        .build();
  }

  static Channel newChannel(ClientConfiguration config) throws IOException {
    if (config.okhttp) {
      if (config.tls) {
        throw new IllegalStateException("TLS unsupported with okhttp");
      }
      return OkHttpChannelBuilder
          .forAddress(config.host, config.port)
          .executor(config.directExecutor ? MoreExecutors.newDirectExecutorService() : null)
          .build();
    }
    SslContext context = null;
    InetAddress address = InetAddress.getByName(config.host);
    NegotiationType negotiationType = config.tls ? NegotiationType.TLS : NegotiationType.PLAINTEXT;
    if (config.tls && config.testca) {
      // Force the hostname to match the cert the server uses.
      address = InetAddress.getByAddress("foo.test.google.fr", address.getAddress());
      File cert = loadCert("ca.pem");
      context = SslContext.newClientContext(cert);
    }
    return NettyChannelBuilder
        .forAddress(new InetSocketAddress(address, config.port))
        .negotiationType(negotiationType)
        .executor(config.directExecutor ? MoreExecutors.newDirectExecutorService() : null)
        .sslContext(context)
        .connectionWindowSize(config.connectionWindow)
        .streamWindowSize(config.streamWindow)
        .build();
  }

  static void saveHistogram(Histogram histogram, String filename) throws IOException {
    File file;
    PrintStream log = null;
    try {
      file = new File(filename);
      if (file.exists()) {
        file.delete();
      }
      log = new PrintStream(new FileOutputStream(file), false);
      histogram.outputPercentileDistribution(log, 1.0);
    } finally {
      if (log != null) {
        log.close();
      }
    }
  }
}
