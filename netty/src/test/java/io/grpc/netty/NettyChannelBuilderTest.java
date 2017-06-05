/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

package io.grpc.netty;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

import io.grpc.ManagedChannel;
import io.grpc.netty.InternalNettyChannelBuilder.OverrideAuthorityChecker;
import io.grpc.netty.ProtocolNegotiators.TlsNegotiator;
import io.netty.handler.ssl.SslContext;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.TimeUnit;
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
  public void authorityIsReadable() {
    NettyChannelBuilder builder = NettyChannelBuilder.forAddress("original", 1234);
    assertEquals("original:1234", builder.build().authority());
  }

  @Test
  public void overrideAuthorityIsReadableForAddress() {
    NettyChannelBuilder builder = NettyChannelBuilder.forAddress("original", 1234);
    overrideAuthorityIsReadableHelper(builder, "override:5678");
  }

  @Test
  public void overrideAuthorityIsReadableForTarget() {
    NettyChannelBuilder builder = NettyChannelBuilder.forTarget("original:1234");
    overrideAuthorityIsReadableHelper(builder, "override:5678");
  }

  @Test
  public void overrideAuthorityIsReadableForSocketAddress() {
    NettyChannelBuilder builder = NettyChannelBuilder.forAddress(new SocketAddress(){});
    overrideAuthorityIsReadableHelper(builder, "override:5678");
  }

  private void overrideAuthorityIsReadableHelper(NettyChannelBuilder builder,
      String overrideAuthority) {
    builder.overrideAuthority(overrideAuthority);
    ManagedChannel channel = builder.build();
    assertEquals(overrideAuthority, channel.authority());
  }

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
    thrown.expect(NullPointerException.class);
    NettyChannelBuilder.createProtocolNegotiator(
        "authority:1234",
        NegotiationType.TLS,
        noSslContext);
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
  public void createProtocolNegotiator_tlsWithAuthorityFallback() throws SSLException {
    ProtocolNegotiator negotiator = NettyChannelBuilder.createProtocolNegotiator(
        "bad_authority",
        NegotiationType.TLS,
        GrpcSslContexts.forClient().build());

    assertTrue(negotiator instanceof ProtocolNegotiators.TlsNegotiator);
    ProtocolNegotiators.TlsNegotiator n = (TlsNegotiator) negotiator;

    assertEquals("bad_authority", n.getHost());
    assertEquals(-1, n.getPort());
  }

  @Test
  public void negativeKeepAliveTime() {
    NettyChannelBuilder builder = NettyChannelBuilder.forTarget("fakeTarget");

    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("keepalive time must be positive");
    builder.keepAliveTime(-1L, TimeUnit.HOURS);
  }

  @Test
  public void negativeKeepAliveTimeout() {
    NettyChannelBuilder builder = NettyChannelBuilder.forTarget("fakeTarget");

    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("keepalive timeout must be positive");
    builder.keepAliveTimeout(-1L, TimeUnit.HOURS);
  }
}
