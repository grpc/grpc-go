package com.google.net.stubby.testing.integration;

import com.google.net.stubby.ChannelImpl;
import com.google.net.stubby.transport.netty.NegotiationType;
import com.google.net.stubby.transport.netty.NettyChannelBuilder;
import com.google.net.stubby.transport.netty.NettyServerBuilder;

import org.junit.AfterClass;
import org.junit.BeforeClass;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Integration tests for GRPC over HTTP2 using the Netty framework.
 */
@RunWith(JUnit4.class)
public class Http2NettyTest extends AbstractTransportTest {
  private static int serverPort = Util.pickUnusedPort();

  @BeforeClass
  public static void startServer() {
    startStaticServer(NettyServerBuilder.forPort(serverPort));
  }

  @AfterClass
  public static void stopServer() {
    stopStaticServer();
  }

  @Override
  protected ChannelImpl createChannel() {
    return NettyChannelBuilder.forAddress("127.0.0.1", serverPort)
        .negotiationType(NegotiationType.PLAINTEXT).build();
  }
}
