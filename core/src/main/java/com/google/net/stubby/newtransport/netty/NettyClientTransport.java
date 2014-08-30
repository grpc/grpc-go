package com.google.net.stubby.newtransport.netty;

import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.newtransport.AbstractClientTransport;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.StreamListener;
import com.google.net.stubby.newtransport.netty.NettyClientTransportFactory.NegotiationType;
import com.google.net.stubby.testing.utils.ssl.SslContextFactory;

import io.netty.handler.codec.http2.Http2OutboundFrameLogger;

import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.util.internal.logging.InternalLogLevel;
import io.netty.handler.codec.http2.Http2FrameLogger;
import io.netty.bootstrap.Bootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.DefaultHttp2OutboundFlowController;
import io.netty.handler.codec.http2.DefaultHttp2StreamRemovalPolicy;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2OutboundFlowController;

import java.util.concurrent.ExecutionException;

import javax.net.ssl.SSLEngine;

/**
 * A Netty-based {@link ClientTransport} implementation.
 */
class NettyClientTransport extends AbstractClientTransport {

  private final String host;
  private final int port;
  private final EventLoopGroup eventGroup;
  private final Http2Negotiator.Negotiation negotiation;
  private final NettyClientHandler handler;
  private Channel channel;

  NettyClientTransport(String host, int port, NegotiationType negotiationType) {
    this(host, port, negotiationType, new NioEventLoopGroup());
  }

  NettyClientTransport(String host, int port, NegotiationType negotiationType,
      EventLoopGroup eventGroup) {
    Preconditions.checkNotNull(host, "host");
    Preconditions.checkArgument(port >= 0, "port must be positive");
    Preconditions.checkNotNull(eventGroup, "eventGroup");
    Preconditions.checkNotNull(negotiationType, "negotiationType");
    this.host = host;
    this.port = port;
    this.eventGroup = eventGroup;

    handler = newHandler(host, negotiationType == NegotiationType.TLS);
    switch (negotiationType) {
      case PLAINTEXT:
        negotiation = Http2Negotiator.plaintext(handler);
        break;
      case PLAINTEXT_UPGRADE:
        negotiation = Http2Negotiator.plaintextUpgrade(handler);
        break;
      case TLS:
        SSLEngine sslEngine = SslContextFactory.getClientContext().createSSLEngine();
        sslEngine.setUseClientMode(true);
        negotiation = Http2Negotiator.tls(handler, sslEngine);
        break;
      default:
        throw new IllegalArgumentException("Unsupported negotiationType: " + negotiationType);
    }
  }

  @Override
  protected ClientStream newStreamInternal(MethodDescriptor<?, ?> method, StreamListener listener) {
    // Create the stream.
    NettyClientStream stream = new NettyClientStream(listener, channel, handler.inboundFlow());

    try {
      // Write the request and await creation of the stream.
      channel.writeAndFlush(new CreateStreamCommand(method, stream)).get();
    } catch (InterruptedException e) {
      // Restore the interrupt.
      Thread.currentThread().interrupt();
      stream.dispose();
      throw new RuntimeException(e);
    } catch (ExecutionException e) {
      stream.dispose();
      throw new RuntimeException(e);
    }

    return stream;
  }

  @Override
  protected void doStart() {
    Bootstrap b = new Bootstrap();
    b.group(eventGroup);
    b.channel(NioSocketChannel.class);
    b.option(SO_KEEPALIVE, true);
    b.handler(negotiation.initializer());

    // Start the connection operation to the server.
    b.connect(host, port).addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (future.isSuccess()) {
          channel = future.channel();
          notifyStarted();

          // Listen for the channel close event.
          channel.closeFuture().addListener(new ChannelFutureListener() {
            @Override
            public void operationComplete(ChannelFuture future) throws Exception {
              if (future.isSuccess()) {
                notifyStopped();
              } else {
                notifyFailed(future.cause());
              }
            }
          });
        } else {
          notifyFailed(future.cause());
        }
      }
    });
  }

  @Override
  protected void doStop() {
    // No explicit call to notifyStopped() here, since this is automatically done when the
    // channel closes.
    if (channel != null && channel.isOpen()) {
      channel.close();
    }

    if (eventGroup != null) {
      eventGroup.shutdownGracefully();
    }
  }

  private static NettyClientHandler newHandler(String host, boolean ssl) {
    Http2Connection connection =
        new DefaultHttp2Connection(false, new DefaultHttp2StreamRemovalPolicy());
    Http2FrameReader frameReader = new DefaultHttp2FrameReader();
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();

    Http2FrameLogger frameLogger = new Http2FrameLogger(InternalLogLevel.DEBUG);
    frameReader = new Http2InboundFrameLogger(frameReader, frameLogger);
    frameWriter = new Http2OutboundFrameLogger(frameWriter, frameLogger);

    DefaultHttp2InboundFlowController inboundFlow =
        new DefaultHttp2InboundFlowController(connection, frameWriter);
    Http2OutboundFlowController outboundFlow =
        new DefaultHttp2OutboundFlowController(connection, frameWriter);
    return new NettyClientHandler(host,
        ssl,
        connection,
        frameReader,
        frameWriter,
        inboundFlow,
        outboundFlow);
  }
}
