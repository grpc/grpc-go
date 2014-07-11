package com.google.net.stubby.newtransport.netty;

import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.StreamListener;

import io.netty.bootstrap.Bootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.handler.codec.http2.DefaultHttp2StreamRemovalPolicy;

import java.util.concurrent.ExecutionException;

/**
 * A Netty-based {@link ClientTransport} implementation.
 */
class NettyClientTransport extends AbstractService implements ClientTransport {

  private final String host;
  private final int port;
  private final EventLoopGroup eventGroup;
  private final ChannelInitializer<SocketChannel> channelInitializer;
  private Channel channel;

  NettyClientTransport(String host, int port, boolean ssl) {
    this(host, port, ssl, new NioEventLoopGroup());
  }

  NettyClientTransport(String host, int port, boolean ssl, EventLoopGroup eventGroup) {
    Preconditions.checkNotNull(host, "host");
    Preconditions.checkArgument(port >= 0, "port must be positive");
    Preconditions.checkNotNull(eventGroup, "eventGroup");
    this.host = host;
    this.port = port;
    this.eventGroup = eventGroup;
    final DefaultHttp2StreamRemovalPolicy streamRemovalPolicy =
        new DefaultHttp2StreamRemovalPolicy();
    final NettyClientHandler handler = new NettyClientHandler(host, ssl, streamRemovalPolicy);
    // TODO(user): handle SSL.
    channelInitializer = new ChannelInitializer<SocketChannel>() {
      @Override
      protected void initChannel(SocketChannel ch) throws Exception {
        ch.pipeline().addLast(streamRemovalPolicy);
        ch.pipeline().addLast(handler);
      }
    };
  }

  @Override
  public ClientStream newStream(MethodDescriptor<?, ?> method, StreamListener listener) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(listener, "listener");
    switch (state()) {
      case STARTING:
        // Wait until the transport is running before creating the new stream.
        awaitRunning();
        break;
      case NEW:
      case TERMINATED:
      case FAILED:
        throw new IllegalStateException("Unable to create new stream in state: " + state());
      default:
        break;
    }

    // Create the stream.
    NettyClientStream stream = new NettyClientStream(listener, channel);

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
    b.handler(channelInitializer);

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
}
