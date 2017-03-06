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

package io.grpc.netty;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

import io.grpc.netty.InternalNettyChannelBuilder.OverrideAuthorityChecker;
import io.grpc.netty.ProtocolNegotiators.TlsNegotiator;
import io.netty.handler.ssl.SslContext;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import javax.net.ssl.SSLException;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class NettyChannelBuilderTest {

  @Rule public final ExpectedException thrown = ExpectedException.none();
  private final SslContext noSslContext = null;

  @Test
  public void overrideAllowsInvalidAuthority() {
    NettyChannelBuilder builder = new NettyChannelBuilder(new SocketAddress(){});
    InternalNettyChannelBuilder.overrideAuthorityChecker(builder, new OverrideAuthorityChecker() {
      @Override
      public String checkAuthority(String authority) {
        return authority;
      }
    });
    Object unused = builder.overrideAuthority("[invalidauthority")
        .negotiationType(NegotiationType.PLAINTEXT)
        .buildTransportFactory();
  }

  @Test
  public void failOverrideInvalidAuthority() {
    NettyChannelBuilder builder = new NettyChannelBuilder(new SocketAddress(){});

    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("Invalid authority:");

    builder.overrideAuthority("[invalidauthority");
  }

  @Test
  public void failInvalidAuthority() {
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("Invalid host or port");

    Object unused =
        NettyChannelBuilder.forAddress(new InetSocketAddress("invalid_authority", 1234));
  }

  @Test
  public void sslContextCanBeNull() {
    NettyChannelBuilder builder = new NettyChannelBuilder(new SocketAddress(){});
    builder.sslContext(null);
  }

  @Test
  public void failIfSslContextIsNotClient() {
    SslContext sslContext = mock(SslContext.class);
    NettyChannelBuilder builder = new NettyChannelBuilder(new SocketAddress(){});

    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("Server SSL context can not be used for client channel");

    builder.sslContext(sslContext);
  }

  @Test
  public void createProtocolNegotiator_plaintext() {
    ProtocolNegotiator negotiator = NettyChannelBuilder.createProtocolNegotiator(
        "authority",
        NegotiationType.PLAINTEXT,
        noSslContext);
    // just check that the classes are the same, and that negotiator is not null.
    assertTrue(negotiator instanceof ProtocolNegotiators.PlaintextNegotiator);
  }

  @Test
  public void createProtocolNegotiator_plaintextUpgrade() {
    ProtocolNegotiator negotiator = NettyChannelBuilder.createProtocolNegotiator(
        "authority",
        NegotiationType.PLAINTEXT_UPGRADE,
        noSslContext);
    // just check that the classes are the same, and that negotiator is not null.
    assertTrue(negotiator instanceof ProtocolNegotiators.PlaintextUpgradeNegotiator);
  }

  @Test
  public void createProtocolNegotiator_tlsWithNoContext() {
    ProtocolNegotiator negotiator = NettyChannelBuilder.createProtocolNegotiator(
        "authority:1234",
        NegotiationType.TLS,
        noSslContext);

    assertTrue(negotiator instanceof ProtocolNegotiators.TlsNegotiator);
    ProtocolNegotiators.TlsNegotiator n = (TlsNegotiator) negotiator;

    assertEquals("authority", n.getHost());
    assertEquals(1234, n.getPort());
  }

  @Test
  public void createProtocolNegotiator_tlsWithClientContext() throws SSLException {
    ProtocolNegotiator negotiator = NettyChannelBuilder.createProtocolNegotiator(
        "authority:1234",
        NegotiationType.TLS,
        GrpcSslContexts.forClient().build());

    assertTrue(negotiator instanceof ProtocolNegotiators.TlsNegotiator);
    ProtocolNegotiators.TlsNegotiator n = (TlsNegotiator) negotiator;

    assertEquals("authority", n.getHost());
    assertEquals(1234, n.getPort());
  }

  @Test
  public void createProtocolNegotiator_tlsWithAuthorityFallback() {
    ProtocolNegotiator negotiator = NettyChannelBuilder.createProtocolNegotiator(
        "bad_authority",
        NegotiationType.TLS,
        noSslContext);

    assertTrue(negotiator instanceof ProtocolNegotiators.TlsNegotiator);
    ProtocolNegotiators.TlsNegotiator n = (TlsNegotiator) negotiator;

    assertEquals("bad_authority", n.getHost());
    assertEquals(-1, n.getPort());
  }
}
