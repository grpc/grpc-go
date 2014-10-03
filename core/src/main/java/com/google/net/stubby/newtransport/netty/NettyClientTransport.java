package com.google.net.stubby.newtransport.netty;

import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.newtransport.AbstractClientTransport;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientStreamListener;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.netty.NettyClientTransportFactory.NegotiationType;
import com.google.net.stubby.testing.utils.ssl.SslContextFactory;

import io.netty.bootstrap.Bootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.AsciiString;
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
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.handler.codec.http2.Http2OutboundFlowController;
import io.netty.handler.codec.http2.Http2OutboundFrameLogger;
import io.netty.util.internal.logging.InternalLogLevel;

import java.net.InetSocketAddress;
import java.util.concurrent.ExecutionException;

import javax.net.ssl.SSLEngine;

/**
 * A Netty-based {@link ClientTransport} implementation.
 */
class NettyClientTransport extends AbstractClientTransport {

  private final InetSocketAddress address;
  private final EventLoopGroup eventGroup;
  private final Http2Negotiator.Negotiation negotiation;
  private final NettyClientHandler handler;
  private final boolean ssl;
  private final AsciiString authority;
  private Channel channel;

  NettyClientTransport(InetSocketAddress address, NegotiationType negotiationType) {
    this(address, negotiationType, new NioEventLoopGroup());
  }

  NettyClientTransport(InetSocketAddress address, NegotiationType negotiationType,
      EventLoopGroup eventGroup) {
    Preconditions.checkNotNull(negotiationType, "negotiationType");
    this.address = Preconditions.checkNotNull(address, "address");
    this.eventGroup = Preconditions.checkNotNull(eventGroup, "eventGroup");

    authority = new AsciiString(address.getHostString() + ":" + address.getPort());

    handler = newHandler();
    switch (negotiationType) {
      case PLAINTEXT:
        negotiation = Http2Negotiator.plaintext(handler);
        ssl = false;
        break;
      case PLAINTEXT_UPGRADE:
        negotiation = Http2Negotiator.plaintextUpgrade(handler);
        ssl = false;
        break;
      case TLS:
        SSLEngine sslEngine = SslContextFactory.getClientContext().createSSLEngine();
        sslEngine.setUseClientMode(true);
        negotiation = Http2Negotiator.tls(handler, sslEngine);
        ssl = true;
        break;
      default:
        throw new IllegalArgumentException("Unsupported negotiationType: " + negotiationType);
    }
  }

  @Override
  protected ClientStream newStreamInternal(MethodDescriptor<?, ?> method, Metadata.Headers headers,
      ClientStreamListener listener) {
    // Create the stream.
    NettyClientStream stream = new NettyClientStream(listener, channel, handler.inboundFlow());

    try {
      // Convert the headers into Netty HTTP/2 headers.
      AsciiString defaultPath = new AsciiString("/" + method.getName());
      Http2Headers http2Headers = Utils.convertHeaders(headers, ssl, defaultPath, authority);

      // Write the request and await creation of the stream.
      channel.writeAndFlush(new CreateStreamCommand(http2Headers, stream)).get();
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
    b.connect(address).addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          // The connection attempt failed.
          notifyFailed(future.cause());
          return;
        }

        // Connected successfully, start the protocol negotiation.
        channel = future.channel();
        negotiation.onConnected(channel);

        final ListenableFuture<Void> negotiationFuture = negotiation.completeFuture();
        Futures.addCallback(negotiationFuture, new FutureCallback<Void>() {
          @Override
          public void onSuccess(Void result) {
            // The negotiation was successful.
            notifyStarted();

            // Handle transport shutdown when the channel is closed.
            channel.closeFuture().addListener(new ChannelFutureListener() {
              @Override
              public void operationComplete(ChannelFuture future) throws Exception {
                if (!future.isSuccess()) {
                  // The close failed. Just notify that transport shutdown failed.
                  notifyFailed(future.cause());
                  return;
                }

                if (handler.connectionError() != null) {
                  // The handler encountered a connection error.
                  notifyFailed(handler.connectionError());
                } else {
                  // Normal termination of the connection.
                  notifyStopped();
                }
              }
            });
          }

          @Override
          public void onFailure(Throwable t) {
            // The negotiation failed.
            notifyFailed(t);
          }
        });
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
  }

  private static NettyClientHandler newHandler() {
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
    return new NettyClientHandler(connection, frameReader, frameWriter, inboundFlow, outboundFlow);
  }
}
