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

import static com.google.common.base.Preconditions.checkNotNull;
import static io.grpc.netty.GrpcSslContexts.NEXT_PROTOCOL_VERSIONS;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.Attributes;
import io.grpc.Grpc;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.Security;
import io.grpc.InternalChannelz.Tls;
import io.grpc.SecurityLevel;
import io.grpc.Status;
import io.grpc.internal.GrpcAttributes;
import io.grpc.internal.GrpcUtil;
import io.netty.channel.ChannelDuplexHandler;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerAdapter;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandler;
import io.netty.channel.ChannelInboundHandlerAdapter;
import io.netty.channel.ChannelPipeline;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http.DefaultHttpRequest;
import io.netty.handler.codec.http.HttpClientCodec;
import io.netty.handler.codec.http.HttpClientUpgradeHandler;
import io.netty.handler.codec.http.HttpHeaderNames;
import io.netty.handler.codec.http.HttpMethod;
import io.netty.handler.codec.http.HttpVersion;
import io.netty.handler.codec.http2.Http2ClientUpgradeCodec;
import io.netty.handler.proxy.HttpProxyHandler;
import io.netty.handler.proxy.ProxyConnectionEvent;
import io.netty.handler.proxy.ProxyHandler;
import io.netty.handler.ssl.OpenSsl;
import io.netty.handler.ssl.OpenSslEngine;
import io.netty.handler.ssl.SslContext;
import io.netty.handler.ssl.SslHandler;
import io.netty.handler.ssl.SslHandshakeCompletionEvent;
import io.netty.util.AsciiString;
import io.netty.util.ReferenceCountUtil;
import java.net.SocketAddress;
import java.net.URI;
import java.util.ArrayDeque;
import java.util.Arrays;
import java.util.Queue;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLParameters;
import javax.net.ssl.SSLSession;

/**
 * Common {@link ProtocolNegotiator}s used by gRPC.
 */
final class ProtocolNegotiators {
  private static final Logger log = Logger.getLogger(ProtocolNegotiators.class.getName());

  private ProtocolNegotiators() {
  }

  /**
   * Create a server plaintext handler for gRPC.
   */
  public static ProtocolNegotiator serverPlaintext() {
    return new ProtocolNegotiator() {
      @Override
      public ChannelHandler newHandler(final GrpcHttp2ConnectionHandler handler) {
        class PlaintextHandler extends ChannelHandlerAdapter {
          @Override
          public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
            // Set sttributes before replace to be sure we pass it before accepting any requests.
            handler.handleProtocolNegotiationCompleted(Attributes.newBuilder()
                .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, ctx.channel().remoteAddress())
                .set(Grpc.TRANSPORT_ATTR_LOCAL_ADDR, ctx.channel().localAddress())
                .build(),
                /*securityInfo=*/ null);
            // Just replace this handler with the gRPC handler.
            ctx.pipeline().replace(this, null, handler);
          }
        }

        return new PlaintextHandler();
      }

      @Override
      public void close() {}

