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

package io.grpc.okhttp;

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.collect.ImmutableList;
import io.grpc.okhttp.OkHttpProtocolNegotiator.AndroidNegotiator;
import io.grpc.okhttp.OkHttpProtocolNegotiator.AndroidNegotiator.TlsExtensionType;
import io.grpc.okhttp.internal.Platform;
import io.grpc.okhttp.internal.Protocol;
import java.io.IOException;
import java.security.Provider;
import java.security.Security;
import javax.net.ssl.HandshakeCompletedListener;
import javax.net.ssl.SSLSession;
import javax.net.ssl.SSLSocket;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mockito;

/**
 * Tests for {@link OkHttpProtocolNegotiator}.
 */
@RunWith(JUnit4.class)
public class OkHttpProtocolNegotiatorTest {
  @Rule public final ExpectedException thrown = ExpectedException.none();

  private final Provider fakeSecurityProvider = new Provider("GmsCore_OpenSSL", 1.0, "info") {};
  private final SSLSocket sock = mock(SSLSocket.class);
  private final Platform platform = mock(Platform.class);

  @Before
  public void setUp() {
    // Tests that depend on android need this to know which protocol negotiation to use.
    Security.addProvider(fakeSecurityProvider);
  }

  @After
  public void tearDown() {
    Security.removeProvider(fakeSecurityProvider.getName());
  }

  @Test
  public void createNegotiator_isAndroid() {
    ClassLoader cl = new ClassLoader(this.getClass().getClassLoader()) {
      @Override
      protected Class<?> findClass(String name) throws ClassNotFoundException {
        // Just don't throw.
        if ("com.android.org.conscrypt.OpenSSLSocketImpl".equals(name)) {
          return null;
        }
        return super.findClass(name);
      }
    };

    OkHttpProtocolNegotiator negotiator = OkHttpProtocolNegotiator.createNegotiator(cl);
    assertEquals(AndroidNegotiator.class, negotiator.getClass());
  }

  @Test
  public void createNegotiator_isAndroidLegacy() {
    ClassLoader cl = new ClassLoader(this.getClass().getClassLoader()) {
      @Override
      protected Class<?> findClass(String name) throws ClassNotFoundException {
        // Just don't throw.
        if ("org.apache.harmony.xnet.provider.jsse.OpenSSLSocketImpl".equals(name)) {
          return null;
        }
        return super.findClass(name);
      }
    };

    OkHttpProtocolNegotiator negotiator = OkHttpProtocolNegotiator.createNegotiator(cl);

    assertEquals(AndroidNegotiator.class, negotiator.getClass());
  }

  @Test
  public void createNegotiator_notAndroid() {
    ClassLoader cl = new ClassLoader(this.getClass().getClassLoader()) {
      @Override
      protected Class<?> findClass(String name) throws ClassNotFoundException {
        if ("com.android.org.conscrypt.OpenSSLSocketImpl".equals(name)) {
          throw new ClassNotFoundException();
        }
        return super.findClass(name);
      }
    };

    OkHttpProtocolNegotiator negotiator = OkHttpProtocolNegotiator.createNegotiator(cl);

    assertEquals(OkHttpProtocolNegotiator.class, negotiator.getClass());
  }

  @Test
  public void negotiatorNotNull() {
    assertNotNull(OkHttpProtocolNegotiator.get());
  }

  @Test
  public void negotiate_handshakeFails() throws IOException {
    OkHttpProtocolNegotiator negotiator = OkHttpProtocolNegotiator.get();
    doThrow(new IOException()).when(sock).startHandshake();
    thrown.expect(IOException.class);

    negotiator.negotiate(sock, "hostname", ImmutableList.of(Protocol.HTTP_2));
  }

