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

package com.squareup.okhttp.internal;

import com.squareup.okhttp.Protocol;

import java.io.IOException;
import java.net.Socket;
import java.util.List;

import javax.net.ssl.SSLSocket;

/**
 * A helper class located in package com.squareup.okhttp.internal for TLS negotiation.
 */
public class OkHttpProtocolNegotiator {
  private static Platform platform = Platform.get();

  // setUseSessionTickets(boolean)
  private static final OptionalMethod<Socket> SET_USE_SESSION_TICKETS =
      new OptionalMethod<Socket>(null, "setUseSessionTickets", Boolean.TYPE);
  // setHostname(String)
  private static final OptionalMethod<Socket> SET_HOSTNAME =
      new OptionalMethod<Socket>(null, "setHostname", String.class);
  // byte[] getAlpnSelectedProtocol()
  private static final OptionalMethod<Socket> GET_ALPN_SELECTED_PROTOCOL =
      new OptionalMethod<Socket>(byte[].class, "getAlpnSelectedProtocol");
  // setAlpnSelectedProtocol(byte[])
  private static final OptionalMethod<Socket> SET_ALPN_PROTOCOLS =
      new OptionalMethod<Socket>(null, "setAlpnProtocols", byte[].class);
  // byte[] getNpnSelectedProtocol()
  private static final OptionalMethod<Socket> GET_NPN_SELECTED_PROTOCOL =
      new OptionalMethod<Socket>(byte[].class, "getNpnSelectedProtocol");
  private static boolean android;

  /**
   * Start and wait until the negotiation is done, returns the negotiated protocol.
   */
  public static String negotiate(
      SSLSocket sslSocket, String hostname, List<Protocol> protocols) throws IOException {
    configureTlsExtensions(sslSocket, hostname, protocols);

    // It's possible that the user provided SSLSocketFactory has already done the handshake
    // when creates the SSLSocket.
    String negotiatedProtocol = getSelectedProtocol(sslSocket);
    if (negotiatedProtocol == null) {
      try {
        // Force handshake.
        sslSocket.startHandshake();

        negotiatedProtocol = OkHttpProtocolNegotiator.getSelectedProtocol(sslSocket);
        if (negotiatedProtocol == null) {
          throw new RuntimeException("protocol negotiation failed");
        }
      } finally {
        afterHandshake(sslSocket);
      }
    }
    return negotiatedProtocol;
  }

  /**
   * Override {@link Platform}'s configureTlsExtensions for Android older than 5.0, since OkHttp
   * (2.3+) only support such function for Android 5.0+.
   */
  private static void configureTlsExtensions(
      SSLSocket sslSocket, String hostname, List<Protocol> protocols) {
    if (!android) {
      platform.configureTlsExtensions(sslSocket, hostname, protocols);
      return;
    }

    // Enable SNI and session tickets.
    if (hostname != null) {
      SET_USE_SESSION_TICKETS.invokeOptionalWithoutCheckedException(sslSocket, true);
      SET_HOSTNAME.invokeOptionalWithoutCheckedException(sslSocket, hostname);
    }

    // Enable ALPN.
    if (SET_ALPN_PROTOCOLS.isSupported(sslSocket)) {
      Object[] parameters = {platform.concatLengthPrefixed(protocols)};
      SET_ALPN_PROTOCOLS.invokeWithoutCheckedException(sslSocket, parameters);
    }
  }

  private static void afterHandshake(SSLSocket sslSocket) {
    if (!android) {
      platform.afterHandshake(sslSocket);
    }
  }

  /** Returns the negotiated protocol, or null if no protocol was negotiated. */
  public static String getSelectedProtocol(SSLSocket socket) {
    if (!android) {
      return platform.getSelectedProtocol(socket);
    }

    if (GET_ALPN_SELECTED_PROTOCOL.isSupported(socket)) {
      try {
        byte[] alpnResult =
            (byte[]) GET_ALPN_SELECTED_PROTOCOL.invokeWithoutCheckedException(socket);
        if (alpnResult != null) {
          return new String(alpnResult, Util.UTF_8);
        }
      } catch (Exception e) {
        // In some implementations, querying selected protocol before the handshake will fail with
        // exception.
      }
    }

    if (GET_NPN_SELECTED_PROTOCOL.isSupported(socket)) {
      try {
        byte[] npnResult = (byte[]) GET_NPN_SELECTED_PROTOCOL.invokeWithoutCheckedException(socket);
        if (npnResult != null) {
          return new String(npnResult, Util.UTF_8);
        }
      } catch (Exception e) {
        // In some implementations, querying selected protocol before the handshake will fail with
        // exception.
      }
    }
    return null;
  }

  static {
    android = true;
    try {
      // Attempt to find Android 2.3+ APIs.
      Class.forName("com.android.org.conscrypt.OpenSSLSocketImpl");
    } catch (ClassNotFoundException ignored) {
      try {
        // Older platform before being unbundled.
        Class.forName("org.apache.harmony.xnet.provider.jsse.OpenSSLSocketImpl");
      } catch (ClassNotFoundException ignored2) {
        android = false;
      }
    }
  }
}
