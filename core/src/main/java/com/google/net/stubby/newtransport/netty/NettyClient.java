package com.google.net.stubby.newtransport.netty;

import static io.netty.channel.ChannelOption.SO_KEEPALIVE;

import com.google.common.util.concurrent.AbstractService;

import io.netty.bootstrap.Bootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioSocketChannel;

/**
 * Implementation of the {@link com.google.common.util.concurrent.Service} interface for a
 * Netty-based client.
 */
public class NettyClient extends AbstractService {
  private final String host;
  private final int port;
  private final ChannelInitializer<SocketChannel> channelInitializer;
  private Channel channel;
  private EventLoopGroup eventGroup;

  public NettyClient(String host, int port, ChannelInitializer<SocketChannel> channelInitializer) {
    this.host = host;
    this.port = port;
    this.channelInitializer = channelInitializer;
  }

  public Channel channel() {
    return channel;
  }

  @Override
  protected void doStart() {
    eventGroup = new NioEventLoopGroup();

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
        } else {
          notifyFailed(future.cause());
        }
      }
    });
  }

  @Override
  protected void doStop() {
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

    if (eventGroup != null) {
      eventGroup.shutdownGracefully();
    }
  }
}
