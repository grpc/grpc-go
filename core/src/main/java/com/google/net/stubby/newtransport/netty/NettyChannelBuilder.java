package com.google.net.stubby.newtransport.netty;

import com.google.common.util.concurrent.Service;
import com.google.net.stubby.AbstractChannelBuilder;
import com.google.net.stubby.newtransport.ClientTransportFactory;
import com.google.net.stubby.newtransport.netty.NettyClientTransportFactory.NegotiationType;

import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;

import java.net.InetSocketAddress;

/**
 * Convenient class for building channels with the netty transport.
 */
public final class NettyChannelBuilder extends AbstractChannelBuilder<NettyChannelBuilder> {

  private final InetSocketAddress serverAddress;

  private NegotiationType negotiationType = NegotiationType.TLS;
  private EventLoopGroup userEventLoopGroup;

  /**
   * Creates a new builder with the given server address.
   */
  public static NettyChannelBuilder forAddress(InetSocketAddress serverAddress) {
    return new NettyChannelBuilder(serverAddress);
  }

  /**
   * Creates a new builder with the given host and port.
   */
  public static NettyChannelBuilder forAddress(String host, int port) {
    return forAddress(new InetSocketAddress(host, port));
  }

  private NettyChannelBuilder(InetSocketAddress serverAddress) {
    this.serverAddress = serverAddress;
  }

  /**
   * Sets the negotiation type for the HTTP/2 connection.
   *
   * <p>Default: <code>TLS</code>
   */
  public NettyChannelBuilder negotiationType(NegotiationType type) {
    negotiationType = type;
    return this;
  }

  /**
   * Provides an EventGroupLoop to be used by the netty transport.
   *
   * <p>It's an optional parameter. If the user has not provided an EventGroupLoop when the channel
   * is built, the builder will create one.
   *
   * <p>The channel won't take ownership of the given EventLoopGroup. It's caller's responsibility
   * to shut it down when it's desired.
   */
  public NettyChannelBuilder eventLoopGroup(EventLoopGroup group) {
    userEventLoopGroup = group;
    return this;
  }

  @Override
  protected ChannelEssentials buildEssentials() {
    final EventLoopGroup group = (userEventLoopGroup == null)
        ? new NioEventLoopGroup() : userEventLoopGroup;
    ClientTransportFactory transportFactory = new NettyClientTransportFactory(
        serverAddress, negotiationType, group);
    Service.Listener listener = null;
    // We shut down the EventLoopGroup only if we created it.
    if (userEventLoopGroup == null) {
      listener = new ClosureHook() {
        @Override
        protected void onClosed() {
          group.shutdownGracefully();
        }
      };
    }
    return new ChannelEssentials(transportFactory, listener);
  }
}