  @Test
  public void negotiate_noSelectedProtocol() throws Exception {
    Platform platform = mock(Platform.class);

    OkHttpProtocolNegotiator negotiator = new OkHttpProtocolNegotiator(platform);

    thrown.expect(RuntimeException.class);
    thrown.expectMessage("protocol negotiation failed");

    negotiator.negotiate(sock, "hostname", ImmutableList.of(Protocol.HTTP_2));
  }

  @Test
  public void negotiate_success() throws Exception {
    when(platform.getSelectedProtocol(Mockito.<SSLSocket>any())).thenReturn("h2");
    OkHttpProtocolNegotiator negotiator = new OkHttpProtocolNegotiator(platform);

    String actual = negotiator.negotiate(sock, "hostname", ImmutableList.of(Protocol.HTTP_2));

    assertEquals("h2", actual);
    verify(sock).startHandshake();
    verify(platform).getSelectedProtocol(sock);
    verify(platform).afterHandshake(sock);
  }

  @Test
  public void negotiate_preferGrpcExp() throws Exception {
    // This test doesn't actually verify that grpc-exp is preferred, since the
    // mocking of getSelectedProtocol() causes the protocol list to be ignored.
    // The main usefulness of the test is for future changes to
    // OkHttpProtocolNegotiator, where we can catch any change that would affect
    // grpc-exp preference.
    when(platform.getSelectedProtocol(Mockito.<SSLSocket>any())).thenReturn("grpc-exp");
    OkHttpProtocolNegotiator negotiator = new OkHttpProtocolNegotiator(platform);

    String actual =
        negotiator.negotiate(sock, "hostname",
                ImmutableList.of(Protocol.GRPC_EXP, Protocol.HTTP_2));

    assertEquals("grpc-exp", actual);
    verify(sock).startHandshake();
    verify(platform).getSelectedProtocol(sock);
    verify(platform).afterHandshake(sock);
  }

  @Test
  public void pickTlsExtensionType_securityProvider() throws Exception {
    assertNotNull(Security.getProvider(fakeSecurityProvider.getName()));

    AndroidNegotiator.TlsExtensionType tlsExtensionType =
        AndroidNegotiator.pickTlsExtensionType(getClass().getClassLoader());

    assertEquals(TlsExtensionType.ALPN_AND_NPN, tlsExtensionType);
  }

  @Test
  public void pickTlsExtensionType_android50() throws Exception {
    Security.removeProvider(fakeSecurityProvider.getName());
    ClassLoader cl = new ClassLoader(this.getClass().getClassLoader()) {
      @Override
      protected Class<?> findClass(String name) throws ClassNotFoundException {
        // Just don't throw.
        if ("android.net.Network".equals(name)) {
          return null;
        }
        return super.findClass(name);
      }
    };

    AndroidNegotiator.TlsExtensionType tlsExtensionType =
        AndroidNegotiator.pickTlsExtensionType(cl);

    assertEquals(TlsExtensionType.ALPN_AND_NPN, tlsExtensionType);
  }

  @Test
  public void pickTlsExtensionType_android41() throws Exception {
    Security.removeProvider(fakeSecurityProvider.getName());
    ClassLoader cl = new ClassLoader(this.getClass().getClassLoader()) {
      @Override
      protected Class<?> findClass(String name) throws ClassNotFoundException {
        // Just don't throw.
        if ("android.app.ActivityOptions".equals(name)) {
          return null;
        }
        return super.findClass(name);
      }
    };

    AndroidNegotiator.TlsExtensionType tlsExtensionType =
        AndroidNegotiator.pickTlsExtensionType(cl);

    assertEquals(TlsExtensionType.NPN, tlsExtensionType);
  }

  @Test
  public void pickTlsExtensionType_none() throws Exception {
    Security.removeProvider(fakeSecurityProvider.getName());

    AndroidNegotiator.TlsExtensionType tlsExtensionType =
        AndroidNegotiator.pickTlsExtensionType(getClass().getClassLoader());

    assertNull(tlsExtensionType);
  }

