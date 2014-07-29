package com.google.net.stubby.newtransport.netty;

import com.google.common.base.Preconditions;
import com.google.net.stubby.newtransport.ClientTransportFactory;

import io.netty.channel.EventLoopGroup;

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

  private final String host;
  private final int port;
  private final NegotiationType negotiationType;
  private final EventLoopGroup group;

  public NettyClientTransportFactory(String host, int port, NegotiationType negotiationType,
      EventLoopGroup group) {
    this.group = Preconditions.checkNotNull(group, "group");
    Preconditions.checkArgument(port > 0, "Port must be positive");
    this.host = Preconditions.checkNotNull(host, "host");
    this.negotiationType = Preconditions.checkNotNull(negotiationType, "negotiationType");
    this.port = port;
  }

  @Override
  public NettyClientTransport newClientTransport() {
    return new NettyClientTransport(host, port, negotiationType, group);
  }
}
