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
import static org.junit.Assert.assertNull;

import com.squareup.okhttp.ConnectionSpec;
import io.grpc.NameResolver;
import io.grpc.internal.GrpcUtil;
import java.net.InetSocketAddress;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link OkHttpChannelBuilder}.
 */
@RunWith(JUnit4.class)
public class OkHttpChannelBuilderTest {

  @Rule public final ExpectedException thrown = ExpectedException.none();

  @Test
  public void authorityIsReadable() {
    OkHttpChannelBuilder builder = OkHttpChannelBuilder.forAddress("original", 1234);
    assertEquals("original:1234", builder.build().authority());
  }

  @Test
  public void overrideAuthorityIsReadableForAddress() {
    OkHttpChannelBuilder builder = OkHttpChannelBuilder.forAddress("original", 1234);
    overrideAuthorityIsReadableHelper(builder, "override:5678");
  }

  @Test
  public void overrideAuthorityIsReadableForTarget() {
    OkHttpChannelBuilder builder = OkHttpChannelBuilder.forTarget("original:1234");
    overrideAuthorityIsReadableHelper(builder, "override:5678");
  }

  private void overrideAuthorityIsReadableHelper(OkHttpChannelBuilder builder,
      String overrideAuthority) {
    builder.overrideAuthority(overrideAuthority);
    assertEquals(overrideAuthority, builder.build().authority());
  }

  @Test
  public void overrideAllowsInvalidAuthority() {
    OkHttpChannelBuilder builder = new OkHttpChannelBuilder("good", 1234) {
      @Override
      protected String checkAuthority(String authority) {
        return authority;
      }
    };

    builder.overrideAuthority("[invalidauthority")
        .negotiationType(NegotiationType.PLAINTEXT)
        .buildTransportFactory();
  }

  @Test
  public void failOverrideInvalidAuthority() {
    OkHttpChannelBuilder builder = new OkHttpChannelBuilder("good", 1234);

    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("Invalid authority:");
    builder.overrideAuthority("[invalidauthority");
  }

  @Test
  public void failInvalidAuthority() {
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("Invalid host or port");

    OkHttpChannelBuilder.forAddress("invalid_authority", 1234);
  }

  @Test
  public void failForUsingClearTextSpecDirectly() {
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("plaintext ConnectionSpec is not accepted");

    OkHttpChannelBuilder.forAddress("host", 1234).connectionSpec(ConnectionSpec.CLEARTEXT);
  }

  @Test
  public void allowUsingTlsConnectionSpec() {
    OkHttpChannelBuilder.forAddress("host", 1234).connectionSpec(ConnectionSpec.MODERN_TLS);
  }

  @Test
  public void usePlaintext_newClientTransportAllowed() {
    OkHttpChannelBuilder builder = OkHttpChannelBuilder.forAddress("host", 1234).usePlaintext(true);
    builder.buildTransportFactory().newClientTransport(new InetSocketAddress(5678),
        "dummy_authority", "dummy_userAgent");
  }

  @Test
  public void usePlaintextDefaultPort() {
    OkHttpChannelBuilder builder = OkHttpChannelBuilder.forAddress("host", 1234).usePlaintext(true);
    assertEquals(GrpcUtil.DEFAULT_PORT_PLAINTEXT,
        builder.getNameResolverParams().get(NameResolver.Factory.PARAMS_DEFAULT_PORT).intValue());
  }

  @Test
  public void usePlaintextCreatesNullSocketFactory() {
    OkHttpChannelBuilder builder = OkHttpChannelBuilder.forAddress("host", 1234);
    assertNotNull(builder.createSocketFactory());

    builder.usePlaintext(true);
    assertNull(builder.createSocketFactory());
  }
}

