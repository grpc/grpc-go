package com.google.net.stubby.newtransport.netty;

import static io.netty.channel.ChannelOption.SO_BACKLOG;
import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.newtransport.ServerListener;
import com.google.net.stubby.newtransport.ServerTransportListener;

import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.DefaultHttp2OutboundFlowController;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2FrameLogger;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.handler.codec.http2.Http2OutboundFlowController;
import io.netty.handler.codec.http2.Http2OutboundFrameLogger;
import io.netty.util.internal.logging.InternalLogLevel;

import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2StreamRemovalPolicy;

/**
 * Implementation of the {@link com.google.common.util.concurrent.Service} interface for a
 * Netty-based server.
 */
public class NettyServer extends AbstractService {
  private final int port;
  private final ChannelInitializer<SocketChannel> channelInitializer;
  private final EventLoopGroup bossGroup;
  private final EventLoopGroup workerGroup;
  private Channel channel;

  public NettyServer(ServerListener serverListener, int port) {
    this(serverListener, port, new NioEventLoopGroup(), new NioEventLoopGroup());
  }

  public NettyServer(final ServerListener serverListener, int port, EventLoopGroup bossGroup,
      EventLoopGroup workerGroup) {
    Preconditions.checkNotNull(bossGroup, "bossGroup");
    Preconditions.checkNotNull(workerGroup, "workerGroup");
    Preconditions.checkArgument(port >= 0, "port must be positive");
    this.port = port;
    this.channelInitializer = new ChannelInitializer<SocketChannel>() {
      @Override
      public void initChannel(SocketChannel ch) throws Exception {
        // TODO(user): pass a real transport object
        ServerTransportListener transportListener = serverListener.transportCreated(null);
        ch.pipeline().addLast(newHandler(transportListener));
      }
    };
    this.bossGroup = bossGroup;
    this.workerGroup = workerGroup;
  }

  @Override
  protected void doStart() {
    ServerBootstrap b = new ServerBootstrap();
    b.group(bossGroup, workerGroup);
    b.channel(NioServerSocketChannel.class);
    b.option(SO_BACKLOG, 128);
    b.childOption(SO_KEEPALIVE, true);
    b.childHandler(channelInitializer);

    // Bind and start to accept incoming connections.
    b.bind(port).addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (future.isSuccess()) {
          channel = future.channel();
          notifyStarted();
        } else {
          notifyFailed(future.cause());
        }
      }
    });
  }

  @Override
  protected void doStop() {
    // Wait for the channel to close.
    if (channel != null && channel.isOpen()) {
      channel.close().addListener(new ChannelFutureListener() {
        @Override
        public void operationComplete(ChannelFuture future) throws Exception {
          if (future.isSuccess()) {
            notifyStopped();
          } else {
            notifyFailed(future.cause());
          }
        }
      });
    }

    // Wait for the event loop group to shutdown.
    if (bossGroup != null) {
      bossGroup.shutdownGracefully();
    }
    if (workerGroup != null) {
      workerGroup.shutdownGracefully();
    }
  }

  private static NettyServerHandler newHandler(ServerTransportListener transportListener) {
    Http2Connection connection =
        new DefaultHttp2Connection(true, new DefaultHttp2StreamRemovalPolicy());
    Http2FrameReader frameReader = new DefaultHttp2FrameReader();
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();

    Http2FrameLogger frameLogger = new Http2FrameLogger(InternalLogLevel.DEBUG);
    frameReader = new Http2InboundFrameLogger(frameReader, frameLogger);
    frameWriter = new Http2OutboundFrameLogger(frameWriter, frameLogger);

    DefaultHttp2InboundFlowController inboundFlow =
        new DefaultHttp2InboundFlowController(connection, frameWriter);
    Http2OutboundFlowController outboundFlow =
        new DefaultHttp2OutboundFlowController(connection, frameWriter);
    return new NettyServerHandler(transportListener,
        connection,
        frameReader,
        frameWriter,
        inboundFlow,
        outboundFlow);
  }
}
