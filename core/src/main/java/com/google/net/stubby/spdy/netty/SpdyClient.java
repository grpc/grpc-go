package com.google.net.stubby.spdy.netty;

import com.google.common.base.Throwables;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Session;

import io.netty.bootstrap.Bootstrap;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.ChannelOption;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.spdy.SpdyFrameCodec;
import io.netty.handler.codec.spdy.SpdyVersion;
import io.netty.handler.ssl.SslHandler;
import io.netty.util.concurrent.GenericFutureListener;

import javax.annotation.Nullable;
import javax.net.ssl.SSLEngine;

/**
 * Simple client connection startup that creates a {@link SpdySession} for use
 * with protocol bindings.
 */
public class SpdyClient {
  private final String host;
  private final int port;
  private final RequestRegistry requestRegistry;
  private ChannelFuture channelFuture;
  private final SSLEngine sslEngine;

  public SpdyClient(String host, int port, RequestRegistry requestRegistry) {
    this(host, port, requestRegistry, null);
  }

  public SpdyClient(String host, int port, RequestRegistry requestRegistry,
                    @Nullable SSLEngine sslEngine) {
    this.host = host;
    this.port = port;
    this.requestRegistry = requestRegistry;
    this.sslEngine = sslEngine;
    // TODO(user): NPN support
    if (sslEngine != null) {
      sslEngine.setUseClientMode(true);
    }
  }

  public Session startAndWait() {
    EventLoopGroup workerGroup = new NioEventLoopGroup();
    try {
      Bootstrap b = new Bootstrap(); // (1)
      b.group(workerGroup); // (2)
      b.channel(NioSocketChannel.class); // (3)
      b.option(ChannelOption.SO_KEEPALIVE, true); // (4)
      // TODO(user): Evaluate use of pooled allocator
      b.option(ChannelOption.ALLOCATOR, UnpooledByteBufAllocator.DEFAULT);
      b.handler(new ChannelInitializer<SocketChannel>() {
        @Override
        public void initChannel(SocketChannel ch) throws Exception {
          if (sslEngine != null) {
            // Assume TLS when using SSL
            ch.pipeline().addLast(new SslHandler(sslEngine, false));
          }
          ch.pipeline().addLast(
              new SpdyFrameCodec(SpdyVersion.SPDY_3_1),
              new SpdyCodec(requestRegistry));
        }
      });
      // Start the client.
      channelFuture = b.connect(host, port);
      // Wait for the connection
      channelFuture.sync(); // (5)
      ChannelFuture closeFuture = channelFuture.channel().closeFuture();
      closeFuture.addListener(new WorkerCleanupListener(workerGroup));
      return new SpdySession(channelFuture.channel(), requestRegistry);
    } catch (Throwable t) {
      workerGroup.shutdownGracefully();
      throw Throwables.propagate(t);
    }
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


}
