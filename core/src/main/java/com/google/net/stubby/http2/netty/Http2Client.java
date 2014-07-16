package com.google.net.stubby.http2.netty;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.collect.ImmutableList;
import com.google.common.logging.FormattingLogger;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Session;

import io.netty.handler.codec.http2.Http2OrHttpChooser;

import io.netty.bootstrap.Bootstrap;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerAdapter;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.ChannelOption;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.http.DefaultHttpRequest;
import io.netty.handler.codec.http.HttpClientCodec;
import io.netty.handler.codec.http.HttpClientUpgradeHandler;
import io.netty.handler.codec.http.HttpMethod;
import io.netty.handler.codec.http.HttpVersion;
import io.netty.handler.codec.http2.Http2ClientUpgradeCodec;
import io.netty.handler.ssl.SslHandler;
import io.netty.util.concurrent.Future;
import io.netty.util.concurrent.GenericFutureListener;
import io.netty.util.concurrent.Promise;

import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

import javax.net.ssl.SSLEngine;

/**
 * Simple client connection startup that creates a {@link Http2Session} for use
 * with protocol bindings.
 */
public class Http2Client {
  public static final String HTTP_VERSION_NAME =
      Http2OrHttpChooser.SelectedProtocol.HTTP_2.protocolName();

  private static final String[] JETTY_TLS_NEGOTIATION_IMPL = {
      "org.eclipse.jetty.alpn.ALPN", // Prefer ALPN to NPN so try it first
      "org.eclipse.jetty.npn.NextProtoNego"};

  private static final FormattingLogger log = FormattingLogger.getLoggerForCallerClass();

  private final String host;
  private final int port;
  private final RequestRegistry requestRegistry;
  private final SSLEngine sslEngine;
  private final boolean usePlaintextUpgrade;
  private Channel channel;

  public Http2Client(String host, int port, RequestRegistry requestRegistry,
                     boolean usePlaintextUpgrade) {
    this.host = Preconditions.checkNotNull(host);
    this.port = port;
    this.requestRegistry = Preconditions.checkNotNull(requestRegistry);
    this.usePlaintextUpgrade = usePlaintextUpgrade;
    this.sslEngine = null;
  }

  public Http2Client(String host, int port, RequestRegistry requestRegistry, SSLEngine sslEngine) {
    this.host = Preconditions.checkNotNull(host);
    this.port = port;
    this.requestRegistry = Preconditions.checkNotNull(requestRegistry);
    this.sslEngine = Preconditions.checkNotNull(sslEngine);
    this.sslEngine.setUseClientMode(true);
    this.usePlaintextUpgrade = false;
  }

  public Session startAndWait() {
    final Http2Codec http2Codec = new Http2Codec(requestRegistry);
    if (sslEngine != null) {
      startTLS(http2Codec);
    } else {
      if (usePlaintextUpgrade) {
        startPlaintextUpgrade(http2Codec);
      } else {
        startPlaintext(http2Codec);
      }
    }
    return new Http2Session(http2Codec.getWriter(), requestRegistry);
  }

  private void startTLS(final Http2Codec http2Codec) {
    SettableFuture<Void> tlsNegotiatedHttp2 = SettableFuture.create();
    if (!installJettyTLSProtocolSelection(sslEngine, tlsNegotiatedHttp2)) {
      throw new IllegalStateException("NPN/ALPN extensions not installed");
    }
    final CountDownLatch sslCompletion = new CountDownLatch(1);
    Channel channel = connect(new ChannelInitializer<SocketChannel>() {
      @Override
      public void initChannel(SocketChannel ch) throws Exception {
        SslHandler sslHandler = new SslHandler(sslEngine, false);
        sslHandler.handshakeFuture().addListener(
            new GenericFutureListener<Future<? super Channel>>() {
              @Override
              public void operationComplete(Future<? super Channel> future) throws Exception {
                sslCompletion.countDown();
              }
            });
        ch.pipeline().addLast(sslHandler);
        ch.pipeline().addLast(http2Codec);
      }
    });
    try {
      // Wait for SSL negotiation to complete
      if (!sslCompletion.await(20, TimeUnit.SECONDS)) {
        throw new IllegalStateException("Failed to negotiate TLS");
      }
      // Wait for NPN/ALPN negotation to complete. Will throw if failed.
      tlsNegotiatedHttp2.get(5, TimeUnit.SECONDS);
    } catch (Exception e) {
      // Attempt to close the channel before propagating the error
      channel.close();
      throw new IllegalStateException("Error waiting for TLS negotiation", e);
    }
  }

