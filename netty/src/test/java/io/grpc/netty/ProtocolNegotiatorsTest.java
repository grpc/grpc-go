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
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

import com.google.common.collect.Iterables;

import io.grpc.netty.ProtocolNegotiators.TlsChannelInboundHandlerAdapter;
import io.grpc.netty.ProtocolNegotiators.TlsNegotiator;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import io.netty.channel.ChannelPipeline;
import io.netty.channel.embedded.EmbeddedChannel;
import io.netty.handler.ssl.SslContext;
import io.netty.handler.ssl.SslHandler;
import io.netty.handler.ssl.SslHandshakeCompletionEvent;

import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.logging.Filter;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLException;

@RunWith(JUnit4.class)
public class ProtocolNegotiatorsTest {
  @Rule public final ExpectedException thrown = ExpectedException.none();

  private ChannelHandler grpcHandler = mock(ChannelHandler.class);

  private EmbeddedChannel channel = new EmbeddedChannel();
  private ChannelPipeline pipeline = channel.pipeline();
  private SslHandler sslHandler;
  private SSLEngine engine;
  private ChannelHandlerContext channelHandlerCtx;

  @Before
  public void setUp() throws Exception {
    engine = SSLContext.getDefault().createSSLEngine();
    sslHandler = new SslHandler(engine, false) {
      @Override
      public String applicationProtocol() {
        // Just get any of them.
        return Iterables.getFirst(GrpcSslContexts.HTTP2_VERSIONS, "");
      }
    };
  }

  @Test
  public void tlsHandler_failsOnNullEngine() throws Exception {
    thrown.expect(NullPointerException.class);
    thrown.expectMessage("ssl");

    ProtocolNegotiators.serverTls(null, null);
  }

  @Test
  public void tlsAdapter_exceptionClosesChannel() throws Exception {
    ChannelInboundHandlerAdapter handler =
        new TlsChannelInboundHandlerAdapter(sslHandler, grpcHandler);

    // Use addFirst due to the funny error handling in EmbeddedChannel.
    pipeline.addFirst(handler);

    pipeline.fireExceptionCaught(new Exception("bad"));

    assertFalse(channel.isOpen());
  }

  @Test
  public void tlsHandler_handlerAddedAddsSslHandler() throws Exception {
    ChannelInboundHandlerAdapter handler =
        new TlsChannelInboundHandlerAdapter(sslHandler, grpcHandler);

    pipeline.addLast(handler);

    assertEquals(sslHandler, pipeline.first());
  }

  @Test
  public void tlsHandler_userEventTriggeredNonSslEvent() throws Exception {
    ChannelInboundHandlerAdapter handler =
        new TlsChannelInboundHandlerAdapter(sslHandler, grpcHandler);
    pipeline.addLast(handler);
    channelHandlerCtx = pipeline.context(handler);
    Object nonSslEvent = new Object();

    pipeline.fireUserEventTriggered(nonSslEvent);

    // A non ssl event should not cause the grpcHandler to be in the pipeline yet.
    ChannelHandlerContext grpcHandlerCtx = pipeline.context(grpcHandler);
    assertNull(grpcHandlerCtx);
  }

  @Test
  public void tlsHandler_userEventTriggeredSslEvent_unsupportedProtocol() throws Exception {
    SslHandler badSslHandler = new SslHandler(engine, false) {
      @Override
      public String applicationProtocol() {
        return "badprotocol";
      }
    };

    ChannelInboundHandlerAdapter handler =
        new TlsChannelInboundHandlerAdapter(badSslHandler, grpcHandler);
    pipeline.addLast(handler);
    channelHandlerCtx = pipeline.context(handler);
    Object sslEvent = SslHandshakeCompletionEvent.SUCCESS;

    pipeline.fireUserEventTriggered(sslEvent);

    // No h2 protocol was specified, so this should be closed.
    assertFalse(channel.isOpen());
    ChannelHandlerContext grpcHandlerCtx = pipeline.context(grpcHandler);
    assertNull(grpcHandlerCtx);
  }

  @Test
  public void tlsHandler_userEventTriggeredSslEvent_handshakeFailure() throws Exception {
    ChannelInboundHandlerAdapter handler =
        new TlsChannelInboundHandlerAdapter(sslHandler, grpcHandler);
    pipeline.addLast(handler);
    channelHandlerCtx = pipeline.context(handler);
    Object sslEvent = new SslHandshakeCompletionEvent(new RuntimeException("bad"));

    pipeline.fireUserEventTriggered(sslEvent);

    // No h2 protocol was specified, so this should be closed.
    assertFalse(channel.isOpen());
    ChannelHandlerContext grpcHandlerCtx = pipeline.context(grpcHandler);
    assertNull(grpcHandlerCtx);
  }

  @Test
  public void tlsHandler_userEventTriggeredSslEvent_supportedProtocol() throws Exception {
    ChannelInboundHandlerAdapter handler =
        new TlsChannelInboundHandlerAdapter(sslHandler, grpcHandler);
    pipeline.addLast(handler);
    channelHandlerCtx = pipeline.context(handler);
    Object sslEvent = SslHandshakeCompletionEvent.SUCCESS;

    pipeline.fireUserEventTriggered(sslEvent);

    assertTrue(channel.isOpen());
    ChannelHandlerContext grpcHandlerCtx = pipeline.context(grpcHandler);
    assertNotNull(grpcHandlerCtx);
  }

  @Test
  public void engineLog() {
    ChannelInboundHandlerAdapter handler =
        new TlsChannelInboundHandlerAdapter(sslHandler, grpcHandler);
    pipeline.addLast(handler);
    channelHandlerCtx = pipeline.context(handler);

    Logger logger = Logger.getLogger(ProtocolNegotiators.class.getName());
    Filter oldFilter = logger.getFilter();
    try {
      logger.setFilter(new Filter() {
        @Override
        public boolean isLoggable(LogRecord record) {
          // We still want to the log method to be exercised, just not printed to stderr.
          return false;
        }
      });

      ProtocolNegotiators.logSslEngineDetails(
          Level.INFO, channelHandlerCtx, "message", new Exception("bad"));
    } finally {
      logger.setFilter(oldFilter);
    }
  }

  @Test
  public void tls_failsOnNullSslContext() {
    thrown.expect(NullPointerException.class);

    ProtocolNegotiators.tls(null, "authority");
  }

  @Test
  public void tls_hostAndPort() throws SSLException {
    SslContext ctx = GrpcSslContexts.forClient().build();
    TlsNegotiator negotiator = (TlsNegotiator) ProtocolNegotiators.tls(ctx, "authority:1234");

    assertEquals("authority", negotiator.getHost());
    assertEquals(1234, negotiator.getPort());
  }

  @Test
  public void tls_host() throws SSLException {
    SslContext ctx = GrpcSslContexts.forClient().build();
    TlsNegotiator negotiator = (TlsNegotiator) ProtocolNegotiators.tls(ctx, "[::1]");

    assertEquals("[::1]", negotiator.getHost());
    assertEquals(-1, negotiator.getPort());
  }

  @Test
  public void tls_invalidHost() throws SSLException {
    SslContext ctx = GrpcSslContexts.forClient().build();
    TlsNegotiator negotiator = (TlsNegotiator) ProtocolNegotiators.tls(ctx, "bad_host:1234");

    // Even though it looks like a port, we treat it as part of the authority, since the host is
    // invalid.
    assertEquals("bad_host:1234", negotiator.getHost());
    assertEquals(-1, negotiator.getPort());
  }
}
