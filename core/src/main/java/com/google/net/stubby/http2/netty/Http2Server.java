package com.google.net.stubby.http2.netty;

import com.google.common.collect.ImmutableList;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Session;

import io.netty.bootstrap.ServerBootstrap;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.ChannelOption;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.handler.codec.http2.Http2OrHttpChooser;
import io.netty.handler.ssl.SslContext;

import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.net.ssl.SSLEngine;

/**
 * Simple server connection startup that attaches a {@link Session} implementation to a connection.
 */
public class Http2Server implements Runnable {

  // Prefer ALPN to NPN so try it first.
  private static final String[] JETTY_TLS_NEGOTIATION_IMPL =
      {"org.eclipse.jetty.alpn.ALPN", "org.eclipse.jetty.npn.NextProtoNego"};

  public static final String HTTP_VERSION_NAME =
      Http2OrHttpChooser.SelectedProtocol.HTTP_2.protocolName();

  private static final Logger log = Logger.getLogger(Http2Server.class.getName());

  private final int port;
  private final Session session;
  private final RequestRegistry operations;
  private Channel channel;

  private final SslContext sslContext;
  private SettableFuture<Void> tlsNegotiatedHttp2;

  public Http2Server(int port, Session session, RequestRegistry operations) {
    this(port, session, operations, null);
  }

  public Http2Server(int port, Session session, RequestRegistry operations,
      @Nullable SslContext sslContext) {
    this.port = port;
    this.session = session;
    this.operations = operations;
    this.sslContext = sslContext;
    this.tlsNegotiatedHttp2 = null;
    if (sslContext != null) {
      tlsNegotiatedHttp2 = SettableFuture.create();
      if (!installJettyTLSProtocolSelection(sslContext.newEngine(null), tlsNegotiatedHttp2)) {
        throw new IllegalStateException("NPN/ALPN extensions not installed");
      }
    }
  }

  @Override
  public void run() {
    EventLoopGroup bossGroup = new NioEventLoopGroup(); // (1)
    EventLoopGroup workerGroup = new NioEventLoopGroup();
    try {
      ServerBootstrap b = new ServerBootstrap(); // (2)
      // TODO(user): Evaluate use of pooled allocator
      b.childOption(ChannelOption.ALLOCATOR, UnpooledByteBufAllocator.DEFAULT);
      b.group(bossGroup, workerGroup).channel(NioServerSocketChannel.class) // (3)
          .childHandler(new ChannelInitializer<SocketChannel>() { // (4)
            @Override
            public void initChannel(SocketChannel ch) throws Exception {
              if (sslContext != null) {
                ch.pipeline().addLast(sslContext.newHandler(ch.alloc()));
              }
              ch.pipeline().addLast(new Http2Codec(session, operations));
            }
          }).option(ChannelOption.SO_BACKLOG, 128) // (5)
          .childOption(ChannelOption.SO_KEEPALIVE, true); // (6)

      // Bind and startContext to accept incoming connections.
      ChannelFuture f = b.bind(port).sync(); // (7)

      // Wait until the server socket is closed.
      channel = f.channel();
      channel.closeFuture().sync();
    } catch (Exception e) {
      e.printStackTrace();
    } finally {
      workerGroup.shutdownGracefully();
      bossGroup.shutdownGracefully();
    }
  }

  public void stop() throws Exception {
    if (channel != null) {
      channel.close().get();
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
          log.warning("Jetty extension " + protocolNegoClassName + " not found");
          continue;
        }
        Class<?> providerClass = Class.forName(protocolNegoClassName + "$Provider");
        Class<?> serverProviderClass = Class.forName(protocolNegoClassName + "$ServerProvider");
        Method putMethod = negoClass.getMethod("put", SSLEngine.class, providerClass);
        final Method removeMethod = negoClass.getMethod("remove", SSLEngine.class);
        putMethod.invoke(null, engine, Proxy.newProxyInstance(
            Http2Server.class.getClassLoader(), new Class[] {serverProviderClass},
            new InvocationHandler() {
              @Override
              public Object invoke(Object proxy, Method method, Object[] args) throws Throwable {
                String methodName = method.getName();
                if ("unsupported".equals(methodName)) {
                  // both
                  log.warning("Calling unsupported");
                  removeMethod.invoke(null, engine);
                  protocolNegotiated.setException(new IllegalStateException(
                      "ALPN/NPN protocol " + HTTP_VERSION_NAME + " not supported by server"));
                  return null;
                }
                if ("protocols".equals(methodName)) {
                  // NPN only
                  return ImmutableList.of(HTTP_VERSION_NAME);
                }
                if ("protocolSelected".equals(methodName)) {
                  // NPN only
                  // Only 'supports' one protocol so we know what was selected.
                  removeMethod.invoke(null, engine);
                  protocolNegotiated.set(null);
                  return null;
                }
                if ("select".equals(methodName)) {
                  // ALPN only
                  log.warning("Calling select");
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
        log.log(Level.SEVERE,
            "Unable to initialize protocol negotation for " + protocolNegoClassName, e);
      }
    }
    return false;
  }
}
