/*
 * Copyright 2016, Google Inc. All rights reserved.
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

import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import io.netty.handler.ssl.SslContext;
import java.util.concurrent.TimeUnit;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;


/**
 * Unit tests for {@link NettyServerBuilder}.
 */
@RunWith(JUnit4.class)
public class NettyServerBuilderTest {

  @Rule public final ExpectedException thrown = ExpectedException.none();

  @Test
  public void sslContextCanBeNull() {
    NettyServerBuilder builder = NettyServerBuilder.forPort(8080);
    builder.sslContext(null);
  }

  @Test
  public void failIfSslContextIsNotServer() {
    SslContext sslContext = mock(SslContext.class);
    when(sslContext.isClient()).thenReturn(true);

    NettyServerBuilder builder = NettyServerBuilder.forPort(8080);

    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("Client SSL context can not be used for server");
    builder.sslContext(sslContext);
  }

  @Test
  public void failIfKeepAliveTimeNegative() {
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("keepalive time must be positive");

    NettyServerBuilder.forPort(8080).keepAliveTime(-10L, TimeUnit.HOURS);
  }

  @Test
  public void failIfKeepAliveTimeoutNegative() {
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("keepalive timeout must be positive");

    NettyServerBuilder.forPort(8080).keepAliveTimeout(-10L, TimeUnit.HOURS);
  }
}
