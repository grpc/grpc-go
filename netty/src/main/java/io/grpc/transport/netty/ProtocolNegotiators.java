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

package io.grpc.transport.netty;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.SettableFuture;

import io.netty.channel.Channel;
import io.netty.channel.ChannelDuplexHandler;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerAdapter;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http.DefaultHttpRequest;
import io.netty.handler.codec.http.HttpClientCodec;
import io.netty.handler.codec.http.HttpClientUpgradeHandler;
import io.netty.handler.codec.http.HttpMethod;
import io.netty.handler.codec.http.HttpVersion;
import io.netty.handler.codec.http2.Http2ClientUpgradeCodec;
import io.netty.handler.codec.http2.Http2ConnectionHandler;
import io.netty.handler.codec.http2.Http2OrHttpChooser;
import io.netty.handler.ssl.SslContext;
import io.netty.handler.ssl.SslHandler;
import io.netty.handler.ssl.SslHandshakeCompletionEvent;
import io.netty.util.ByteString;
import io.netty.util.concurrent.Future;
import io.netty.util.concurrent.GenericFutureListener;

import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.net.InetSocketAddress;
import java.util.ArrayDeque;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Queue;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLParameters;

/**
 * Common {@link ProtocolNegotiator}s used by gRPC.
 */
public final class ProtocolNegotiators {
  private static final Logger log = Logger.getLogger(ProtocolNegotiators.class.getName());

  // TODO(madongfly): Remove "h2-xx" at a right time.
  private static final List<String> SUPPORTED_PROTOCOLS = Collections.unmodifiableList(
      Arrays.asList(
          Http2OrHttpChooser.SelectedProtocol.HTTP_2.protocolName(),
          "h2-14",
          "h2-15",
          "h2-16"));

  // Prefer ALPN to NPN so try it first.
  private static final String[] JETTY_TLS_NEGOTIATION_IMPL =
      {"org.eclipse.jetty.alpn.ALPN", "org.eclipse.jetty.npn.NextProtoNego"};

  private ProtocolNegotiators() {
  }

  /**
   * Create a TLS handler for HTTP/2 capable of using ALPN/NPN.
   */
  public static ChannelHandler serverTls(SSLEngine sslEngine) {
    Preconditions.checkNotNull(sslEngine, "sslEngine");
    if (!isOpenSsl(sslEngine.getClass())) {
      // Using JDK SSL
      if (!installJettyTlsProtocolSelection(sslEngine, SettableFuture.<Void>create(), true)) {
        throw new IllegalStateException("NPN/ALPN extensions not installed");
      }
    }
    return new SslHandler(sslEngine, false);
  }

