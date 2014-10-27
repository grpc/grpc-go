package com.google.net.stubby.testing.integration;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.net.stubby.Channel;
import com.google.net.stubby.ChannelImpl;
import com.google.net.stubby.transport.ClientTransportFactory;
import com.google.net.stubby.transport.netty.NettyClientTransportFactory;
import com.google.net.stubby.transport.okhttp.OkHttpClientTransportFactory;

import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;

import java.net.InetSocketAddress;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

/**
 * Starts a GRPC client with one of the supported transports.
 */
public class ClientBootstrap {
  /**
   * Protocol types
   */
  public enum Protocol {
    HTTP2
  }

  /**
   * Transport types
   */
  public enum Transport {
    NETTY(Protocol.HTTP2),
    NETTY_TLS(Protocol.HTTP2),
    OKHTTP(Protocol.HTTP2);

    private final Protocol protocol;

    Transport(Protocol protocol) {
      this.protocol = protocol;
    }

    public Protocol getProtocol() {
      return this.protocol;
    }
  }

  private final ExecutorService channelExecutor;
  private final EventLoopGroup eventGroup = new NioEventLoopGroup();
  private final ClientTransportFactory client;
  private ChannelImpl channel;

  /**
   * Constructs the GRPC client.
   *
   * @param transport the concrete implementation of the protocol.
   * @param serverHost the host of the GRPC server
   * @param serverPort the port of the GRPC server.
   */
  public ClientBootstrap(Transport transport, String serverHost, int serverPort) throws Exception {
    Preconditions.checkNotNull(transport, "transport");
    this.channelExecutor = Executors.newCachedThreadPool();

    switch (transport) {
      case NETTY:
        client = new NettyClientTransportFactory(new InetSocketAddress(serverHost, serverPort),
            NettyClientTransportFactory.NegotiationType.PLAINTEXT, eventGroup);
        break;
      case NETTY_TLS:
        client = new NettyClientTransportFactory(new InetSocketAddress(serverHost, serverPort),
            NettyClientTransportFactory.NegotiationType.TLS, eventGroup);
        break;
      case OKHTTP:
        client = new OkHttpClientTransportFactory(new InetSocketAddress(serverHost, serverPort),
            channelExecutor);
        break;
      default:
        throw new IllegalArgumentException("Unsupported transport: " + transport);
    }
  }

  /**
   * Starts the client and returns the {@link Channel} for communication to the server.
   */
  public Channel start() throws Exception {
    channel = new ChannelImpl(client, channelExecutor);
    channel.startAsync();
    channel.awaitRunning(5, TimeUnit.SECONDS);
    return channel;
  }

  /**
   * Shuts down the channel and transport.
   */
  public void stop() throws Exception {
    channel.stopAsync();
    channel.awaitTerminated(5, TimeUnit.SECONDS);
    MoreExecutors.shutdownAndAwaitTermination(channelExecutor, 5, TimeUnit.SECONDS);
    MoreExecutors.shutdownAndAwaitTermination(eventGroup, 5, TimeUnit.SECONDS);
  }
}