  /**
   * Start the connection and use the plaintext upgrade mechanism from HTTP/1.1 to HTTP2.
   */
  private void startPlaintextUpgrade(final Http2Codec http2Codec) {
    // Register the plaintext upgrader
    Http2ClientUpgradeCodec upgradeCodec = new Http2ClientUpgradeCodec(http2Codec);
    HttpClientCodec httpClientCodec = new HttpClientCodec();
    final HttpClientUpgradeHandler upgrader = new HttpClientUpgradeHandler(httpClientCodec,
        upgradeCodec, 1000);
    final UpgradeCompletionHandler completionHandler = new UpgradeCompletionHandler();

    Channel channel = connect(new ChannelInitializer<SocketChannel>() {
      @Override
      public void initChannel(SocketChannel ch) throws Exception {
        ch.pipeline().addLast(upgrader);
        ch.pipeline().addLast(completionHandler);
      }
    });

    try {
      // Trigger the HTTP/1.1 plaintext upgrade protocol by issuing an HTTP request
      // which causes the upgrade headers to be added
      Promise<Void> upgradePromise = completionHandler.getUpgradePromise();
      DefaultHttpRequest upgradeTrigger =
          new DefaultHttpRequest(HttpVersion.HTTP_1_1, HttpMethod.GET, "/");
      channel.writeAndFlush(upgradeTrigger);
      // Wait for the upgrade to complete
      upgradePromise.get(5, TimeUnit.SECONDS);
    } catch (Exception e) {
      // Attempt to close the channel before propagating the error
      channel.close();
      throw new IllegalStateException("Error waiting for plaintext protocol upgrade", e);
    }
  }

  /**
   * Start the connection and simply assume the protocol to already be negotiated.
   */
  private void startPlaintext(final Http2Codec http2Codec) {
    connect(new ChannelInitializer<SocketChannel>() {
      @Override
      public void initChannel(SocketChannel ch) throws Exception {
        ch.pipeline().addLast(http2Codec);
      }
    });
  }

  /**
   * Configure the bootstrap options for the connection.
   */
  private Channel connect(ChannelInitializer<SocketChannel> handler) {
    // Configure worker pools and buffer allocator
    EventLoopGroup workerGroup = new NioEventLoopGroup();
    Bootstrap b = new Bootstrap();
    b.group(workerGroup);
    b.channel(NioSocketChannel.class);
    b.option(ChannelOption.SO_KEEPALIVE, true);
    // TODO(user): Evaluate use of pooled allocator
    b.option(ChannelOption.ALLOCATOR, UnpooledByteBufAllocator.DEFAULT);

    // Install the handler
    b.handler(handler);

    // Connect and wait for connection to be available
    ChannelFuture channelFuture = b.connect(host, port);
    try {
      // Wait for the connection
      channelFuture.get(5, TimeUnit.SECONDS);
      channel = channelFuture.channel();
      ChannelFuture closeFuture = channel.closeFuture();
      closeFuture.addListener(new WorkerCleanupListener(b.group()));
      return channel;
    } catch (TimeoutException te) {
      throw new IllegalStateException("Timeout waiting for connection to " + host + ":" + port, te);
    } catch (Throwable t) {
      throw new IllegalStateException("Error connecting to " + host + ":" + port, t);
    }
  }

  public void stop() {
    if (channel != null && channel.isOpen()) {
      try {
        channel.close().get();
      } catch (Exception e) {
        throw Throwables.propagate(e);
      }
    }
    channel = null;
  }

  private static class WorkerCleanupListener
      implements GenericFutureListener<io.netty.util.concurrent.Future<Void>> {
    private final EventLoopGroup workerGroup;

    public WorkerCleanupListener(EventLoopGroup workerGroup) {
      this.workerGroup = workerGroup;
    }

    @Override
    public void operationComplete(io.netty.util.concurrent.Future<Void> future) throws Exception {
      workerGroup.shutdownGracefully();
    }
  }

