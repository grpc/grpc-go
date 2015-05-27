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

package com.squareup.okhttp;

import com.google.common.base.Preconditions;

import com.squareup.okhttp.internal.Platform;
import com.squareup.okhttp.internal.SelectedProtocolQuerier;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Proxy;
import java.net.ProxySelector;
import java.net.Socket;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

import javax.net.ssl.SSLSocket;
import javax.net.ssl.SSLSocketFactory;

/**
 * A helper class that located in package com.squareup.okhttp, so that we can use OkHttp internals
 * to do TLS upgrading.
 */
public final class OkHttpTlsUpgrader {

  // TODO(madongfly): We should only support "h2" at a right time.
  private static final List<String> SUPPORTED_HTTP2_PROTOCOLS = Collections.unmodifiableList(
      Arrays.asList("h2", "h2-14", "h2-15", "h2-16"));

  // A dummy address used to bypass null check.
  private static final InetSocketAddress DUMMY_INET_SOCKET_ADDRESS =
      InetSocketAddress.createUnresolved("fake", 73);

  /**
   * Upgrades given Socket to be a SSLSocket.
   */
  public static SSLSocket upgrade(SSLSocketFactory sslSocketFactory,
      Socket socket, String host, int port, ConnectionSpec spec) throws IOException {
    Preconditions.checkNotNull(sslSocketFactory);
    Preconditions.checkNotNull(socket);
    Preconditions.checkNotNull(spec);
    SSLSocket sslSocket = (SSLSocket) sslSocketFactory.createSocket(
        socket, host, port, true /* auto close */);
    spec.apply(sslSocket, getOkHttpRoute(host, port, spec));

    Platform platform = Platform.get();

    // It's possible that the user provided SSLSocketFactory has already done the handshake
    // when creates the SSLSocket.
    String negotiatedProtocol = SelectedProtocolQuerier.getSelectedProtocol(sslSocket);
    if (negotiatedProtocol == null) {
      try {
        // Force handshake.
        sslSocket.startHandshake();

        negotiatedProtocol = SelectedProtocolQuerier.getSelectedProtocol(sslSocket);
        if (negotiatedProtocol == null) {
          throw new RuntimeException("protocol negotiation failed");
        }
      } finally {
        platform.afterHandshake(sslSocket);
      }
    }

    Preconditions.checkState(SUPPORTED_HTTP2_PROTOCOLS.contains(negotiatedProtocol),
        "negotiated protocol %s is unsupported", negotiatedProtocol);

    return sslSocket;
  }

  private static Route getOkHttpRoute(String host, int port, ConnectionSpec spec) {
    return new Route(getOkHttpAddress(host, port), Proxy.NO_PROXY, DUMMY_INET_SOCKET_ADDRESS, spec);
  }

  private static Address getOkHttpAddress(String host, int port) {
    return new Address(host, port, null, null, null, null,
        DummyAuthenticator.INSTANCE, Proxy.NO_PROXY, Arrays.<Protocol>asList(Protocol.HTTP_2),
        Collections.<ConnectionSpec>emptyList(), ProxySelector.getDefault());
  }

  /**
   * A dummy implementation does nothing.
   */
  private static class DummyAuthenticator implements Authenticator {
    static final DummyAuthenticator INSTANCE = new DummyAuthenticator();

    @Override public Request authenticate(Proxy proxy, Response response) throws IOException {
      return null;
    }

    @Override public Request authenticateProxy(Proxy proxy, Response response) throws IOException {
      return null;
    }
  }
}
