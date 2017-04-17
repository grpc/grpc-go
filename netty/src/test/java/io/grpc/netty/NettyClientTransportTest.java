/*
 * Copyright 2014, Google Inc. All rights reserved.
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

import static com.google.common.base.Charsets.UTF_8;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static io.grpc.internal.GrpcUtil.DEFAULT_SERVER_KEEPALIVE_TIMEOUT_NANOS;
import static io.grpc.internal.GrpcUtil.DEFAULT_SERVER_KEEPALIVE_TIME_NANOS;
import static io.grpc.internal.GrpcUtil.KEEPALIVE_TIME_NANOS_DISABLED;
import static io.grpc.internal.GrpcUtil.USER_AGENT_KEY;
import static io.grpc.netty.NettyServerBuilder.MAX_CONNECTION_AGE_GRACE_NANOS_INFINITE;
import static io.grpc.netty.NettyServerBuilder.MAX_CONNECTION_AGE_NANOS_DISABLED;
import static io.grpc.netty.NettyServerBuilder.MAX_CONNECTION_IDLE_NANOS_DISABLED;
import static io.netty.handler.codec.http2.Http2CodecUtil.DEFAULT_WINDOW_SIZE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.Attributes;
import io.grpc.Context;
import io.grpc.Grpc;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.ServerStreamTracer;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.StatusException;
import io.grpc.internal.ClientStream;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.FakeClock;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.ManagedClientTransport;
import io.grpc.internal.ServerListener;
import io.grpc.internal.ServerStream;
import io.grpc.internal.ServerStreamListener;
import io.grpc.internal.ServerTransport;
import io.grpc.internal.ServerTransportListener;
import io.grpc.testing.TestUtils;
import io.netty.channel.ChannelConfig;
import io.netty.channel.ChannelOption;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannelConfig;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.http2.StreamBufferingEncoder;
import io.netty.handler.ssl.SslContext;
import io.netty.handler.ssl.SupportedCipherSuiteFilter;
import java.io.ByteArrayInputStream;
import java.io.File;
import java.io.IOException;
import java.io.InputStream;
import java.net.InetSocketAddress;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Tests for {@link NettyClientTransport}.
 */
@RunWith(JUnit4.class)
public class NettyClientTransportTest {

  @Mock
  private ManagedClientTransport.Listener clientTransportListener;

  private final List<NettyClientTransport> transports = new ArrayList<NettyClientTransport>();
  private final NioEventLoopGroup group = new NioEventLoopGroup(1);
  private final EchoServerListener serverListener = new EchoServerListener();
  private Runnable tooManyPingsRunnable = new Runnable() {
    // Throwing is useless in this method, because Netty doesn't propagate the exception
    @Override public void run() {}
  };

