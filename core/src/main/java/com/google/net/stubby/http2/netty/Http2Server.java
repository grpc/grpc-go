package com.google.net.stubby.http2.netty;

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

/**
 * Simple server connection startup that attaches a {@link Session} implementation to a connection.
 */
public class Http2Server implements Runnable {
  private final int port;
  private final Session session;
  private final RequestRegistry operations;
  private Channel channel;

  public Http2Server(int port, Session session, RequestRegistry operations) {
    this.port = port;
    this.session = session;
    this.operations = operations;
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
}
