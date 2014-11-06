package com.google.net.stubby.transport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.transport.ClientTransportFactory;

import io.netty.channel.EventLoopGroup;

import java.net.InetSocketAddress;

/**
 * Factory that manufactures instances of {@link NettyClientTransport}.
 */
class NettyClientTransportFactory implements ClientTransportFactory {

  private final InetSocketAddress address;
  private final NegotiationType negotiationType;
  private final EventLoopGroup group;

  public NettyClientTransportFactory(InetSocketAddress address, NegotiationType negotiationType,
      EventLoopGroup group) {
    this.address = Preconditions.checkNotNull(address, "address");
    this.group = Preconditions.checkNotNull(group, "group");
    this.negotiationType = Preconditions.checkNotNull(negotiationType, "negotiationType");
  }

  @Override
  public NettyClientTransport newClientTransport() {
    return new NettyClientTransport(address, negotiationType, group);
  }
}
