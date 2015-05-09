/*
 * Copyright 2014, Google Inc. All rights reserved.
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

import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.transport.ClientStream;
import io.grpc.transport.ClientStreamListener;
import io.grpc.transport.ClientTransport;

import io.netty.bootstrap.Bootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionEncoder;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2FrameLogger;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.handler.codec.http2.Http2OutboundFrameLogger;
import io.netty.handler.logging.LogLevel;
import io.netty.util.AsciiString;

import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.concurrent.GuardedBy;

/**
 * A Netty-based {@link ClientTransport} implementation.
 */
class NettyClientTransport implements ClientTransport {
  private static final Logger log = Logger.getLogger(NettyClientTransport.class.getName());

  private final SocketAddress address;
  private final Class<? extends Channel> channelType;
  private final EventLoopGroup group;
  private final ProtocolNegotiator.Handler negotiationHandler;
  private final NettyClientHandler handler;
  private final AsciiString authority;
  private final int connectionWindowSize;
  private final int streamWindowSize;
  // We should not send on the channel until negotiation completes. This is a hard requirement
  // by SslHandler but is appropriate for HTTP/1.1 Upgrade as well.
  private Channel channel;
  private Listener listener;
  /** Whether the transport started shutting down. */
  @GuardedBy("this")
  private boolean shutdown;
  /** Whether the transport completed shutting down. */
  @GuardedBy("this")
  private boolean terminated;

  NettyClientTransport(SocketAddress address, Class<? extends Channel> channelType,
                       EventLoopGroup group, ProtocolNegotiator negotiator,
                       int connectionWindowSize, int streamWindowSize) {
    Preconditions.checkNotNull(negotiator, "negotiator");
    this.address = Preconditions.checkNotNull(address, "address");
    this.group = Preconditions.checkNotNull(group, "group");
    this.channelType = Preconditions.checkNotNull(channelType, "channelType");
    this.connectionWindowSize = connectionWindowSize;
    this.streamWindowSize = streamWindowSize;

    if (address instanceof InetSocketAddress) {
      InetSocketAddress inetAddress = (InetSocketAddress) address;
      authority = new AsciiString(inetAddress.getHostString() + ":" + inetAddress.getPort());
    } else {
      // Specialized address types are allowed to support custom Channel types so just assume their
      // toString() values are valid :authority values
      authority = new AsciiString(address.toString());
    }

    handler = newHandler();
    negotiationHandler = negotiator.newHandler(handler);
  }

  @Override
  public void ping(PingCallback callback, Executor executor) {
    // Write the command requesting the ping
    handler.getWriteQueue().enqueue(new SendPingCommand(callback, executor), true);
  }

  @Override
  public ClientStream newStream(MethodDescriptor<?, ?> method, Metadata.Headers headers,
      ClientStreamListener listener) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(headers, "headers");
    Preconditions.checkNotNull(listener, "listener");

    // Create the stream.
    final NettyClientStream stream = new NettyClientStream(listener, channel, handler);

    // Convert the headers into Netty HTTP/2 headers.
    AsciiString defaultPath = new AsciiString("/" + method.getName());
    Http2Headers http2Headers = Utils.convertClientHeaders(headers, negotiationHandler.scheme(),
        defaultPath, authority);

    ChannelFutureListener failureListener = new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          // Stream creation failed. Close the stream if not already closed.
          stream.transportReportStatus(Status.fromThrowable(future.cause()), true,
                  new Metadata.Trailers());
        }
      }
    };

    // Write the command requesting the creation of the stream.
    handler.getWriteQueue().enqueue(new CreateStreamCommand(http2Headers, stream),
            !method.getType().clientSendsOneMessage()).addListener(failureListener);
    return stream;
  }

  @Override
  public void start(Listener transportListener) {
    listener = Preconditions.checkNotNull(transportListener, "listener");
    Bootstrap b = new Bootstrap();
    b.group(group);
    b.channel(channelType);
    if (NioSocketChannel.class.isAssignableFrom(channelType)) {
      b.option(SO_KEEPALIVE, true);
    }
    /**
     * We don't use a ChannelInitializer in the client bootstrap because its "initChannel" method
     * is executed in the event loop and we need this handler to be in the pipeline immediately so
     * that it may begin buffering writes.
     */
    b.handler(negotiationHandler);
    // Start the connection operation to the server.
    channel = b.connect(address).channel();
    // Start the write queue as soon as the channel is constructed
    handler.startWriteQueue(channel);
    // Handle transport shutdown when the channel is closed.
    channel.closeFuture().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        notifyTerminated(handler.connectionError());
      }
    });
  }

  @Override
  public void shutdown() {
    notifyShutdown(null);
    // Notifying of termination is automatically done when the channel closes.
    if (channel != null && channel.isOpen()) {
      channel.close();
    }
  }

  private void notifyShutdown(Throwable t) {
    if (t != null) {
      log.log(Level.SEVERE, "Transport failed", t);
    }
    boolean notifyShutdown;
    synchronized (this) {
      notifyShutdown = !shutdown;
      if (!shutdown) {
        shutdown = true;
      }
    }
    if (notifyShutdown) {
      listener.transportShutdown();
    }
  }

  private void notifyTerminated(Throwable t) {
    notifyShutdown(t);
    boolean notifyTerminated;
    synchronized (this) {
      notifyTerminated = !terminated;
      terminated = true;
    }
    if (notifyTerminated) {
      listener.transportTerminated();
    }
  }

  private NettyClientHandler newHandler() {
    Http2Connection connection = new DefaultHttp2Connection(false);
    Http2FrameReader frameReader = new DefaultHttp2FrameReader();
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();

    Http2FrameLogger frameLogger = new Http2FrameLogger(LogLevel.DEBUG, getClass());
    frameReader = new Http2InboundFrameLogger(frameReader, frameLogger);
    frameWriter = new Http2OutboundFrameLogger(frameWriter, frameLogger);

    BufferingHttp2ConnectionEncoder encoder = new BufferingHttp2ConnectionEncoder(
            new DefaultHttp2ConnectionEncoder(connection, frameWriter));
    return new NettyClientHandler(encoder, connection, frameReader, connectionWindowSize,
        streamWindowSize);
  }
}