  /**
   * Report protocol upgrade completion using a promise.
   */
  private class UpgradeCompletionHandler extends ChannelHandlerAdapter {

    private Promise<Void> upgradePromise;

    @Override
    public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
      upgradePromise = ctx.newPromise();
    }

    public Promise<Void> getUpgradePromise() {
      return upgradePromise;
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (!upgradePromise.isDone()) {
        if (evt == HttpClientUpgradeHandler.UpgradeEvent.UPGRADE_REJECTED) {
          upgradePromise.setFailure(new Throwable());
        } else if (evt == HttpClientUpgradeHandler.UpgradeEvent.UPGRADE_SUCCESSFUL) {
          upgradePromise.setSuccess(null);
          ctx.pipeline().remove(this);
        }
      }
    }

    @Override
    public void channelInactive(ChannelHandlerContext ctx) throws Exception {
      super.channelInactive(ctx);
      if (!upgradePromise.isDone()) {
        upgradePromise.setFailure(new Throwable());
      }
    }

    @Override
    public void channelUnregistered(ChannelHandlerContext ctx) throws Exception {
      super.channelUnregistered(ctx);
      if (!upgradePromise.isDone()) {
        upgradePromise.setFailure(new Throwable());
      }
    }

    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
      super.exceptionCaught(ctx, cause);
      if (!upgradePromise.isDone()) {
        upgradePromise.setFailure(cause);
      }
    }
  }

  /**
   * Find Jetty's TLS NPN/ALPN extensions and attempt to use them
   *
   * @return true if NPN/ALPN support is available.
   */
  private static boolean installJettyTLSProtocolSelection(final SSLEngine engine,
          final SettableFuture<Void> protocolNegotiated) {
    for (String protocolNegoClassName : JETTY_TLS_NEGOTIATION_IMPL) {
      try {
        Class<?> negoClass;
        try {
          negoClass = Class.forName(protocolNegoClassName);
        } catch (ClassNotFoundException ignored) {
          // Not on the classpath.
          log.warningfmt("Jetty extension %s not found", protocolNegoClassName);
          continue;
        }
        Class<?> providerClass = Class.forName(protocolNegoClassName + "$Provider");
        Class<?> clientProviderClass = Class.forName(protocolNegoClassName + "$ClientProvider");
        Method putMethod = negoClass.getMethod("put", SSLEngine.class, providerClass);
        final Method removeMethod = negoClass.getMethod("remove", SSLEngine.class);
        putMethod.invoke(null, engine, Proxy.newProxyInstance(
            Http2Client.class.getClassLoader(),
            new Class[]{clientProviderClass},
            new InvocationHandler() {
              @Override
              public Object invoke(Object proxy, Method method, Object[] args) throws Throwable {
                String methodName = method.getName();
                switch (methodName) {
                  case "supports":
                    // both
                    return true;
                  case "unsupported":
                    // both
                    removeMethod.invoke(null, engine);
                    protocolNegotiated.setException(
                        new IllegalStateException("ALPN/NPN not supported by server"));
                    return null;
                  case "protocols":
                    // ALPN only
                    return ImmutableList.of(HTTP_VERSION_NAME);
                  case "selected":
                    // ALPN only
                    // Only 'supports' one protocol so we know what was 'selected.
                    removeMethod.invoke(null, engine);
                    protocolNegotiated.set(null);
                    return null;
                  case "selectProtocol":
                    // NPN only
                    @SuppressWarnings("unchecked")
                    List<String> names = (List<String>) args[0];
                    for (String name : names) {
                      if (name.startsWith(HTTP_VERSION_NAME)) {
                        protocolNegotiated.set(null);
                        return name;
                      }
                    }
                    protocolNegotiated.setException(
                        new IllegalStateException("Protocol not available via ALPN/NPN: " + names));
                    removeMethod.invoke(null, engine);
                    return null;
                }
                throw new IllegalStateException("Unknown method " + methodName);
              }
            }));
        return true;
      } catch (Exception e) {
        log.severefmt(e, "Unable to initialize protocol negotation for %s",
            protocolNegoClassName);
      }
    }
    return false;
  }
}
