package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.newtransport.ServerListener;
import com.google.net.stubby.newtransport.ServerTransportListener;

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

  NettyServerHandler handler;

  @Override
  protected void doStart() {
    notifyStarted();
  }

  @Override
  protected void doStop() {
    // TODO(user): signal GO_AWAY and optionally terminate the socket after a timeout
    notifyStopped();
  }

  /**
   * This must be called when the transport is starting or running.
   */
  void bind(SocketChannel ch, ServerListener serverListener) {
    Preconditions.checkState(handler == null, "Handler already registered");
    ServerTransportListener transportListener = serverListener.transportCreated(this);
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
    handler = new NettyServerHandler(transportListener,
        connection,
        frameReader,
        frameWriter,
        inboundFlow,
        outboundFlow);
    ch.pipeline().addLast(handler);
  }
}
