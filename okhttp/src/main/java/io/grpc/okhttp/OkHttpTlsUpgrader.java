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