  /**
   * Returns a {@link ProtocolNegotiator} that ensures the pipeline is set up so that TLS will
   * be negotiated, the {@code handler} is added and writes to the {@link Channel} may happen
   * immediately, even before the TLS Handshake is complete.
   */
  public static ProtocolNegotiator tls(final SslContext sslContext,
                                       final InetSocketAddress inetAddress) {
    Preconditions.checkNotNull(sslContext, "sslContext");
    Preconditions.checkNotNull(inetAddress, "inetAddress");

    final ChannelHandler sslBootstrapHandler = new ChannelHandlerAdapter() {
      @Override
      public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
        // TODO(nmittler): This method is currently unsupported for OpenSSL. Need to fix in Netty.
        SSLEngine sslEngine = sslContext.newEngine(ctx.alloc(),
            inetAddress.getHostName(), inetAddress.getPort());
        SSLParameters sslParams = new SSLParameters();
        sslParams.setEndpointIdentificationAlgorithm("HTTPS");
        sslEngine.setSSLParameters(sslParams);

        final SettableFuture<Void> completeFuture = SettableFuture.create();
        if (isOpenSsl(sslContext.getClass())) {
          completeFuture.set(null);
        } else {
          // Using JDK SSL
          if (!installJettyTlsProtocolSelection(sslEngine, completeFuture, false)) {
            throw new IllegalStateException("NPN/ALPN extensions not installed");
          }
        }

        SslHandler sslHandler = new SslHandler(sslEngine, false);
        sslHandler.handshakeFuture().addListener(
            new GenericFutureListener<Future<? super Channel>>() {
              @Override
              public void operationComplete(Future<? super Channel> future) throws Exception {
                // If an error occurred during the handshake, throw it to the pipeline.
                if (future.isSuccess()) {
                  completeFuture.get();
                } else {
                  future.get();
                }
              }
            });
        ctx.pipeline().replace(this, "sslHandler", sslHandler);
      }
    };
    return new ProtocolNegotiator() {
      @Override
      public Handler newHandler(Http2ConnectionHandler handler) {
        return new BufferUntilTlsNegotiatedHandler(sslBootstrapHandler, handler);
      }
    };
  }

  /**
   * Returns a {@link ProtocolNegotiator} used for upgrading to HTTP/2 from HTTP/1.x.
   */
  public static ProtocolNegotiator plaintextUpgrade() {
    return new ProtocolNegotiator() {
      @Override
      public Handler newHandler(Http2ConnectionHandler handler) {
        // Register the plaintext upgrader
        Http2ClientUpgradeCodec upgradeCodec = new Http2ClientUpgradeCodec(handler);
        HttpClientCodec httpClientCodec = new HttpClientCodec();
        final HttpClientUpgradeHandler upgrader =
            new HttpClientUpgradeHandler(httpClientCodec, upgradeCodec, 1000);
        return new BufferingHttp2UpgradeHandler(upgrader);
      }
    };
  }

  /**
   * Returns a {@link ChannelHandler} that ensures that the {@code handler} is added to the
   * pipeline writes to the {@link Channel} may happen immediately, even before it is active.
   */
  public static ProtocolNegotiator plaintext() {
    return new ProtocolNegotiator() {
      @Override
      public Handler newHandler(Http2ConnectionHandler handler) {
        return new BufferUntilChannelActiveHandler(handler);
      }
    };
  }

  /**
   * Returns {@code true} if the given class is for use with Netty OpenSsl.
   */
  private static boolean isOpenSsl(Class<?> clazz) {
    return clazz.getSimpleName().toLowerCase().contains("openssl");
  }

  /**
   * Buffers all writes until either {@link #writeBufferedAndRemove(ChannelHandlerContext)} or
   * {@link #failBufferedAndClose(ChannelHandlerContext)} is called. This handler allows us to
   * write to a {@link Channel} before we are allowed to write to it officially i.e.
   * before it's active or the TLS Handshake is complete.
   */
  private abstract static class AbstractBufferingHandler extends ChannelDuplexHandler {

    private ChannelHandler[] handlers;
    private Queue<ChannelWrite> bufferedWrites = new ArrayDeque<ChannelWrite>();
    private boolean writing;
    private boolean flushRequested;

    /**
     * @param handlers the ChannelHandlers are added to the pipeline on channelRegistered and
     *                 before this handler.
     */
    AbstractBufferingHandler(ChannelHandler... handlers) {
      this.handlers = handlers;
    }

    @Override
    public void channelRegistered(ChannelHandlerContext ctx) throws Exception {
      /**
       * This check is necessary as a channel may be registered with different event loops during it
       * lifetime and we only want to configure it once.
       */
      if (handlers != null) {
        ctx.pipeline().addFirst(handlers);
        handlers = null;
      }
      super.channelRegistered(ctx);
    }

    @Override
    public void channelInactive(ChannelHandlerContext ctx) throws Exception {
      failBufferedAndClose(ctx);
      super.channelInactive(ctx);
    }

    @Override
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
      if (bufferedWrites == null) {
        super.write(ctx, msg, promise);
      } else {
        bufferedWrites.add(new ChannelWrite(msg, promise));
      }
    }

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

    @Override
    public void close(ChannelHandlerContext ctx, ChannelPromise future) throws Exception {
      failBufferedAndClose(ctx);
    }

    protected void failBufferedAndClose(ChannelHandlerContext ctx) {
      if (bufferedWrites != null) {
        Exception e = new Exception("Buffered write failed.");
        while (!bufferedWrites.isEmpty()) {
          ChannelWrite write = bufferedWrites.poll();
          write.promise.setFailure(e);
        }
        bufferedWrites = null;
      }
      /**
       * In case something goes wrong ensure that the channel gets closed as the
       * NettyClientTransport relies on the channel's close future to get completed.
       */
      ctx.close();
    }

    protected void writeBufferedAndRemove(ChannelHandlerContext ctx) {
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
   * Buffers all writes until the TLS Handshake is complete.
   */
  private static class BufferUntilTlsNegotiatedHandler extends AbstractBufferingHandler
      implements ProtocolNegotiator.Handler {

    BufferUntilTlsNegotiatedHandler(ChannelHandler... handlers) {
      super(handlers);
    }

    @Override
    public ByteString scheme() {
      return Utils.HTTPS;
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (evt instanceof SslHandshakeCompletionEvent) {
        SslHandshakeCompletionEvent handshakeEvent = (SslHandshakeCompletionEvent) evt;
        if (handshakeEvent.isSuccess()) {
          writeBufferedAndRemove(ctx);
        } else {
          failBufferedAndClose(ctx);
        }
      }
      super.userEventTriggered(ctx, evt);
    }
  }

  /**
   * Buffers all writes until the {@link Channel} is active.
   */
  private static class BufferUntilChannelActiveHandler extends AbstractBufferingHandler
      implements ProtocolNegotiator.Handler {

    BufferUntilChannelActiveHandler(ChannelHandler... handlers) {
      super(handlers);
    }

    @Override
    public ByteString scheme() {
      return Utils.HTTP;
    }

    @Override
    public void handlerAdded(ChannelHandlerContext ctx) {
      writeBufferedAndRemove(ctx);
    }

    @Override
    public void channelActive(ChannelHandlerContext ctx) throws Exception {
      writeBufferedAndRemove(ctx);
      super.channelActive(ctx);
    }
  }

  /**
   * Buffers all writes until the HTTP to HTTP/2 upgrade is complete.
   */
  private static class BufferingHttp2UpgradeHandler extends AbstractBufferingHandler
      implements ProtocolNegotiator.Handler {

    BufferingHttp2UpgradeHandler(ChannelHandler... handlers) {
      super(handlers);
    }

    @Override
    public ByteString scheme() {
      return Utils.HTTP;
    }

    @Override
    public void channelActive(ChannelHandlerContext ctx) throws Exception {
      // Trigger the HTTP/1.1 plaintext upgrade protocol by issuing an HTTP request
      // which causes the upgrade headers to be added
      DefaultHttpRequest upgradeTrigger =
          new DefaultHttpRequest(HttpVersion.HTTP_1_1, HttpMethod.GET, "/");
      ctx.writeAndFlush(upgradeTrigger);
      super.channelActive(ctx);
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (evt == HttpClientUpgradeHandler.UpgradeEvent.UPGRADE_SUCCESSFUL) {
        writeBufferedAndRemove(ctx);
      } else if (evt == HttpClientUpgradeHandler.UpgradeEvent.UPGRADE_REJECTED) {
        failBufferedAndClose(ctx);
        ctx.pipeline().fireExceptionCaught(new Exception("HTTP/2 upgrade rejected"));
      }
      super.userEventTriggered(ctx, evt);
    }
  }

  /**
   * Find Jetty's TLS NPN/ALPN extensions and attempt to use them
   *
   * @return true if NPN/ALPN support is available.
   */
  private static boolean installJettyTlsProtocolSelection(final SSLEngine engine,
      final SettableFuture<Void> protocolNegotiated, boolean server) {
    for (String protocolNegoClassName : JETTY_TLS_NEGOTIATION_IMPL) {
      try {
        Class<?> negoClass;
        try {
          negoClass = Class.forName(protocolNegoClassName, true, null);
        } catch (ClassNotFoundException ignored) {
          // Not on the classpath.
          log.warning("Jetty extension " + protocolNegoClassName + " not found");
          continue;
        }
        Class<?> providerClass = Class.forName(protocolNegoClassName + "$Provider", true, null);
        Class<?> clientProviderClass
            = Class.forName(protocolNegoClassName + "$ClientProvider", true, null);
        Class<?> serverProviderClass
            = Class.forName(protocolNegoClassName + "$ServerProvider", true, null);
        Method putMethod = negoClass.getMethod("put", SSLEngine.class, providerClass);
        final Method removeMethod = negoClass.getMethod("remove", SSLEngine.class);
        putMethod.invoke(null, engine, Proxy.newProxyInstance(
            null,
            new Class[] {server ? serverProviderClass : clientProviderClass},
            new InvocationHandler() {
              @Override
              public Object invoke(Object proxy, Method method, Object[] args) throws Throwable {
                String methodName = method.getName();
                if ("supports".equals(methodName)) {
                  // NPN client
                  return true;
                }
                if ("unsupported".equals(methodName)) {
                  // all
                  removeMethod.invoke(null, engine);
                  protocolNegotiated.setException(new RuntimeException(
                      "Endpoint does not support any of " + SUPPORTED_PROTOCOLS
                          + " in ALPN/NPN negotiation"));
                  return null;
                }
                if ("protocols".equals(methodName)) {
                  // ALPN client, NPN server
                  return SUPPORTED_PROTOCOLS;
                }
                if ("selected".equals(methodName) || "protocolSelected".equals(methodName)) {
                  // ALPN client, NPN server
                  removeMethod.invoke(null, engine);
                  String protocol = (String) args[0];
                  if (!SUPPORTED_PROTOCOLS.contains(protocol)) {
                    RuntimeException e = new RuntimeException(
                        "Unsupported protocol selected via ALPN/NPN: " + protocol);
                    protocolNegotiated.setException(e);
                    if ("selected".equals(methodName)) {
                      // ALPN client
                      // Throwing exception causes TLS alert.
                      throw e;
                    } else {
                      return null;
                    }
                  }
                  protocolNegotiated.set(null);
                  return null;
                }
                if ("select".equals(methodName) || "selectProtocol".equals(methodName)) {
                  // ALPN server, NPN client
                  removeMethod.invoke(null, engine);
                  @SuppressWarnings("unchecked")
                  List<String> names = (List<String>) args[0];
                  for (String name : names) {
                    if (SUPPORTED_PROTOCOLS.contains(name)) {
                      protocolNegotiated.set(null);
                      return name;
                    }
                  }
                  RuntimeException e =
                      new RuntimeException("Protocol not available via ALPN/NPN: " + names);
                  protocolNegotiated.setException(e);
                  if ("select".equals(methodName)) {
                    // ALPN server
                    throw e; // Throwing exception causes TLS alert.
                  }
                  return null;
                }
                throw new IllegalStateException("Unknown method " + methodName);
              }
            }));
        return true;
      } catch (Exception e) {
        log.log(Level.SEVERE,
            "Unable to initialize protocol negotation for " + protocolNegoClassName, e);
      }
    }
    return false;
  }
}
