package com.google.net.stubby.transport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.transport.ClientTransportFactory;

import io.netty.channel.EventLoopGroup;

import java.net.InetSocketAddress;

/**
 * Factory that manufactures instances of {@link NettyClientTransport}.
 */
public class NettyClientTransportFactory implements ClientTransportFactory {

  /**
   * Identifies the negotiation used for starting up HTTP/2.
   */
  public enum NegotiationType {
    /**
     * Uses TLS ALPN/NPN negotiation, assumes an SSL connection.
     */
    TLS,

    /**
     * Use the HTTP UPGRADE protocol for a plaintext (non-SSL) upgrade from HTTP/1.1 to HTTP/2.
     */
    PLAINTEXT_UPGRADE,

    /**
     * Just assume the connection is plaintext (non-SSL) and the remote endpoint supports HTTP/2
     * directly without an upgrade.
     */
    PLAINTEXT
  }

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
