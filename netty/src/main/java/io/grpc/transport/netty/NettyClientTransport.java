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
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.transport.ClientStream;
import io.grpc.transport.ClientStreamListener;
import io.grpc.transport.ClientTransport;
import io.netty.bootstrap.Bootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2LocalFlowController;
import io.netty.handler.codec.http2.DefaultHttp2StreamRemovalPolicy;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2FrameLogger;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.handler.codec.http2.Http2OutboundFrameLogger;
import io.netty.handler.codec.http2.Http2StreamRemovalPolicy;
import io.netty.handler.ssl.SslContext;
import io.netty.util.internal.logging.InternalLogLevel;

import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.ExecutionException;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.concurrent.GuardedBy;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLException;
import javax.net.ssl.SSLParameters;

/**
 * A Netty-based {@link ClientTransport} implementation.
 */
class NettyClientTransport implements ClientTransport {
  private static final Logger log = Logger.getLogger(NettyClientTransport.class.getName());

  private final SocketAddress address;
  private final Class<? extends Channel> channelType;
  private final EventLoopGroup group;
  private final Http2Negotiator.Negotiation negotiation;
  private final NettyClientHandler handler;
  private final boolean ssl;
  private final AsciiString authority;
  // We should not send on the channel until negotiation completes. This is a hard requirement
  // by SslHandler but is appropriate for HTTP/1.1 Upgrade as well.
  private Channel channel;
  private Listener listener;
  /**
   * Whether the transport started or failed during starting. Only transitions to true. When
   * changed, this.notifyAll() must be called.
   */
  private volatile boolean started;
  /** Guaranteed to be true when RUNNING. */
  private volatile boolean negotiationComplete;
  /** Whether the transport started shutting down. */
  @GuardedBy("this")
  private boolean shutdown;
  private Throwable shutdownCause;
  /** Whether the transport completed shutting down. */
  @GuardedBy("this")
  private boolean terminated;