  private InetSocketAddress address;
  private String authority;
  private NettyServer server;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);
  }

  @After
  public void teardown() throws Exception {
    Context.ROOT.attach();
    for (NettyClientTransport transport : transports) {
      transport.shutdown();
    }

    if (server != null) {
      server.shutdown();
    }

    group.shutdownGracefully(0, 10, TimeUnit.SECONDS);
  }

  @Test
  public void testToString() throws Exception {
    address = TestUtils.testServerAddress(12345);
    authority = GrpcUtil.authorityFromHostAndPort(address.getHostString(), address.getPort());
    String s = newTransport(newNegotiator()).toString();
    transports.clear();
    assertTrue("Unexpected: " + s, s.contains("NettyClientTransport"));
    assertTrue("Unexpected: " + s, s.contains(address.toString()));
  }

  @Test
  public void addDefaultUserAgent() throws Exception {
    startServer();
    NettyClientTransport transport = newTransport(newNegotiator());
    callMeMaybe(transport.start(clientTransportListener));

    // Send a single RPC and wait for the response.
    new Rpc(transport).halfClose().waitForResponse();

    // Verify that the received headers contained the User-Agent.
    assertEquals(1, serverListener.streamListeners.size());

    Metadata headers = serverListener.streamListeners.get(0).headers;
    assertEquals(GrpcUtil.getGrpcUserAgent("netty", null), headers.get(USER_AGENT_KEY));
  }

  @Test
  public void setSoLingerChannelOption() throws IOException {
    startServer();
    Map<ChannelOption<?>, Object> channelOptions = new HashMap<ChannelOption<?>, Object>();
    // set SO_LINGER option
    int soLinger = 123;
    channelOptions.put(ChannelOption.SO_LINGER, soLinger);
    NettyClientTransport transport = new NettyClientTransport(
        address, NioSocketChannel.class, channelOptions, group, newNegotiator(),
        DEFAULT_WINDOW_SIZE, DEFAULT_MAX_MESSAGE_SIZE, GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE,
        KEEPALIVE_TIME_NANOS_DISABLED, 1L, false, authority, null /* user agent */,
        tooManyPingsRunnable);
    transports.add(transport);
    callMeMaybe(transport.start(clientTransportListener));

    // verify SO_LINGER has been set
    ChannelConfig config = transport.channel().config();
    assertTrue(config instanceof SocketChannelConfig);
    assertEquals(soLinger, ((SocketChannelConfig) config).getSoLinger());
  }

  @Test
  public void overrideDefaultUserAgent() throws Exception {
    startServer();
    NettyClientTransport transport = newTransport(newNegotiator(),
        DEFAULT_MAX_MESSAGE_SIZE, GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE, "testUserAgent", true);
    callMeMaybe(transport.start(clientTransportListener));

    new Rpc(transport, new Metadata()).halfClose().waitForResponse();

    // Verify that the received headers contained the User-Agent.
    assertEquals(1, serverListener.streamListeners.size());
    Metadata receivedHeaders = serverListener.streamListeners.get(0).headers;
    assertEquals(GrpcUtil.getGrpcUserAgent("netty", "testUserAgent"),
        receivedHeaders.get(USER_AGENT_KEY));
  }

  @Test
  public void maxMessageSizeShouldBeEnforced() throws Throwable {
    startServer();
    // Allow the response payloads of up to 1 byte.
    NettyClientTransport transport = newTransport(newNegotiator(),
        1, GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE, null, true);
    callMeMaybe(transport.start(clientTransportListener));

    try {
      // Send a single RPC and wait for the response.
      new Rpc(transport).halfClose().waitForResponse();
      fail("Expected the stream to fail.");
    } catch (ExecutionException e) {
      Status status = Status.fromThrowable(e);
      assertEquals(Code.RESOURCE_EXHAUSTED, status.getCode());
      assertTrue("Missing exceeds maximum from: " + status.getDescription(),
          status.getDescription().contains("exceeds maximum"));
    }
  }

  /**
   * Verifies that we can create multiple TLS client transports from the same builder.
   */
  @Test
  public void creatingMultipleTlsTransportsShouldSucceed() throws Exception {
    startServer();

    // Create a couple client transports.
    ProtocolNegotiator negotiator = newNegotiator();
    for (int index = 0; index < 2; ++index) {
      NettyClientTransport transport = newTransport(negotiator);
      callMeMaybe(transport.start(clientTransportListener));
    }

    // Send a single RPC on each transport.
    final List<Rpc> rpcs = new ArrayList<Rpc>(transports.size());
    for (NettyClientTransport transport : transports) {
      rpcs.add(new Rpc(transport).halfClose());
    }

    // Wait for the RPCs to complete.
    for (Rpc rpc : rpcs) {
      rpc.waitForResponse();
    }
  }

  @Test
  public void bufferedStreamsShouldBeClosedWhenConnectionTerminates() throws Exception {
    // Only allow a single stream active at a time.
    startServer(1, GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE);

    NettyClientTransport transport = newTransport(newNegotiator());
    callMeMaybe(transport.start(clientTransportListener));

    // Send a dummy RPC in order to ensure that the updated SETTINGS_MAX_CONCURRENT_STREAMS
    // has been received by the remote endpoint.
    new Rpc(transport).halfClose().waitForResponse();

    // Create 3 streams, but don't half-close. The transport will buffer the second and third.
    Rpc[] rpcs = new Rpc[] { new Rpc(transport), new Rpc(transport), new Rpc(transport) };

    // Wait for the response for the stream that was actually created.
    rpcs[0].waitForResponse();

    // Now forcibly terminate the connection from the server side.
    serverListener.transports.get(0).channel().pipeline().firstContext().close();

    // Now wait for both listeners to be closed.
    for (int i = 1; i < rpcs.length; i++) {
      try {
        rpcs[i].waitForClose();
        fail("Expected the RPC to fail");
      } catch (ExecutionException e) {
        // Expected.
        Throwable t = getRootCause(e);
        // Make sure that the Http2ChannelClosedException got replaced with the real cause of
        // the shutdown.
        assertFalse(t instanceof StreamBufferingEncoder.Http2ChannelClosedException);
      }
    }
  }

  public static class CantConstructChannel extends NioSocketChannel {
    /** Constructor. It doesn't work. Feel free to try. But it doesn't work. */
    public CantConstructChannel() {
      // Use an Error because we've seen cases of channels failing to construct due to classloading
      // problems (like mixing different versions of Netty), and those involve Errors.
      throw new CantConstructChannelError();
    }
  }

  private static class CantConstructChannelError extends Error {}

  @Test
  public void failingToConstructChannelShouldFailGracefully() throws Exception {
    address = TestUtils.testServerAddress(12345);
    authority = GrpcUtil.authorityFromHostAndPort(address.getHostString(), address.getPort());
    NettyClientTransport transport = new NettyClientTransport(
        address, CantConstructChannel.class, new HashMap<ChannelOption<?>, Object>(), group,
        newNegotiator(), DEFAULT_WINDOW_SIZE, DEFAULT_MAX_MESSAGE_SIZE,
        GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE, KEEPALIVE_TIME_NANOS_DISABLED, 1, false, authority,
        null, tooManyPingsRunnable);
    transports.add(transport);

    // Should not throw
    callMeMaybe(transport.start(clientTransportListener));

    // And RPCs and PINGs should fail cleanly, reporting the failure
    Rpc rpc = new Rpc(transport);
    try {
      rpc.waitForResponse();
      fail("Expected exception");
    } catch (Exception ex) {
      if (!(getRootCause(ex) instanceof CantConstructChannelError)) {
        throw new AssertionError("Could not find expected error", ex);
      }
    }

    final SettableFuture<Object> pingResult = SettableFuture.create();
    FakeClock clock = new FakeClock();
    ClientTransport.PingCallback pingCallback = new ClientTransport.PingCallback() {
      @Override
      public void onSuccess(long roundTripTimeNanos) {
        pingResult.set(roundTripTimeNanos);
      }

      @Override
      public void onFailure(Throwable cause) {
        pingResult.setException(cause);
      }
    };
    transport.ping(pingCallback, clock.getScheduledExecutorService());
    assertFalse(pingResult.isDone());
    clock.runDueTasks();
    assertTrue(pingResult.isDone());
    try {
      pingResult.get();
      fail("Expected exception");
    } catch (Exception ex) {
      if (!(getRootCause(ex) instanceof CantConstructChannelError)) {
        throw new AssertionError("Could not find expected error", ex);
      }
    }
  }

  @Test
  public void maxHeaderListSizeShouldBeEnforcedOnClient() throws Exception {
    startServer();

    NettyClientTransport transport =
        newTransport(newNegotiator(), DEFAULT_MAX_MESSAGE_SIZE, 1, null, true);
    callMeMaybe(transport.start(clientTransportListener));

    try {
      // Send a single RPC and wait for the response.
      new Rpc(transport, new Metadata()).halfClose().waitForResponse();
      fail("The stream should have been failed due to client received header exceeds header list"
          + " size limit!");
    } catch (Exception e) {
      Throwable rootCause = getRootCause(e);
      Status status = ((StatusException) rootCause).getStatus();
      assertEquals(Status.Code.INTERNAL, status.getCode());
      assertEquals("HTTP/2 error code: PROTOCOL_ERROR\nReceived Rst Stream",
          status.getDescription());
    }
  }

  @Test
  public void maxHeaderListSizeShouldBeEnforcedOnServer() throws Exception {
    startServer(100, 1);

    NettyClientTransport transport = newTransport(newNegotiator());
    callMeMaybe(transport.start(clientTransportListener));

    try {
      // Send a single RPC and wait for the response.
      new Rpc(transport, new Metadata()).halfClose().waitForResponse();
      fail("The stream should have been failed due to server received header exceeds header list"
          + " size limit!");
    } catch (Exception e) {
      Status status = Status.fromThrowable(e);
      assertEquals(status.toString(), Status.Code.INTERNAL, status.getCode());
    }
  }

  @Test
  public void getAttributes_negotiatorHandler() throws Exception {
    address = TestUtils.testServerAddress(12345);
    authority = GrpcUtil.authorityFromHostAndPort(address.getHostString(), address.getPort());

    NettyClientTransport transport = newTransport(
        new ProtocolNegotiator() {
          @Override
          public Handler newHandler(GrpcHttp2ConnectionHandler handler) {
            return null;
          }
        });

    assertEquals(Attributes.EMPTY, transport.getAttributes());

    transports.clear();
  }

  @Test
  public void clientStreamGetsAttributes() throws Exception {
    startServer();
    NettyClientTransport transport = newTransport(newNegotiator());
    callMeMaybe(transport.start(clientTransportListener));
    Rpc rpc = new Rpc(transport).halfClose();
    rpc.waitForResponse();

    assertNotNull(rpc.stream.getAttributes().get(Grpc.TRANSPORT_ATTR_SSL_SESSION));
    assertEquals(address, rpc.stream.getAttributes().get(Grpc.TRANSPORT_ATTR_REMOTE_ADDR));
  }

  @Test
  public void keepAliveEnabled() throws Exception {
    startServer();
    NettyClientTransport transport = newTransport(newNegotiator(), DEFAULT_MAX_MESSAGE_SIZE,
        GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE, null /* user agent */, true /* keep alive */);
    callMeMaybe(transport.start(clientTransportListener));
    Rpc rpc = new Rpc(transport).halfClose();
    rpc.waitForResponse();

    assertNotNull(transport.keepAliveManager());
  }

  @Test
  public void keepAliveDisabled() throws Exception {
    startServer();
    NettyClientTransport transport = newTransport(newNegotiator(), DEFAULT_MAX_MESSAGE_SIZE,
        GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE, null /* user agent */, false /* keep alive */);
    callMeMaybe(transport.start(clientTransportListener));
    Rpc rpc = new Rpc(transport).halfClose();
    rpc.waitForResponse();

    assertNull(transport.keepAliveManager());
  }

  private Throwable getRootCause(Throwable t) {
    if (t.getCause() == null) {
      return t;
    }
    return getRootCause(t.getCause());
  }

  private ProtocolNegotiator newNegotiator() throws IOException {
    File clientCert = TestUtils.loadCert("ca.pem");
    SslContext clientContext = GrpcSslContexts.forClient().trustManager(clientCert)
        .ciphers(TestUtils.preferredTestCiphers(), SupportedCipherSuiteFilter.INSTANCE).build();
    return ProtocolNegotiators.tls(clientContext, authority);
  }

  private NettyClientTransport newTransport(ProtocolNegotiator negotiator) {
    return newTransport(negotiator, DEFAULT_MAX_MESSAGE_SIZE, GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE,
        null /* user agent */, true /* keep alive */);
  }

  private NettyClientTransport newTransport(ProtocolNegotiator negotiator, int maxMsgSize,
      int maxHeaderListSize, String userAgent, boolean enableKeepAlive) {
    long keepAliveTimeNano = KEEPALIVE_TIME_NANOS_DISABLED;
    long keepAliveTimeoutNano = TimeUnit.SECONDS.toNanos(1L);
    if (enableKeepAlive) {
      keepAliveTimeNano = TimeUnit.SECONDS.toNanos(10L);
    }
    NettyClientTransport transport = new NettyClientTransport(
        address, NioSocketChannel.class, new HashMap<ChannelOption<?>, Object>(), group, negotiator,
        DEFAULT_WINDOW_SIZE, maxMsgSize, maxHeaderListSize,
        keepAliveTimeNano, keepAliveTimeoutNano,
        false, authority, userAgent, tooManyPingsRunnable);
    transports.add(transport);
    return transport;
  }

  private void startServer() throws IOException {
    startServer(100, GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE);
  }

  private void startServer(int maxStreamsPerConnection, int maxHeaderListSize) throws IOException {
    File serverCert = TestUtils.loadCert("server1.pem");
    File key = TestUtils.loadCert("server1.key");
    SslContext serverContext = GrpcSslContexts.forServer(serverCert, key)
        .ciphers(TestUtils.preferredTestCiphers(), SupportedCipherSuiteFilter.INSTANCE).build();
    ProtocolNegotiator negotiator = ProtocolNegotiators.serverTls(serverContext);
    server = new NettyServer(
        TestUtils.testServerAddress(0),
        NioServerSocketChannel.class, group, group, negotiator,
        Collections.<ServerStreamTracer.Factory>emptyList(), maxStreamsPerConnection,
        DEFAULT_WINDOW_SIZE, DEFAULT_MAX_MESSAGE_SIZE, maxHeaderListSize,
        DEFAULT_SERVER_KEEPALIVE_TIME_NANOS, DEFAULT_SERVER_KEEPALIVE_TIMEOUT_NANOS,
        MAX_CONNECTION_IDLE_NANOS_DISABLED,
        MAX_CONNECTION_AGE_NANOS_DISABLED, MAX_CONNECTION_AGE_GRACE_NANOS_INFINITE, true, 0);
    server.start(serverListener);
    address = TestUtils.testServerAddress(server.getPort());
    authority = GrpcUtil.authorityFromHostAndPort(address.getHostString(), address.getPort());
  }

  private void callMeMaybe(Runnable r) {
    if (r != null) {
      r.run();
    }
  }

  private static class Rpc {
    static final String MESSAGE = "hello";
    static final MethodDescriptor<String, String> METHOD =
        MethodDescriptor.<String, String>newBuilder()
            .setType(MethodDescriptor.MethodType.UNARY)
            .setFullMethodName("/testService/test")
            .setRequestMarshaller(StringMarshaller.INSTANCE)
            .setResponseMarshaller(StringMarshaller.INSTANCE)
            .build();

    final ClientStream stream;
    final TestClientStreamListener listener = new TestClientStreamListener();

    Rpc(NettyClientTransport transport) {
      this(transport, new Metadata());
    }

    Rpc(NettyClientTransport transport, Metadata headers) {
      stream = transport.newStream(METHOD, headers);
      stream.start(listener);
      stream.request(1);
      stream.writeMessage(new ByteArrayInputStream(MESSAGE.getBytes(UTF_8)));
      stream.flush();
    }

    Rpc halfClose() {
      stream.halfClose();
      return this;
    }

    void waitForResponse() throws InterruptedException, ExecutionException, TimeoutException {
      listener.responseFuture.get(10, TimeUnit.SECONDS);
    }

    void waitForClose() throws InterruptedException, ExecutionException, TimeoutException {
      listener.closedFuture.get(10, TimeUnit.SECONDS);
    }
  }

  private static final class TestClientStreamListener implements ClientStreamListener {
    final SettableFuture<Void> closedFuture = SettableFuture.create();
    final SettableFuture<Void> responseFuture = SettableFuture.create();

    @Override
    public void headersRead(Metadata headers) {
    }

    @Override
    public void closed(Status status, Metadata trailers) {
      if (status.isOk()) {
        closedFuture.set(null);
      } else {
        StatusException e = status.asException();
        closedFuture.setException(e);
        responseFuture.setException(e);
      }
    }

    @Override
    public void messageRead(InputStream message) {
      responseFuture.set(null);
    }

    @Override
    public void onReady() {
    }
  }

  private static final class EchoServerStreamListener implements ServerStreamListener {
    final ServerStream stream;
    final String method;
    final Metadata headers;

    EchoServerStreamListener(ServerStream stream, String method, Metadata headers) {
      this.stream = stream;
      this.method = method;
      this.headers = headers;
      stream.writeHeaders(new Metadata());
      stream.request(1);
    }

    @Override
    public void messageRead(InputStream message) {
      // Just echo back the message.
      stream.writeMessage(message);
      stream.flush();
    }

    @Override
    public void onReady() {
    }

    @Override
    public void halfClosed() {
      // Just close when the client closes.
      stream.close(Status.OK, new Metadata());
    }

    @Override
    public void closed(Status status) {
    }
  }

  private static final class EchoServerListener implements ServerListener {
    final List<NettyServerTransport> transports = new ArrayList<NettyServerTransport>();
    final List<EchoServerStreamListener> streamListeners =
            Collections.synchronizedList(new ArrayList<EchoServerStreamListener>());

    @Override
    public ServerTransportListener transportCreated(final ServerTransport transport) {
      transports.add((NettyServerTransport) transport);
      return new ServerTransportListener() {
        @Override
        public void streamCreated(ServerStream stream, String method, Metadata headers) {
          EchoServerStreamListener listener = new EchoServerStreamListener(stream, method, headers);
          stream.setListener(listener);
          streamListeners.add(listener);
        }

        @Override
        public Attributes transportReady(Attributes transportAttrs) {
          return transportAttrs;
        }

        @Override
        public void transportTerminated() {
        }
      };
    }

    @Override
    public void serverShutdown() {
    }
  }

  private static final class StringMarshaller implements Marshaller<String> {
    static final StringMarshaller INSTANCE = new StringMarshaller();

    @Override
    public InputStream stream(String value) {
      return new ByteArrayInputStream(value.getBytes(UTF_8));
    }

    @Override
    public String parse(InputStream stream) {
      try {
        return new String(ByteStreams.toByteArray(stream), UTF_8);
      } catch (IOException ex) {
        throw new RuntimeException(ex);
      }
    }
  }
}
