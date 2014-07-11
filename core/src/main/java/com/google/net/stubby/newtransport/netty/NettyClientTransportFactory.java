package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.newtransport.ClientTransportFactory;

import io.netty.channel.EventLoopGroup;

/**
 * Factory that manufactures instances of {@link NettyClientTransport}.
 */
public class NettyClientTransportFactory implements ClientTransportFactory {

  private final String host;
  private final int port;
  private final boolean ssl;
  private final EventLoopGroup group;

  public NettyClientTransportFactory(String host, int port, boolean ssl, EventLoopGroup group) {
    this.group = Preconditions.checkNotNull(group, "group");
    Preconditions.checkArgument(port > 0, "Port must be positive");
    this.host = Preconditions.checkNotNull(host, "host");
    this.port = port;
    this.ssl = ssl;
  }

  @Override
  public NettyClientTransport newClientTransport() {
    return new NettyClientTransport(host, port, ssl, group);
  }
}
