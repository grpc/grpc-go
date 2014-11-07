package com.google.net.stubby.testing.integration;

import com.google.net.stubby.ChannelImpl;
import com.google.net.stubby.transport.AbstractStream;
import com.google.net.stubby.transport.netty.NettyServerBuilder;
import com.google.net.stubby.transport.okhttp.OkHttpChannelBuilder;

import org.junit.AfterClass;
import org.junit.BeforeClass;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Integration tests for GRPC over Http2 using the OkHttp framework.
 */
@RunWith(JUnit4.class)
public class Http2OkHttpTest extends AbstractTransportTest {
  private static int serverPort = Util.pickUnusedPort();

  @BeforeClass
  public static void startServer() throws Exception {
    AbstractStream.GRPC_V2_PROTOCOL = true;
    startStaticServer(NettyServerBuilder.forPort(serverPort));
  }

  @AfterClass
  public static void stopServer() throws Exception {
    stopStaticServer();
    AbstractStream.GRPC_V2_PROTOCOL = false;
  }

  @Override
  protected ChannelImpl createChannel() {
    return OkHttpChannelBuilder.forAddress("127.0.0.1", serverPort).build();
  }
}