      @Override
      public AsciiString scheme() {
        return Utils.HTTP;
      }
    };
  }

  /**
   * Create a server TLS handler for HTTP/2 capable of using ALPN/NPN.
   */
  public static ProtocolNegotiator serverTls(final SslContext sslContext) {
    Preconditions.checkNotNull(sslContext, "sslContext");
    return new ProtocolNegotiator() {
      @Override
      public ChannelHandler newHandler(GrpcHttp2ConnectionHandler handler) {
        return new ServerTlsHandler(sslContext, handler);
      }

      @Override
      public void close() {}


      @Override
      public AsciiString scheme() {
        return Utils.HTTPS;
      }
    };
  }

  @VisibleForTesting
  static final class ServerTlsHandler extends ChannelInboundHandlerAdapter {
    private final GrpcHttp2ConnectionHandler grpcHandler;
    private final SslContext sslContext;

    ServerTlsHandler(SslContext sslContext, GrpcHttp2ConnectionHandler grpcHandler) {
      this.sslContext = sslContext;
      this.grpcHandler = grpcHandler;
    }

    @Override
    public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
      super.handlerAdded(ctx);

      SSLEngine sslEngine = sslContext.newEngine(ctx.alloc());
      ctx.pipeline().addFirst(new SslHandler(sslEngine, false));
    }

    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
      fail(ctx, cause);
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (evt instanceof SslHandshakeCompletionEvent) {
        SslHandshakeCompletionEvent handshakeEvent = (SslHandshakeCompletionEvent) evt;
        if (handshakeEvent.isSuccess()) {
          if (NEXT_PROTOCOL_VERSIONS.contains(sslHandler(ctx.pipeline()).applicationProtocol())) {
            SSLSession session = sslHandler(ctx.pipeline()).engine().getSession();
            // Successfully negotiated the protocol.
            // Notify about completion and pass down SSLSession in attributes.
            grpcHandler.handleProtocolNegotiationCompleted(
                Attributes.newBuilder()
                    .set(Grpc.TRANSPORT_ATTR_SSL_SESSION, session)
                    .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, ctx.channel().remoteAddress())
                    .set(Grpc.TRANSPORT_ATTR_LOCAL_ADDR, ctx.channel().localAddress())
                    .build(),
                new InternalChannelz.Security(new InternalChannelz.Tls(session)));
            // Replace this handler with the GRPC handler.
            ctx.pipeline().replace(this, null, grpcHandler);
          } else {
            fail(ctx,
                unavailableException(
                  "Failed protocol negotiation: Unable to find compatible protocol"));
          }
        } else {
          fail(ctx, handshakeEvent.cause());
        }
      }
      super.userEventTriggered(ctx, evt);
    }

    private SslHandler sslHandler(ChannelPipeline pipeline) {
      return pipeline.get(SslHandler.class);
    }

    @SuppressWarnings("FutureReturnValueIgnored")
    private void fail(ChannelHandlerContext ctx, Throwable exception) {
      logSslEngineDetails(Level.FINE, ctx, "TLS negotiation failed for new client.", exception);
      ctx.close();
    }
  }

  /**
   * Returns a {@link ProtocolNegotiator} that does HTTP CONNECT proxy negotiation.
   */
  public static ProtocolNegotiator httpProxy(final SocketAddress proxyAddress,
      final @Nullable String proxyUsername, final @Nullable String proxyPassword,
      final ProtocolNegotiator negotiator) {
    final AsciiString scheme = negotiator.scheme();
    Preconditions.checkNotNull(proxyAddress, "proxyAddress");
    Preconditions.checkNotNull(negotiator, "negotiator");
    class ProxyNegotiator implements ProtocolNegotiator {
      @Override
      public ChannelHandler newHandler(GrpcHttp2ConnectionHandler http2Handler) {
        HttpProxyHandler proxyHandler;
        if (proxyUsername == null || proxyPassword == null) {
          proxyHandler = new HttpProxyHandler(proxyAddress);
        } else {
          proxyHandler = new HttpProxyHandler(proxyAddress, proxyUsername, proxyPassword);
        }
        return new BufferUntilProxyTunnelledHandler(
            proxyHandler, negotiator.newHandler(http2Handler));
      }

      @Override
      public AsciiString scheme() {
        return scheme;
      }

      // This method is not normally called, because we use httpProxy on a per-connection basis in
      // NettyChannelBuilder. Instead, we expect `negotiator' to be closed by NettyTransportFactory.
      @Override
      public void close() {
        negotiator.close();
      }
    }

    return new ProxyNegotiator();
  }

  /**
   * Buffers all writes until the HTTP CONNECT tunnel is established.
   */
  static final class BufferUntilProxyTunnelledHandler extends AbstractBufferingHandler {

    public BufferUntilProxyTunnelledHandler(ProxyHandler proxyHandler, ChannelHandler handler) {
      super(proxyHandler, handler);
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (evt instanceof ProxyConnectionEvent) {
        writeBufferedAndRemove(ctx);
      }
      super.userEventTriggered(ctx, evt);
    }

    @Override
    public void channelInactive(ChannelHandlerContext ctx) throws Exception {
      fail(ctx, unavailableException("Connection broken while trying to CONNECT through proxy"));
      super.channelInactive(ctx);
    }

    @Override
    public void close(ChannelHandlerContext ctx, ChannelPromise future) throws Exception {
      if (ctx.channel().isActive()) { // This may be a notification that the socket was closed
        fail(ctx, unavailableException("Channel closed while trying to CONNECT through proxy"));
      }
      super.close(ctx, future);
    }
  }

  static final class ClientTlsProtocolNegotiator implements ProtocolNegotiator {

    public ClientTlsProtocolNegotiator(SslContext sslContext) {
      this.sslContext = checkNotNull(sslContext, "sslContext");
    }

    private final SslContext sslContext;

    @Override
    public AsciiString scheme() {
      return Utils.HTTPS;
    }

    @Override
    public ChannelHandler newHandler(GrpcHttp2ConnectionHandler grpcHandler) {
      ChannelHandler gnh = new GrpcNegotiationHandler(grpcHandler);
      ChannelHandler cth = new ClientTlsHandler(gnh, sslContext, grpcHandler.getAuthority());
      WaitUntilActiveHandler wuah = new WaitUntilActiveHandler(cth);
      return wuah;
    }

    @Override
    public void close() {}
  }

  static final class ClientTlsHandler extends ChannelDuplexHandler {

    private final ChannelHandler next;
    private final SslContext sslContext;
    private final String host;
    private final int port;

    private ProtocolNegotiationEvent pne = ProtocolNegotiationEvent.DEFAULT;

    ClientTlsHandler(ChannelHandler next, SslContext sslContext, String authority) {
      this.next = checkNotNull(next, "next");
      this.sslContext = checkNotNull(sslContext, "sslContext");
      HostPort hostPort = parseAuthority(authority);
      this.host = hostPort.host;
      this.port = hostPort.port;
    }

    @Override
    public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
      SSLEngine sslEngine = sslContext.newEngine(ctx.alloc(), host, port);
      SSLParameters sslParams = sslEngine.getSSLParameters();
      sslParams.setEndpointIdentificationAlgorithm("HTTPS");
      sslEngine.setSSLParameters(sslParams);
      ctx.pipeline().addBefore(ctx.name(), /* name= */ null, new SslHandler(sslEngine, false));
      super.handlerAdded(ctx);
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (evt instanceof ProtocolNegotiationEvent) {
        pne = (ProtocolNegotiationEvent) evt;
      } else if (evt instanceof SslHandshakeCompletionEvent) {
        SslHandshakeCompletionEvent handshakeEvent = (SslHandshakeCompletionEvent) evt;
        if (handshakeEvent.isSuccess()) {
          SslHandler handler = ctx.pipeline().get(SslHandler.class);
          if (NEXT_PROTOCOL_VERSIONS.contains(handler.applicationProtocol())) {
            // Successfully negotiated the protocol.
            logSslEngineDetails(Level.FINER, ctx, "TLS negotiation succeeded.", null);
            ctx.pipeline().replace(ctx.name(), null, next);
            fireProtocolNegotiationEvent(ctx, handler.engine().getSession());
          } else {
            Exception ex =
                unavailableException("Failed ALPN negotiation: Unable to find compatible protocol");
            logSslEngineDetails(Level.FINE, ctx, "TLS negotiation failed.", ex);
            ctx.fireExceptionCaught(ex);
          }
        } else {
          ctx.fireExceptionCaught(handshakeEvent.cause());
        }
      } else {
        super.userEventTriggered(ctx, evt);
      }
    }

    private void fireProtocolNegotiationEvent(ChannelHandlerContext ctx, SSLSession session) {
      Security security = new Security(new Tls(session));
      Attributes attrs = pne.getAttributes().toBuilder()
          .set(GrpcAttributes.ATTR_SECURITY_LEVEL, SecurityLevel.PRIVACY_AND_INTEGRITY)
          .set(Grpc.TRANSPORT_ATTR_SSL_SESSION, session)
          .build();
      ctx.fireUserEventTriggered(pne.withAttributes(attrs).withSecurity(security));
    }
  }

  @VisibleForTesting
  static HostPort parseAuthority(String authority) {
    URI uri = GrpcUtil.authorityToUri(Preconditions.checkNotNull(authority, "authority"));
    String host;
    int port;
    if (uri.getHost() != null) {
      host = uri.getHost();
      port = uri.getPort();
    } else {
      /*
       * Implementation note: We pick -1 as the port here rather than deriving it from the
       * original socket address.  The SSL engine doesn't use this port number when contacting the
       * remote server, but rather it is used for other things like SSL Session caching.  When an
       * invalid authority is provided (like "bad_cert"), picking the original port and passing it
       * in would mean that the port might used under the assumption that it was correct.   By
       * using -1 here, it forces the SSL implementation to treat it as invalid.
       */
      host = authority;
      port = -1;
    }
    return new HostPort(host, port);
  }

  /**
   * Returns a {@link ProtocolNegotiator} that ensures the pipeline is set up so that TLS will
   * be negotiated, the {@code handler} is added and writes to the {@link io.netty.channel.Channel}
   * may happen immediately, even before the TLS Handshake is complete.
   */
  public static ProtocolNegotiator tls(SslContext sslContext) {
    return new ClientTlsProtocolNegotiator(sslContext);
  }

  /** A tuple of (host, port). */
  @VisibleForTesting
  static final class HostPort {
    final String host;
    final int port;

    public HostPort(String host, int port) {
      this.host = host;
      this.port = port;
    }
  }

  /**
   * Returns a {@link ProtocolNegotiator} used for upgrading to HTTP/2 from HTTP/1.x.
   */
  public static ProtocolNegotiator plaintextUpgrade() {
    return new PlaintextUpgradeProtocolNegotiator();
  }

  static final class PlaintextUpgradeProtocolNegotiator implements ProtocolNegotiator {

    @Override
    public AsciiString scheme() {
      return Utils.HTTP;
    }

    @Override
    public ChannelHandler newHandler(GrpcHttp2ConnectionHandler grpcHandler) {
      ChannelHandler upgradeHandler =
          new Http2UpgradeAndGrpcHandler(grpcHandler.getAuthority(), grpcHandler);
      ChannelHandler wuah = new WaitUntilActiveHandler(upgradeHandler);
      return wuah;
    }

    @Override
    public void close() {}
  }

  /**
   * Acts as a combination of Http2Upgrade and {@link GrpcNegotiationHandler}.  Unfortunately,
   * this negotiator doesn't follow the pattern of "just one handler doing negotiation at a time."
   * This is due to the tight coupling between the upgrade handler and the HTTP/2 handler.
   */
  static final class Http2UpgradeAndGrpcHandler extends ChannelInboundHandlerAdapter {

    private final String authority;
    private final GrpcHttp2ConnectionHandler next;

    private ProtocolNegotiationEvent pne = ProtocolNegotiationEvent.DEFAULT;

    Http2UpgradeAndGrpcHandler(String authority, GrpcHttp2ConnectionHandler next) {
      this.authority = checkNotNull(authority, "authority");
      this.next = checkNotNull(next, "next");
    }

    @Override
    public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
      HttpClientCodec httpClientCodec = new HttpClientCodec();
      ctx.pipeline().addBefore(ctx.name(), null, httpClientCodec);

      Http2ClientUpgradeCodec upgradeCodec = new Http2ClientUpgradeCodec(next);
      HttpClientUpgradeHandler upgrader =
          new HttpClientUpgradeHandler(httpClientCodec, upgradeCodec, /*maxContentLength=*/ 1000);
      ctx.pipeline().addBefore(ctx.name(), null, upgrader);

      // Trigger the HTTP/1.1 plaintext upgrade protocol by issuing an HTTP request
      // which causes the upgrade headers to be added
      DefaultHttpRequest upgradeTrigger =
          new DefaultHttpRequest(HttpVersion.HTTP_1_1, HttpMethod.GET, "/");
      upgradeTrigger.headers().add(HttpHeaderNames.HOST, authority);
      ctx.writeAndFlush(upgradeTrigger).addListener(
          ChannelFutureListener.FIRE_EXCEPTION_ON_FAILURE);
      super.handlerAdded(ctx);
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (evt instanceof ProtocolNegotiationEvent) {
        pne = (ProtocolNegotiationEvent) evt;
      } else if (evt == HttpClientUpgradeHandler.UpgradeEvent.UPGRADE_SUCCESSFUL) {
        ctx.pipeline().remove(ctx.name());
        next.handleProtocolNegotiationCompleted(pne.getAttributes(), pne.getSecurity());
      } else if (evt == HttpClientUpgradeHandler.UpgradeEvent.UPGRADE_REJECTED) {
        ctx.fireExceptionCaught(unavailableException("HTTP/2 upgrade rejected"));
      } else {
        super.userEventTriggered(ctx, evt);
      }
    }
  }

  /**
   * Returns a {@link ChannelHandler} that ensures that the {@code handler} is added to the
   * pipeline writes to the {@link io.netty.channel.Channel} may happen immediately, even before it
   * is active.
   */
  public static ProtocolNegotiator plaintext() {
    return new PlaintextProtocolNegotiator();
  }

  private static RuntimeException unavailableException(String msg) {
    return Status.UNAVAILABLE.withDescription(msg).asRuntimeException();
  }

  @VisibleForTesting
  static void logSslEngineDetails(Level level, ChannelHandlerContext ctx, String msg,
      @Nullable Throwable t) {
    if (!log.isLoggable(level)) {
      return;
    }

    SslHandler sslHandler = ctx.pipeline().get(SslHandler.class);
    SSLEngine engine = sslHandler.engine();

    StringBuilder builder = new StringBuilder(msg);
    builder.append("\nSSLEngine Details: [\n");
    if (engine instanceof OpenSslEngine) {
      builder.append("    OpenSSL, ");
      builder.append("Version: 0x").append(Integer.toHexString(OpenSsl.version()));
      builder.append(" (").append(OpenSsl.versionString()).append("), ");
      builder.append("ALPN supported: ").append(OpenSsl.isAlpnSupported());
    } else if (JettyTlsUtil.isJettyAlpnConfigured()) {
      builder.append("    Jetty ALPN");
    } else if (JettyTlsUtil.isJettyNpnConfigured()) {
      builder.append("    Jetty NPN");
    } else if (JettyTlsUtil.isJava9AlpnAvailable()) {
      builder.append("    JDK9 ALPN");
    }
    builder.append("\n    TLS Protocol: ");
    builder.append(engine.getSession().getProtocol());
    builder.append("\n    Application Protocol: ");
    builder.append(sslHandler.applicationProtocol());
    builder.append("\n    Need Client Auth: " );
    builder.append(engine.getNeedClientAuth());
    builder.append("\n    Want Client Auth: ");
    builder.append(engine.getWantClientAuth());
    builder.append("\n    Supported protocols=");
    builder.append(Arrays.toString(engine.getSupportedProtocols()));
    builder.append("\n    Enabled protocols=");
    builder.append(Arrays.toString(engine.getEnabledProtocols()));
    builder.append("\n    Supported ciphers=");
    builder.append(Arrays.toString(engine.getSupportedCipherSuites()));
    builder.append("\n    Enabled ciphers=");
    builder.append(Arrays.toString(engine.getEnabledCipherSuites()));
    builder.append("\n]");

    log.log(level, builder.toString(), t);
  }

  /**
   * Buffers all writes until either {@link #writeBufferedAndRemove(ChannelHandlerContext)} or
   * {@link #fail(ChannelHandlerContext, Throwable)} is called. This handler allows us to
   * write to a {@link io.netty.channel.Channel} before we are allowed to write to it officially
   * i.e.  before it's active or the TLS Handshake is complete.
   */
  public abstract static class AbstractBufferingHandler extends ChannelDuplexHandler {

    private ChannelHandler[] handlers;
    private Queue<ChannelWrite> bufferedWrites = new ArrayDeque<>();
    private boolean writing;
    private boolean flushRequested;
    private Throwable failCause;

    /**
     * @param handlers the ChannelHandlers are added to the pipeline on channelRegistered and
     *                 before this handler.
     */
    protected AbstractBufferingHandler(ChannelHandler... handlers) {
      this.handlers = handlers;
    }

    /**
     * When this channel is registered, we will add all the ChannelHandlers passed into our
     * constructor to the pipeline.
     */
    @Override
    public void channelRegistered(ChannelHandlerContext ctx) throws Exception {
      /**
       * This check is necessary as a channel may be registered with different event loops during it
       * lifetime and we only want to configure it once.
       */
      if (handlers != null && handlers.length > 0) {
        for (ChannelHandler handler : handlers) {
          ctx.pipeline().addBefore(ctx.name(), null, handler);
        }
        ChannelHandler handler0 = handlers[0];
        ChannelHandlerContext handler0Ctx = ctx.pipeline().context(handlers[0]);
        handlers = null;
        if (handler0Ctx != null) { // The handler may have removed itself immediately
          if (handler0 instanceof ChannelInboundHandler) {
            ((ChannelInboundHandler) handler0).channelRegistered(handler0Ctx);
          } else {
            handler0Ctx.fireChannelRegistered();
          }
        }
      } else {
        super.channelRegistered(ctx);
      }
    }

    /**
     * Do not rely on channel handlers to propagate exceptions to us.
     * {@link NettyClientHandler} is an example of a class that does not propagate exceptions.
     * Add a listener to the connect future directly and do appropriate error handling.
     */
    @Override
    public void connect(final ChannelHandlerContext ctx, SocketAddress remoteAddress,
        SocketAddress localAddress, ChannelPromise promise) throws Exception {
      super.connect(ctx, remoteAddress, localAddress, promise);
      promise.addListener(new ChannelFutureListener() {
        @Override
        public void operationComplete(ChannelFuture future) throws Exception {
          if (!future.isSuccess()) {
            fail(ctx, future.cause());
          }
        }
      });
    }

    /**
     * If we encounter an exception, then notify all buffered writes that we failed.
     */
    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
      fail(ctx, cause);
    }

    /**
     * If this channel becomes inactive, then notify all buffered writes that we failed.
     */
    @Override
    public void channelInactive(ChannelHandlerContext ctx) throws Exception {
      fail(ctx, unavailableException("Connection broken while performing protocol negotiation"));
      super.channelInactive(ctx);
    }

    /**
     * Buffers the write until either {@link #writeBufferedAndRemove(ChannelHandlerContext)} is
     * called, or we have somehow failed. If we have already failed in the past, then the write
     * will fail immediately.
     */
    @Override
    @SuppressWarnings("FutureReturnValueIgnored")
    public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise)
        throws Exception {
      /**
       * This check handles a race condition between Channel.write (in the calling thread) and the
       * removal of this handler (in the event loop thread).
       * The problem occurs in e.g. this sequence:
       * 1) [caller thread] The write method identifies the context for this handler
       * 2) [event loop] This handler removes itself from the pipeline
       * 3) [caller thread] The write method delegates to the invoker to call the write method in
       *    the event loop thread. When this happens, we identify that this handler has been
       *    removed with "bufferedWrites == null".
       */
      if (failCause != null) {
        promise.setFailure(failCause);
        ReferenceCountUtil.release(msg);
      } else if (bufferedWrites == null) {
        super.write(ctx, msg, promise);
      } else {
        bufferedWrites.add(new ChannelWrite(msg, promise));
      }
    }

    /**
     * Calls to this method will not trigger an immediate flush. The flush will be deferred until
     * {@link #writeBufferedAndRemove(ChannelHandlerContext)}.
     */
    @Override
    public void flush(ChannelHandlerContext ctx) {
      /**
       * Swallowing any flushes is not only an optimization but also required
       * for the SslHandler to work correctly. If the SslHandler receives multiple
       * flushes while the handshake is still ongoing, then the handshake "randomly"
       * times out. Not sure at this point why this is happening. Doing a single flush
       * seems to work but multiple flushes don't ...
       */
      if (bufferedWrites == null) {
        ctx.flush();
      } else {
        flushRequested = true;
      }
    }

    /**
     * If we are still performing protocol negotiation, then this will propagate failures to all
     * buffered writes.
     */
    @Override
    public void close(ChannelHandlerContext ctx, ChannelPromise future) throws Exception {
      if (ctx.channel().isActive()) { // This may be a notification that the socket was closed
        fail(ctx, unavailableException("Channel closed while performing protocol negotiation"));
      }
      super.close(ctx, future);
    }

    /**
     * Propagate failures to all buffered writes.
     */
    @SuppressWarnings("FutureReturnValueIgnored")
    protected final void fail(ChannelHandlerContext ctx, Throwable cause) {
      if (failCause == null) {
        failCause = cause;
      }
      if (bufferedWrites != null) {
        while (!bufferedWrites.isEmpty()) {
          ChannelWrite write = bufferedWrites.poll();
          write.promise.setFailure(cause);
          ReferenceCountUtil.release(write.msg);
        }
        bufferedWrites = null;
      }

      ctx.fireExceptionCaught(cause);
    }

    @SuppressWarnings("FutureReturnValueIgnored")
    protected final void writeBufferedAndRemove(ChannelHandlerContext ctx) {
      if (!ctx.channel().isActive() || writing) {
        return;
      }
      // Make sure that method can't be reentered, so that the ordering
      // in the queue can't be messed up.
      writing = true;
      while (!bufferedWrites.isEmpty()) {
        ChannelWrite write = bufferedWrites.poll();
        ctx.write(write.msg, write.promise);
      }
      assert bufferedWrites.isEmpty();
      bufferedWrites = null;
      if (flushRequested) {
        ctx.flush();
      }
      // Removal has to happen last as the above writes will likely trigger
      // new writes that have to be added to the end of queue in order to not
      // mess up the ordering.
      ctx.pipeline().remove(this);
    }

    private static class ChannelWrite {
      Object msg;
      ChannelPromise promise;

      ChannelWrite(Object msg, ChannelPromise promise) {
        this.msg = msg;
        this.promise = promise;
      }
    }
  }

  /**
   * Adapts a {@link ProtocolNegotiationEvent} to the {@link GrpcHttp2ConnectionHandler}.
   */
  static final class GrpcNegotiationHandler extends ChannelInboundHandlerAdapter {
    private final GrpcHttp2ConnectionHandler next;

    public GrpcNegotiationHandler(GrpcHttp2ConnectionHandler next) {
      this.next = checkNotNull(next, "next");
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (evt instanceof ProtocolNegotiationEvent) {
        ProtocolNegotiationEvent protocolNegotiationEvent = (ProtocolNegotiationEvent) evt;
        ctx.pipeline().replace(ctx.name(), null, next);
        next.handleProtocolNegotiationCompleted(
            protocolNegotiationEvent.getAttributes(), protocolNegotiationEvent.getSecurity());
      } else {
        super.userEventTriggered(ctx, evt);
      }
    }
  }

  /*
   * Common {@link ProtocolNegotiator}s used by gRPC.  Protocol negotiation follows a pattern to
   * simplify the pipeline.   The pipeline should look like:
   *
   * 1.  {@link ProtocolNegotiator#newHandler() PN.H}, created.
   * 2.  [Tail], {@link WriteBufferingAndExceptionHandler WBAEH}, [Head]
   * 3.  [Tail], WBAEH, PN.H, [Head]
   *
   * <p>Typically, PN.H with be an instance of {@link InitHandler IH}, which is a trivial handler
   * that can return the {@code scheme()} of the negotiation.  IH, and each handler after,
   * replaces itself with a "next" handler once its part of negotiation is complete.  This keeps
   * the pipeline small, and limits the interaction between handlers.
   *
   * <p>Additionally, each handler may fire a {@link ProtocolNegotiationEvent PNE} just after
   * replacing itself.  Handlers should capture user events of type PNE, and re-trigger the events
   * once that handler's part of negotiation is complete.  This can be seen in the
   * {@link WaitUntilActiveHandler WUAH}, which waits until the channel is active.  Once active, it
   * replaces itself with the next handler, and fires a PNE containing the addresses.  Continuing
   * with IH and WUAH:
   *
   * 3.  [Tail], WBAEH, IH, [Head]
   * 4.  [Tail], WBAEH, WUAH, [Head]
   * 5.  [Tail], WBAEH, {@link GrpcNegotiationHandler}, [Head]
   * 6a. [Tail], WBAEH, {@link GrpcHttp2ConnectionHandler GHCH}, [Head]
   * 6b. [Tail], GHCH, [Head]
   */

  /**
   * A negotiator that only does plain text.
   */
  static final class PlaintextProtocolNegotiator implements ProtocolNegotiator {

    @Override
    public ChannelHandler newHandler(GrpcHttp2ConnectionHandler grpcHandler) {
      ChannelHandler grpcNegotiationHandler = new GrpcNegotiationHandler(grpcHandler);
      ChannelHandler activeHandler = new WaitUntilActiveHandler(grpcNegotiationHandler);
      return activeHandler;
    }

    @Override
    public void close() {}

    @Override
    public AsciiString scheme() {
      return Utils.HTTP;
    }
  }

  /**
   * Waits for the channel to be active, and then installs the next Handler.  Using this allows
   * subsequent handlers to assume the channel is active and ready to send.  Additionally, this a
   * {@link ProtocolNegotiationEvent}, with the connection addresses.
   */
  static final class WaitUntilActiveHandler extends ChannelInboundHandlerAdapter {
    private final ChannelHandler next;
    private ProtocolNegotiationEvent pne = ProtocolNegotiationEvent.DEFAULT;

    public WaitUntilActiveHandler(ChannelHandler next) {
      this.next = checkNotNull(next, "next");
    }

    @Override
    public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
      // This should be a noop, but just in case...
      super.handlerAdded(ctx);
      if (ctx.channel().isActive()) {
        ctx.pipeline().replace(ctx.name(), null, next);
        fireProtocolNegotiationEvent(ctx);
      }
    }

    @Override
    public void channelActive(ChannelHandlerContext ctx) throws Exception {
      ctx.pipeline().replace(ctx.name(), null, next);
      // Still propagate channelActive to the new handler.
      super.channelActive(ctx);
      fireProtocolNegotiationEvent(ctx);
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (evt instanceof ProtocolNegotiationEvent) {
        pne = (ProtocolNegotiationEvent) evt;
      } else {
        super.userEventTriggered(ctx, evt);
      }
    }

    private void fireProtocolNegotiationEvent(ChannelHandlerContext ctx) {
      Attributes attrs = pne.getAttributes().toBuilder()
          .set(Grpc.TRANSPORT_ATTR_LOCAL_ADDR, ctx.channel().localAddress())
          .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, ctx.channel().remoteAddress())
          // Later handlers are expected to overwrite this.
          .set(GrpcAttributes.ATTR_SECURITY_LEVEL, SecurityLevel.NONE)
          .build();
      ctx.fireUserEventTriggered(pne.withAttributes(attrs));
    }
  }
}
