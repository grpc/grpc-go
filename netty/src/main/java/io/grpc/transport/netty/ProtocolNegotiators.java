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

import io.grpc.Status;

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
import io.netty.handler.ssl.OpenSslContext;
import io.netty.handler.ssl.OpenSslEngine;
import io.netty.handler.ssl.SslContext;
import io.netty.handler.ssl.SslHandler;
import io.netty.handler.ssl.SslHandshakeCompletionEvent;
import io.netty.util.ByteString;
import io.netty.util.concurrent.Future;
import io.netty.util.concurrent.GenericFutureListener;

import java.net.InetSocketAddress;
import java.util.ArrayDeque;
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

  private ProtocolNegotiators() {
  }

  /**
   * Create a TLS handler for HTTP/2 capable of using ALPN/NPN.
   */
  public static ChannelHandler serverTls(SSLEngine sslEngine) {
    Preconditions.checkNotNull(sslEngine, "sslEngine");

    // If we're using Jetty ALPN, verify that it is configured properly.
    if (!(sslEngine instanceof OpenSslEngine)) {
      try {
        JettyAlpnVerifier.verifyJettyAlpn();
      } catch (JettyAlpnVerifier.NotFoundException e) {
        throw new IllegalArgumentException(e);
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

    // If we're using Jetty ALPN, verify that it is configured properly.
    if (!(sslContext instanceof OpenSslContext)) {
      try {
        JettyAlpnVerifier.verifyJettyAlpn();
      } catch (JettyAlpnVerifier.NotFoundException e) {
        throw new IllegalArgumentException(e);
      }
    }

    return new ProtocolNegotiator() {
      @Override
      public Handler newHandler(Http2ConnectionHandler handler) {
        ChannelHandler sslBootstrap = new ChannelHandlerAdapter() {
          @Override
          public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
            // TODO(nmittler): Unsupported for OpenSSL in Netty < 4.1.Beta6.
            SSLEngine sslEngine = sslContext.newEngine(ctx.alloc(),
                    inetAddress.getHostName(), inetAddress.getPort());
            SSLParameters sslParams = new SSLParameters();
            sslParams.setEndpointIdentificationAlgorithm("HTTPS");
            sslEngine.setSSLParameters(sslParams);

            SslHandler sslHandler = new SslHandler(sslEngine, false);
            sslHandler.handshakeFuture().addListener(new GenericFutureListener<Future<Channel>>() {
              @Override
              public void operationComplete(Future<Channel> future) throws Exception {
                // If an error occurred during the handshake, throw it to the pipeline.
                future.get();
              }
            });
            ctx.pipeline().replace(this, null, sslHandler);
          }
        };
        return new BufferUntilTlsNegotiatedHandler(sslBootstrap, handler);
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

  private static RuntimeException unavailableException(String msg) {
    return Status.UNAVAILABLE.withDescription(msg).asRuntimeException();
  }

  /**
   * Buffers all writes until either {@link #writeBufferedAndRemove(ChannelHandlerContext)} or
   * {@link #fail(ChannelHandlerContext, Throwable)} is called. This handler allows us to
   * write to a {@link Channel} before we are allowed to write to it officially i.e.
   * before it's active or the TLS Handshake is complete.
   */
  public abstract static class AbstractBufferingHandler extends ChannelDuplexHandler {

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
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
      fail(ctx, cause);
    }

    @Override
    public void channelInactive(ChannelHandlerContext ctx) throws Exception {
      fail(ctx, unavailableException("Connection broken while performing protocol negotiation"));
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
      fail(ctx, unavailableException("Channel closed while performing protocol negotiation"));
    }

    protected final void fail(ChannelHandlerContext ctx, Throwable cause) {
      if (bufferedWrites != null) {
        while (!bufferedWrites.isEmpty()) {
          ChannelWrite write = bufferedWrites.poll();
          write.promise.setFailure(cause);
        }
        bufferedWrites = null;
      }

      log.log(Level.SEVERE, "Transport failed during protocol negotiation", cause);

      /**
       * In case something goes wrong ensure that the channel gets closed as the
       * NettyClientTransport relies on the channel's close future to get completed.
       */
      ctx.close();
    }

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
          fail(ctx, handshakeEvent.cause());
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
        fail(ctx, unavailableException("HTTP/2 upgrade rejected"));
      }
      super.userEventTriggered(ctx, evt);
    }
  }
}