  @Test
  public void androidNegotiator_failsOnNull() {
    thrown.expect(NullPointerException.class);
    thrown.expectMessage("Unable to pick a TLS extension");

    new AndroidNegotiator(platform, null);
  }

  // Checks that the super class is properly invoked.
  @Test
  public void negotiate_android_handshakeFails() throws Exception {
    AndroidNegotiator negotiator = new AndroidNegotiator(platform, TlsExtensionType.ALPN_AND_NPN);

    FakeAndroidSslSocket androidSock = new FakeAndroidSslSocket() {
      @Override
      public void startHandshake() throws IOException {
        throw new IOException("expected");
      }
    };

    thrown.expect(IOException.class);
    thrown.expectMessage("expected");

    negotiator.negotiate(androidSock, "hostname", ImmutableList.of(Protocol.HTTP_2));
  }

  @VisibleForTesting
  public static class FakeAndroidSslSocketAlpn extends FakeAndroidSslSocket {
    @Override
    public byte[] getAlpnSelectedProtocol() {
      return "h2".getBytes(UTF_8);
    }
  }

  @Test
  public void getSelectedProtocol_alpn() throws Exception {
    AndroidNegotiator negotiator = new AndroidNegotiator(platform, TlsExtensionType.ALPN_AND_NPN);
    FakeAndroidSslSocket androidSock = new FakeAndroidSslSocketAlpn();

    String actual = negotiator.getSelectedProtocol(androidSock);

    assertEquals("h2", actual);
  }

  @VisibleForTesting
  public static class FakeAndroidSslSocketNpn extends FakeAndroidSslSocket {
    @Override
    public byte[] getNpnSelectedProtocol() {
      return "h2".getBytes(UTF_8);
    }
  }

  @Test
  public void getSelectedProtocol_npn() throws Exception {
    AndroidNegotiator negotiator = new AndroidNegotiator(platform, TlsExtensionType.NPN);
    FakeAndroidSslSocket androidSock = new FakeAndroidSslSocketNpn();

    String actual = negotiator.getSelectedProtocol(androidSock);

    assertEquals("h2", actual);
  }

  // A fake of org.conscrypt.OpenSSLSocketImpl
  @VisibleForTesting // Must be public for reflection to work
  public static class FakeAndroidSslSocket extends SSLSocket {

    public void setUseSessionTickets(boolean arg) {}

    public void setHostname(String arg) {}

    public byte[] getAlpnSelectedProtocol() {
      return null;
    }

    public void setAlpnProtocols(byte[] arg) {}

    public byte[] getNpnSelectedProtocol() {
      return null;
    }

    public void setNpnProtocols(byte[] arg) {}

    @Override
    public void addHandshakeCompletedListener(HandshakeCompletedListener listener) {}

    @Override
    public boolean getEnableSessionCreation() {
      return false;
    }

    @Override
    public String[] getEnabledCipherSuites() {
      return null;
    }

    @Override
    public String[] getEnabledProtocols() {
      return null;
    }

    @Override
    public boolean getNeedClientAuth() {
      return false;
    }

    @Override
    public SSLSession getSession() {
      return null;
    }

    @Override
    public String[] getSupportedCipherSuites() {
      return null;
    }

    @Override
    public String[] getSupportedProtocols() {
      return null;
    }

    @Override
    public boolean getUseClientMode() {
      return false;
    }

    @Override
    public boolean getWantClientAuth() {
      return false;
    }

    @Override
    public void removeHandshakeCompletedListener(HandshakeCompletedListener listener) {}

    @Override
    public void setEnableSessionCreation(boolean flag) {}

    @Override
    public void setEnabledCipherSuites(String[] suites) {}

    @Override
    public void setEnabledProtocols(String[] protocols) {}

    @Override
    public void setNeedClientAuth(boolean need) {}

    @Override
    public void setUseClientMode(boolean mode) {}

    @Override
    public void setWantClientAuth(boolean want) {}

    @Override
    public void startHandshake() throws IOException {}
  }
}