  NettyClientTransport(SocketAddress address, Class<? extends Channel> channelType,
      NegotiationType negotiationType, EventLoopGroup group, SslContext sslContext) {
    Preconditions.checkNotNull(negotiationType, "negotiationType");
    this.address = Preconditions.checkNotNull(address, "address");
    this.group = Preconditions.checkNotNull(group, "group");
    this.channelType = Preconditions.checkNotNull(channelType, "channelType");

    InetSocketAddress inetAddress = null;
    if (address instanceof InetSocketAddress) {
      inetAddress = (InetSocketAddress) address;
      authority = new AsciiString(inetAddress.getHostString() + ":" + inetAddress.getPort());
    } else {
      Preconditions.checkState(negotiationType != NegotiationType.TLS,
          "TLS not supported for non-internet socket types");
      // Specialized address types are allowed to support custom Channel types so just assume their
      // toString() values are valid :authority values
      authority = new AsciiString(address.toString());
    }

    DefaultHttp2StreamRemovalPolicy streamRemovalPolicy = new DefaultHttp2StreamRemovalPolicy();
    handler = newHandler(streamRemovalPolicy);
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
        if (sslContext == null) {
          try {
            sslContext = SslContext.newClientContext();
          } catch (SSLException ex) {
            throw new RuntimeException(ex);
          }
        }
        // TODO(ejona86): specify allocator. The method currently ignores it though.
        SSLEngine sslEngine
            = sslContext.newEngine(null, inetAddress.getHostString(), inetAddress.getPort());
        SSLParameters sslParams = new SSLParameters();
        sslParams.setEndpointIdentificationAlgorithm("HTTPS");
        sslEngine.setSSLParameters(sslParams);
        negotiation = Http2Negotiator.tls(sslEngine, streamRemovalPolicy, handler);
        ssl = true;
        break;
      default:
        throw new IllegalArgumentException("Unsupported negotiationType: " + negotiationType);
    }
  }

  @Override
  public ClientStream newStream(MethodDescriptor<?, ?> method, Metadata.Headers headers,
      ClientStreamListener listener) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(headers, "headers");
    Preconditions.checkNotNull(listener, "listener");

    // We can't write to the channel until negotiation is complete.
    awaitStarted();
    if (!negotiationComplete) {
      throw new IllegalStateException("Negotiation failed to complete", shutdownCause);
    }

    // Create the stream.
    NettyClientStream stream = new NettyClientStream(listener, channel, handler);

    try {
      // Convert the headers into Netty HTTP/2 headers.
      AsciiString defaultPath = new AsciiString("/" + method.getName());
      Http2Headers http2Headers = Utils.convertClientHeaders(headers, ssl, defaultPath, authority);

      // Write the request and await creation of the stream.
      channel.writeAndFlush(new CreateStreamCommand(http2Headers, stream)).get();
    } catch (InterruptedException e) {
      // Restore the interrupt.
      Thread.currentThread().interrupt();
      stream.cancel();
      throw new RuntimeException(e);
    } catch (ExecutionException e) {
      stream.cancel();
      throw new RuntimeException(e.getCause() != null ? e.getCause() : e);
    }

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
    b.handler(negotiation.initializer());

    // Start the connection operation to the server.
    final ChannelFuture connectFuture = b.connect(address);
    channel = connectFuture.channel();

    connectFuture.addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          // The connection attempt failed.
          notifyTerminated(future.cause());
          return;
        }

        // Connected successfully, start the protocol negotiation.
        negotiation.onConnected(channel);
      }
    });
    Futures.addCallback(negotiation.completeFuture(), new FutureCallback<Void>() {
      @Override
      public void onSuccess(Void result) {
        // The negotiation was successful.
        negotiationComplete = true;
        notifyStarted();
      }

      @Override
      public void onFailure(Throwable t) {
        // The negotiation failed.
        notifyTerminated(t);
      }
    });

    // Handle transport shutdown when the channel is closed.
    channel.closeFuture().addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          // The close failed. Just notify that transport shutdown failed.
          notifyTerminated(future.cause());
          return;
        }

        if (handler.connectionError() != null) {
          // The handler encountered a connection error.
          notifyTerminated(handler.connectionError());
        } else {
          // Normal termination of the connection.
          notifyTerminated(null);
        }
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

  /**
   * Waits until started. Does not throw an exception if the transport has now failed.
   */
  private void awaitStarted() {
    if (!started) {
      try {
        synchronized (this) {
          while (!started) {
            wait();
          }
        }
      } catch (InterruptedException ex) {
        Thread.currentThread().interrupt();
        throw new RuntimeException("Interrupted while waiting for transport to start", ex);
      }
    }
  }

  private synchronized void notifyStarted() {
    started = true;
    notifyAll();
  }

  private void notifyShutdown(Throwable t) {
    if (t != null) {
      log.log(Level.SEVERE, "Transport failed", t);
    }
    boolean notifyShutdown;
    synchronized (this) {
      notifyShutdown = !shutdown;
      if (!shutdown) {
        shutdownCause = t;
        shutdown = true;
        notifyStarted();
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

  private static NettyClientHandler newHandler(Http2StreamRemovalPolicy streamRemovalPolicy) {
    Http2Connection connection =
        new DefaultHttp2Connection(false, streamRemovalPolicy);
    Http2FrameReader frameReader = new DefaultHttp2FrameReader();
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();

    Http2FrameLogger frameLogger = new Http2FrameLogger(InternalLogLevel.DEBUG);
    frameReader = new Http2InboundFrameLogger(frameReader, frameLogger);
    frameWriter = new Http2OutboundFrameLogger(frameWriter, frameLogger);

    DefaultHttp2LocalFlowController inboundFlow =
        new DefaultHttp2LocalFlowController(connection, frameWriter);
    return new NettyClientHandler(connection, frameReader, frameWriter, inboundFlow);
  }
}
