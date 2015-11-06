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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableList;

import io.grpc.okhttp.internal.Platform;
import io.grpc.okhttp.internal.Protocol;

import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mockito;

import java.io.IOException;

import javax.net.ssl.SSLSocket;

/**
 * Tests for {@link OkHttpProtocolNegotiator}.
 */
@RunWith(JUnit4.class)
public class OkHttpProtocolNegotiatorTest {
  @Rule public final ExpectedException thrown = ExpectedException.none();

  private SSLSocket sock = mock(SSLSocket.class);

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

    assertEquals(OkHttpProtocolNegotiator.AndroidNegotiator.class, negotiator.getClass());
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

    assertEquals(OkHttpProtocolNegotiator.AndroidNegotiator.class, negotiator.getClass());
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
    Platform platform = mock(Platform.class);
    when(platform.getSelectedProtocol(Mockito.<SSLSocket>any())).thenReturn("h2");
    OkHttpProtocolNegotiator negotiator = new OkHttpProtocolNegotiator(platform);

    String actual = negotiator.negotiate(sock, "hostname", ImmutableList.of(Protocol.HTTP_2));

    assertEquals("h2", actual);
    verify(sock).startHandshake();
    verify(platform).getSelectedProtocol(sock);
    verify(platform).afterHandshake(sock);
  }
}

