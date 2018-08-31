/*
 * Copyright 2015 The gRPC Authors
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

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;

import io.grpc.internal.testing.TestUtils;
import io.grpc.netty.ProtocolNegotiators.HostPort;
import io.grpc.netty.ProtocolNegotiators.ServerTlsHandler;
import io.grpc.netty.ProtocolNegotiators.TlsNegotiator;
import io.netty.bootstrap.Bootstrap;
import io.netty.bootstrap.ServerBootstrap;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufUtil;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandler;
import io.netty.channel.ChannelPipeline;
import io.netty.channel.DefaultEventLoopGroup;
import io.netty.channel.embedded.EmbeddedChannel;
import io.netty.channel.local.LocalAddress;
import io.netty.channel.local.LocalChannel;
import io.netty.channel.local.LocalServerChannel;
import io.netty.handler.proxy.ProxyConnectException;
import io.netty.handler.ssl.SslContext;
import io.netty.handler.ssl.SslHandler;
import io.netty.handler.ssl.SslHandshakeCompletionEvent;
import io.netty.handler.ssl.SupportedCipherSuiteFilter;
import java.io.File;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.logging.Filter;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLException;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.rules.Timeout;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mockito;

@RunWith(JUnit4.class)
public class ProtocolNegotiatorsTest {
  private static final Runnable NOOP_RUNNABLE = new Runnable() {
    @Override public void run() {}
  };

  @Rule public final Timeout globalTimeout = Timeout.seconds(5);

  @Rule
  public final ExpectedException thrown = ExpectedException.none();

  private GrpcHttp2ConnectionHandler grpcHandler = mock(GrpcHttp2ConnectionHandler.class);

  private EmbeddedChannel channel = new EmbeddedChannel();
  private ChannelPipeline pipeline = channel.pipeline();
  private SslContext sslContext;
  private SSLEngine engine;
  private ChannelHandlerContext channelHandlerCtx;

  @Before
  public void setUp() throws Exception {
    File serverCert = TestUtils.loadCert("server1.pem");
    File key = TestUtils.loadCert("server1.key");
    sslContext = GrpcSslContexts.forServer(serverCert, key)
        .ciphers(TestUtils.preferredTestCiphers(), SupportedCipherSuiteFilter.INSTANCE).build();
    engine = SSLContext.getDefault().createSSLEngine();
  }

  @Test
  public void tlsHandler_failsOnNullEngine() throws Exception {
    thrown.expect(NullPointerException.class);
    thrown.expectMessage("ssl");

    Object unused = ProtocolNegotiators.serverTls(null);
  }

  @Test
  public void tlsAdapter_exceptionClosesChannel() throws Exception {
    ChannelHandler handler = new ServerTlsHandler(sslContext, grpcHandler);

    // Use addFirst due to the funny error handling in EmbeddedChannel.
    pipeline.addFirst(handler);

    pipeline.fireExceptionCaught(new Exception("bad"));

    assertFalse(channel.isOpen());
  }

  @Test
  public void tlsHandler_handlerAddedAddsSslHandler() throws Exception {
    ChannelHandler handler = new ServerTlsHandler(sslContext, grpcHandler);

    pipeline.addLast(handler);

    assertTrue(pipeline.first() instanceof SslHandler);
  }

  @Test
  public void tlsHandler_userEventTriggeredNonSslEvent() throws Exception {
    ChannelHandler handler = new ServerTlsHandler(sslContext, grpcHandler);
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

    ChannelHandler handler = new ServerTlsHandler(sslContext, grpcHandler);
    pipeline.addLast(handler);

    pipeline.replace(SslHandler.class, null, badSslHandler);
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
    ChannelHandler handler = new ServerTlsHandler(sslContext, grpcHandler);
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
  public void tlsHandler_userEventTriggeredSslEvent_supportedProtocolH2() throws Exception {
    SslHandler goodSslHandler = new SslHandler(engine, false) {
      @Override
      public String applicationProtocol() {
        return "h2";
      }
    };

    ChannelHandler handler = new ServerTlsHandler(sslContext, grpcHandler);
    pipeline.addLast(handler);

    pipeline.replace(SslHandler.class, null, goodSslHandler);
    channelHandlerCtx = pipeline.context(handler);
    Object sslEvent = SslHandshakeCompletionEvent.SUCCESS;

    pipeline.fireUserEventTriggered(sslEvent);

    assertTrue(channel.isOpen());
    ChannelHandlerContext grpcHandlerCtx = pipeline.context(grpcHandler);
    assertNotNull(grpcHandlerCtx);
  }

  @Test
  public void tlsHandler_userEventTriggeredSslEvent_supportedProtocolGrpcExp() throws Exception {
    SslHandler goodSslHandler = new SslHandler(engine, false) {
      @Override
      public String applicationProtocol() {
        return "grpc-exp";
      }
    };

    ChannelHandler handler = new ServerTlsHandler(sslContext, grpcHandler);
    pipeline.addLast(handler);

    pipeline.replace(SslHandler.class, null, goodSslHandler);
    channelHandlerCtx = pipeline.context(handler);
    Object sslEvent = SslHandshakeCompletionEvent.SUCCESS;

    pipeline.fireUserEventTriggered(sslEvent);

    assertTrue(channel.isOpen());
    ChannelHandlerContext grpcHandlerCtx = pipeline.context(grpcHandler);
    assertNotNull(grpcHandlerCtx);
  }

  @Test
  public void engineLog() {
    ChannelHandler handler = new ServerTlsHandler(sslContext, grpcHandler);
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

    Object unused = ProtocolNegotiators.tls(null);
  }

  @Test
  public void tls_hostAndPort() throws SSLException {
    SslContext ctx = GrpcSslContexts.forClient().build();
    TlsNegotiator negotiator = (TlsNegotiator) ProtocolNegotiators.tls(ctx);
    HostPort hostPort = negotiator.parseAuthority("authority:1234");

    assertEquals("authority", hostPort.host);
    assertEquals(1234, hostPort.port);
  }

  @Test
  public void tls_host() throws SSLException {
    SslContext ctx = GrpcSslContexts.forClient().build();
    TlsNegotiator negotiator = (TlsNegotiator) ProtocolNegotiators.tls(ctx);
    HostPort hostPort = negotiator.parseAuthority("[::1]");

    assertEquals("[::1]", hostPort.host);
    assertEquals(-1, hostPort.port);
  }

  @Test
  public void tls_invalidHost() throws SSLException {
    SslContext ctx = GrpcSslContexts.forClient().build();
    TlsNegotiator negotiator = (TlsNegotiator) ProtocolNegotiators.tls(ctx);
    HostPort hostPort = negotiator.parseAuthority("bad_host:1234");

    // Even though it looks like a port, we treat it as part of the authority, since the host is
    // invalid.
    assertEquals("bad_host:1234", hostPort.host);
    assertEquals(-1, hostPort.port);
  }

  @Test
  public void httpProxy_nullAddressNpe() throws Exception {
    thrown.expect(NullPointerException.class);
    Object unused =
        ProtocolNegotiators.httpProxy(null, "user", "pass", ProtocolNegotiators.plaintext());
  }

  @Test
  public void httpProxy_nullNegotiatorNpe() throws Exception {
    thrown.expect(NullPointerException.class);
    Object unused = ProtocolNegotiators.httpProxy(
        InetSocketAddress.createUnresolved("localhost", 80), "user", "pass", null);
  }

  @Test
  public void httpProxy_nullUserPassNoException() throws Exception {
    assertNotNull(ProtocolNegotiators.httpProxy(
        InetSocketAddress.createUnresolved("localhost", 80), null, null,
        ProtocolNegotiators.plaintext()));
  }

  @Test
  public void httpProxy_completes() throws Exception {
    DefaultEventLoopGroup elg = new DefaultEventLoopGroup(1);
    // ProxyHandler is incompatible with EmbeddedChannel because when channelRegistered() is called
    // the channel is already active.
    LocalAddress proxy = new LocalAddress("httpProxy_completes");
    SocketAddress host = InetSocketAddress.createUnresolved("specialHost", 314);

    ChannelInboundHandler mockHandler = mock(ChannelInboundHandler.class);
    Channel serverChannel = new ServerBootstrap().group(elg).channel(LocalServerChannel.class)
        .childHandler(mockHandler)
        .bind(proxy).sync().channel();

    ProtocolNegotiator nego =
        ProtocolNegotiators.httpProxy(proxy, null, null, ProtocolNegotiators.plaintext());
    ChannelHandler handler = nego.newHandler(grpcHandler);
    Channel channel = new Bootstrap().group(elg).channel(LocalChannel.class).handler(handler)
        .register().sync().channel();
    pipeline = channel.pipeline();
    // Wait for initialization to complete
    channel.eventLoop().submit(NOOP_RUNNABLE).sync();
    // The grpcHandler must be in the pipeline, but we don't actually want it during our test
    // because it will consume all events since it is a mock. We only use it because it is required
    // to construct the Handler.
    pipeline.remove(grpcHandler);
    channel.connect(host).sync();
    serverChannel.close();
    ArgumentCaptor<ChannelHandlerContext> contextCaptor =
        ArgumentCaptor.forClass(ChannelHandlerContext.class);
    Mockito.verify(mockHandler).channelActive(contextCaptor.capture());
    ChannelHandlerContext serverContext = contextCaptor.getValue();

    final String golden = "isThisThingOn?";
    ChannelFuture negotiationFuture = channel.writeAndFlush(bb(golden, channel));

    // Wait for sending initial request to complete
    channel.eventLoop().submit(NOOP_RUNNABLE).sync();
    ArgumentCaptor<Object> objectCaptor = ArgumentCaptor.forClass(Object.class);
    Mockito.verify(mockHandler)
        .channelRead(any(ChannelHandlerContext.class), objectCaptor.capture());
    ByteBuf b = (ByteBuf) objectCaptor.getValue();
    String request = b.toString(UTF_8);
    b.release();
    assertTrue("No trailing newline: " + request, request.endsWith("\r\n\r\n"));
    assertTrue("No CONNECT: " + request, request.startsWith("CONNECT specialHost:314 "));
    assertTrue("No host header: " + request, request.contains("host: specialHost:314"));

    assertFalse(negotiationFuture.isDone());
    serverContext.writeAndFlush(bb("HTTP/1.1 200 OK\r\n\r\n", serverContext.channel())).sync();
    negotiationFuture.sync();

    channel.eventLoop().submit(NOOP_RUNNABLE).sync();
    objectCaptor.getAllValues().clear();
    Mockito.verify(mockHandler, times(2))
        .channelRead(any(ChannelHandlerContext.class), objectCaptor.capture());
    b = (ByteBuf) objectCaptor.getAllValues().get(1);
    // If we were using the real grpcHandler, this would have been the HTTP/2 preface
    String preface = b.toString(UTF_8);
    b.release();
    assertEquals(golden, preface);

    channel.close();
  }

  @Test
  public void httpProxy_500() throws Exception {
    DefaultEventLoopGroup elg = new DefaultEventLoopGroup(1);
    // ProxyHandler is incompatible with EmbeddedChannel because when channelRegistered() is called
    // the channel is already active.
    LocalAddress proxy = new LocalAddress("httpProxy_500");
    SocketAddress host = InetSocketAddress.createUnresolved("specialHost", 314);

    ChannelInboundHandler mockHandler = mock(ChannelInboundHandler.class);
    Channel serverChannel = new ServerBootstrap().group(elg).channel(LocalServerChannel.class)
        .childHandler(mockHandler)
        .bind(proxy).sync().channel();

    ProtocolNegotiator nego =
        ProtocolNegotiators.httpProxy(proxy, null, null, ProtocolNegotiators.plaintext());
    ChannelHandler handler = nego.newHandler(grpcHandler);
    Channel channel = new Bootstrap().group(elg).channel(LocalChannel.class).handler(handler)
        .register().sync().channel();
    pipeline = channel.pipeline();
    // Wait for initialization to complete
    channel.eventLoop().submit(NOOP_RUNNABLE).sync();
    // The grpcHandler must be in the pipeline, but we don't actually want it during our test
    // because it will consume all events since it is a mock. We only use it because it is required
    // to construct the Handler.
    pipeline.remove(grpcHandler);
    channel.connect(host).sync();
    serverChannel.close();
    ArgumentCaptor<ChannelHandlerContext> contextCaptor =
        ArgumentCaptor.forClass(ChannelHandlerContext.class);
    Mockito.verify(mockHandler).channelActive(contextCaptor.capture());
    ChannelHandlerContext serverContext = contextCaptor.getValue();

    final String golden = "isThisThingOn?";
    ChannelFuture negotiationFuture = channel.writeAndFlush(bb(golden, channel));

    // Wait for sending initial request to complete
    channel.eventLoop().submit(NOOP_RUNNABLE).sync();
    ArgumentCaptor<Object> objectCaptor = ArgumentCaptor.forClass(Object.class);
    Mockito.verify(mockHandler)
        .channelRead(any(ChannelHandlerContext.class), objectCaptor.capture());
    ByteBuf request = (ByteBuf) objectCaptor.getValue();
    request.release();

    assertFalse(negotiationFuture.isDone());
    String response = "HTTP/1.1 500 OMG\r\nContent-Length: 4\r\n\r\noops";
    serverContext.writeAndFlush(bb(response, serverContext.channel())).sync();
    thrown.expect(ProxyConnectException.class);
    try {
      negotiationFuture.sync();
    } finally {
      channel.close();
    }
  }

  private static ByteBuf bb(String s, Channel c) {
    return ByteBufUtil.writeUtf8(c.alloc(), s);
  }
}
