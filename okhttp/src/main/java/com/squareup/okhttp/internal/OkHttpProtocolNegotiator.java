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
import java.security.Provider;
import java.security.Security;
import java.util.List;

import javax.net.ssl.SSLSocket;

/**
 * A helper class located in package com.squareup.okhttp.internal for TLS negotiation.
 */
public class OkHttpProtocolNegotiator {
  private static final Platform PLATFORM = Platform.get();
  private static OkHttpProtocolNegotiator NEGOTIATOR = createNegotiator();

  private OkHttpProtocolNegotiator() {}

  public static OkHttpProtocolNegotiator get() {
    return NEGOTIATOR;
  }

  /**
   * Creates corresponding negotiator according to whether on Android.
   */
  private static OkHttpProtocolNegotiator createNegotiator() {
    boolean android = true;
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
    return android ? new AndroidNegotiator() : new OkHttpProtocolNegotiator();
  }

  /**
   * Start and wait until the negotiation is done, returns the negotiated protocol.
   *
   * @throws IOException if an IO error was encountered during the handshake.
   * @throws RuntimeException if the negotiation completed, but no protocol was selected.
   */
  public String negotiate(
      SSLSocket sslSocket, String hostname, List<Protocol> protocols) throws IOException {
    configureTlsExtensions(sslSocket, hostname, protocols);
    try {
      // Force handshake.
      sslSocket.startHandshake();

      String negotiatedProtocol = getSelectedProtocol(sslSocket);
      if (negotiatedProtocol == null) {
        throw new RuntimeException("protocol negotiation failed");
      }
      return negotiatedProtocol;
    } finally {
      PLATFORM.afterHandshake(sslSocket);
    }
  }

  /** Configure TLS extensions. */
  protected void configureTlsExtensions(
      SSLSocket sslSocket, String hostname, List<Protocol> protocols) {
    PLATFORM.configureTlsExtensions(sslSocket, hostname, protocols);
  }

  /** Returns the negotiated protocol, or null if no protocol was negotiated. */
  public String getSelectedProtocol(SSLSocket socket) {
    return PLATFORM.getSelectedProtocol(socket);
  }

  private static class AndroidNegotiator extends OkHttpProtocolNegotiator {
    // setUseSessionTickets(boolean)
    private static final OptionalMethod<Socket> SET_USE_SESSION_TICKETS =
        new OptionalMethod<Socket>(null, "setUseSessionTickets", Boolean.TYPE);
    // setHostname(String)
    private static final OptionalMethod<Socket> SET_HOSTNAME =
        new OptionalMethod<Socket>(null, "setHostname", String.class);
    // byte[] getAlpnSelectedProtocol()
    private static final OptionalMethod<Socket> GET_ALPN_SELECTED_PROTOCOL =
        new OptionalMethod<Socket>(byte[].class, "getAlpnSelectedProtocol");
    // setAlpnProtocol(byte[])
    private static final OptionalMethod<Socket> SET_ALPN_PROTOCOLS =
        new OptionalMethod<Socket>(null, "setAlpnProtocols", byte[].class);
    // byte[] getNpnSelectedProtocol()
    private static final OptionalMethod<Socket> GET_NPN_SELECTED_PROTOCOL =
        new OptionalMethod<Socket>(byte[].class, "getNpnSelectedProtocol");
    // setNpnProtocol(byte[])
    private static final OptionalMethod<Socket> SET_NPN_PROTOCOLS =
        new OptionalMethod<Socket>(null, "setNpnProtocols", byte[].class);

    private enum TlsExtensionType {
      ALPN_AND_NPN,
      NPN,
    }

    private static TlsExtensionType tlsExtensionType;

    static {
      // Decide which TLS Extension (APLN and NPN) we will use, follow the rules:
      // 1. If Google Play Services Security Provider is installed, use both
      // 2. If on Android 5.0 or later, use both, else
      // 3. If on Android 4.1 or later, use NPN, else
      // 4. Fail.
      // TODO(madongfly): Logging.

      // Check if Google Play Services Security Provider is installed.
      Provider provider = Security.getProvider("GmsCore_OpenSSL");
      if (provider != null) {
        tlsExtensionType = TlsExtensionType.ALPN_AND_NPN;
      }

      // Check if on Android 5.0 or later.
      if (tlsExtensionType == null) {
        try {
          Class.forName("android.net.Network"); // Arbitrary class added in Android 5.0.
          tlsExtensionType = TlsExtensionType.ALPN_AND_NPN;
        } catch (ClassNotFoundException ignored) {
          // making checkstyle happy.
        }
      }

      // Check if on Android 4.1 or later.
      if (tlsExtensionType == null) {
        try {
          Class.forName("android.app.ActivityOptions"); // Arbitrary class added in Android 4.1.
          tlsExtensionType = TlsExtensionType.NPN;
        } catch (ClassNotFoundException ignored) {
          // making checkstyle happy.
        }
      }
    }

    @Override
    public String negotiate(SSLSocket sslSocket, String hostname, List<Protocol> protocols)
        throws IOException {
      // First check if a protocol has already been selected, since it's possible that the user
      // provided SSLSocketFactory has already done the handshake when creates the SSLSocket.
      String negotiatedProtocol = getSelectedProtocol(sslSocket);
      if (negotiatedProtocol == null) {
        negotiatedProtocol = super.negotiate(sslSocket, hostname, protocols);
      }
      return negotiatedProtocol;
    }

    /**
     * Override {@link Platform}'s configureTlsExtensions for Android older than 5.0, since OkHttp
     * (2.3+) only support such function for Android 5.0+.
     */
    @Override
    protected void configureTlsExtensions(
        SSLSocket sslSocket, String hostname, List<Protocol> protocols) {
      // Enable SNI and session tickets.
      if (hostname != null) {
        SET_USE_SESSION_TICKETS.invokeOptionalWithoutCheckedException(sslSocket, true);
        SET_HOSTNAME.invokeOptionalWithoutCheckedException(sslSocket, hostname);
      }

      Object[] parameters = {Platform.concatLengthPrefixed(protocols)};
      if (tlsExtensionType == TlsExtensionType.ALPN_AND_NPN) {
        SET_ALPN_PROTOCOLS.invokeWithoutCheckedException(sslSocket, parameters);
      }

      if (tlsExtensionType != null) {
        SET_NPN_PROTOCOLS.invokeWithoutCheckedException(sslSocket, parameters);
      } else {
        throw new RuntimeException("We can not do TLS handshake on this Android version, please"
            + " install the Google Play Services Dynamic Security Provider to use TLS");
      }
    }

    @Override
    public String getSelectedProtocol(SSLSocket socket) {
      if (tlsExtensionType == TlsExtensionType.ALPN_AND_NPN) {
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

      if (tlsExtensionType != null) {
        try {
          byte[] npnResult =
              (byte[]) GET_NPN_SELECTED_PROTOCOL.invokeWithoutCheckedException(socket);
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
  }
}
