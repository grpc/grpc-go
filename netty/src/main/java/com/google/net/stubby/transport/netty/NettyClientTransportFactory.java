package com.google.net.stubby.transport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.transport.ClientTransportFactory;

import io.netty.channel.EventLoopGroup;
import io.netty.handler.ssl.SslContext;

import java.net.InetSocketAddress;

/**
 * Factory that manufactures instances of {@link NettyClientTransport}.
 */
class NettyClientTransportFactory implements ClientTransportFactory {

  private final InetSocketAddress address;
  private final NegotiationType negotiationType;
  private final EventLoopGroup group;
  private final SslContext sslContext;

  public NettyClientTransportFactory(InetSocketAddress address, NegotiationType negotiationType,
      EventLoopGroup group, SslContext sslContext) {
    this.address = Preconditions.checkNotNull(address, "address");
    this.group = Preconditions.checkNotNull(group, "group");
    this.negotiationType = Preconditions.checkNotNull(negotiationType, "negotiationType");
    this.sslContext = sslContext;
  }

  @Override
  public NettyClientTransport newClientTransport() {
    return new NettyClientTransport(address, negotiationType, group, sslContext);
  }
}
