package com.google.net.stubby.transport.netty;

import com.google.common.util.concurrent.Service;
import com.google.net.stubby.AbstractChannelBuilder;
import com.google.net.stubby.SharedResourceHolder;
import com.google.net.stubby.transport.ClientTransportFactory;

import io.netty.channel.EventLoopGroup;
import io.netty.handler.ssl.SslContext;

import java.net.InetSocketAddress;

/**
 * Convenient class for building channels with the netty transport.
 */
public final class NettyChannelBuilder extends AbstractChannelBuilder<NettyChannelBuilder> {

  private final InetSocketAddress serverAddress;

  private NegotiationType negotiationType = NegotiationType.TLS;
  private EventLoopGroup userEventLoopGroup;
  private SslContext sslContext;

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
   * is built, the builder will use the default one which is static.
   *
   * <p>The channel won't take ownership of the given EventLoopGroup. It's caller's responsibility
   * to shut it down when it's desired.
   */
  public NettyChannelBuilder eventLoopGroup(EventLoopGroup group) {
    userEventLoopGroup = group;
    return this;
  }

  /** SSL/TLS context to use instead of the system default. */
  public NettyChannelBuilder sslContext(SslContext sslContext) {
    this.sslContext = sslContext;
    return this;
  }

  @Override
  protected ChannelEssentials buildEssentials() {
    final EventLoopGroup group = (userEventLoopGroup == null)
        ? SharedResourceHolder.get(Utils.DEFAULT_CHANNEL_EVENT_LOOP_GROUP) : userEventLoopGroup;
    ClientTransportFactory transportFactory = new NettyClientTransportFactory(
        serverAddress, negotiationType, group, sslContext);
    Service.Listener listener = null;
    if (userEventLoopGroup == null) {
      listener = new ClosureHook() {
        @Override
        protected void onClosed() {
          SharedResourceHolder.release(Utils.DEFAULT_CHANNEL_EVENT_LOOP_GROUP, group);
        }
      };
    }
    return new ChannelEssentials(transportFactory, listener);
  }
}
