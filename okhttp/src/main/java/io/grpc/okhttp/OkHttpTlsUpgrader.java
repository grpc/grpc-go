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

package io.grpc.okhttp;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.okhttp.internal.ConnectionSpec;
import io.grpc.okhttp.internal.OkHostnameVerifier;
import io.grpc.okhttp.internal.Protocol;
import java.io.IOException;
import java.net.Socket;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import javax.net.ssl.SSLPeerUnverifiedException;
import javax.net.ssl.SSLSocket;
import javax.net.ssl.SSLSocketFactory;

/**
 * A helper class that located in package com.squareup.okhttp, so that we can use OkHttp internals
 * to do TLS upgrading.
 */
final class OkHttpTlsUpgrader {

  /*
   * List of ALPN/NPN protocols in order of preference. GRPC_EXP requires that
   * HTTP_2 be present and that GRPC_EXP should be preferenced over HTTP_2.
   */
  @VisibleForTesting
  static final List<Protocol> TLS_PROTOCOLS =
      Collections.unmodifiableList(Arrays.<Protocol>asList(Protocol.GRPC_EXP, Protocol.HTTP_2));

  /**
   * Upgrades given Socket to be a SSLSocket.
   *
   * @throws IOException if an IO error was encountered during the upgrade handshake.
   * @throws RuntimeException if the upgrade negotiation failed.
   */
  public static SSLSocket upgrade(SSLSocketFactory sslSocketFactory,
      Socket socket, String host, int port, ConnectionSpec spec) throws IOException {
    Preconditions.checkNotNull(sslSocketFactory, "sslSocketFactory");
    Preconditions.checkNotNull(socket, "socket");
    Preconditions.checkNotNull(spec, "spec");
    SSLSocket sslSocket = (SSLSocket) sslSocketFactory.createSocket(
        socket, host, port, true /* auto close */);
    spec.apply(sslSocket, false);
    String negotiatedProtocol = OkHttpProtocolNegotiator.get().negotiate(
        sslSocket, host, spec.supportsTlsExtensions() ? TLS_PROTOCOLS : null);
    Preconditions.checkState(
        TLS_PROTOCOLS.contains(Protocol.get(negotiatedProtocol)),
        "Only " + TLS_PROTOCOLS + " are supported, but negotiated protocol is %s",
        negotiatedProtocol);

    if (!OkHostnameVerifier.INSTANCE.verify(host, sslSocket.getSession())) {
      throw new SSLPeerUnverifiedException("Cannot verify hostname: " + host);
    }
    return sslSocket;
  }
}
