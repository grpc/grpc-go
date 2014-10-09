package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.newtransport.ServerListener;
import com.google.net.stubby.newtransport.ServerTransportListener;

import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.socket.SocketChannel;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.DefaultHttp2OutboundFlowController;
import io.netty.handler.codec.http2.DefaultHttp2StreamRemovalPolicy;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2FrameLogger;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.handler.codec.http2.Http2OutboundFlowController;
import io.netty.handler.codec.http2.Http2OutboundFrameLogger;
import io.netty.util.internal.logging.InternalLogLevel;

/**
 * The Netty-based server transport.
 */
class NettyServerTransport extends AbstractService {
  private static final Http2FrameLogger frameLogger = new Http2FrameLogger(InternalLogLevel.DEBUG); 
  private final SocketChannel channel;
  private final ServerListener serverListener;
  private NettyServerHandler handler;

  NettyServerTransport(SocketChannel channel, ServerListener serverListener) {
    this.channel = Preconditions.checkNotNull(channel, "channel");
    this.serverListener = Preconditions.checkNotNull(serverListener, "serverListener");
  }

  @Override
  protected void doStart() {
    Preconditions.checkState(handler == null, "Handler already registered");

    // Notify the listener that this transport is being constructed.
    ServerTransportListener transportListener = serverListener.transportCreated(this);

    // Create the Netty handler for the pipeline.
    handler = createHandler(transportListener);

    // Notify when the channel closes.
    channel.closeFuture().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          // Close failed.
          notifyFailed(future.cause());
        } else if (handler.connectionError() != null) {
          // The handler encountered a connection error.
          notifyFailed(handler.connectionError());
        } else {
          // Normal termination of the connection.
          notifyStopped();
        }
      }
    });

    channel.pipeline().addLast(handler);

    notifyStarted();
  }

  @Override
  protected void doStop() {
    // No explicit call to notifyStopped() here, since this is automatically done when the
    // channel closes.
    if (channel.isOpen()) {
      channel.close();
    }
  }

  /**
   * Creates the Netty handler to be used in the channel pipeline.
   */
  private NettyServerHandler createHandler(ServerTransportListener transportListener) {
    Http2Connection connection =
        new DefaultHttp2Connection(true, new DefaultHttp2StreamRemovalPolicy());
    Http2FrameReader frameReader =
        new Http2InboundFrameLogger(new DefaultHttp2FrameReader(), frameLogger);
    Http2FrameWriter frameWriter =
        new Http2OutboundFrameLogger(new DefaultHttp2FrameWriter(), frameLogger);

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
