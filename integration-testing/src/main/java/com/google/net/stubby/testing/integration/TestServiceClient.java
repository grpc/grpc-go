package com.google.net.stubby.testing.integration;

import static com.google.net.stubby.testing.integration.Messages.PayloadType.COMPRESSABLE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.common.collect.Maps;
import com.google.net.stubby.ChannelImpl;
import com.google.net.stubby.stub.StreamRecorder;
import com.google.net.stubby.testing.integration.Messages.SimpleRequest;
import com.google.net.stubby.testing.integration.Messages.SimpleResponse;
import com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest;
import com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse;
import com.google.net.stubby.testing.integration.TestServiceGrpc.TestService;
import com.google.net.stubby.transport.okhttp.OkHttpChannelBuilder;
import com.google.net.stubby.transport.netty.NettyChannelBuilder;
import com.google.net.stubby.transport.netty.NettyClientTransportFactory;

import java.util.Map;
import java.util.concurrent.TimeUnit;

/**
 * Application that starts a client for the {@link TestServiceGrpc.TestService} and runs through a
 * series of tests.
 */
public class TestServiceClient {
  private static final String SERVER_HOST_ARG = "--server_host";
  private static final String SERVER_PORT_ARG = "--server_port";
  private static final String TRANSPORT_ARG = "--transport";
  private static final String TEST_CASE_ARG = "--test_case";
  private static final String GRPC_VERSION_ARG = "--grpc_version";

  private enum Transport {
    NETTY {
      @Override
      public ChannelImpl createChannel(String serverHost, int serverPort) {
        return NettyChannelBuilder.forAddress(serverHost, serverPort)
            .negotiationType(NettyClientTransportFactory.NegotiationType.PLAINTEXT).build();
      }
    },
    NETTY_TLS {
      @Override
      public ChannelImpl createChannel(String serverHost, int serverPort) {
        return NettyChannelBuilder.forAddress(serverHost, serverPort)
            .negotiationType(NettyClientTransportFactory.NegotiationType.TLS).build();
      }
    },
    OKHTTP {
      @Override
      public ChannelImpl createChannel(String serverHost, int serverPort) {
        return OkHttpChannelBuilder.forAddress(serverHost, serverPort).build();
      }
    },
    ;

    public abstract ChannelImpl createChannel(String serverHost, int serverPort);
  }

  /**
   * The main application allowing this client to be launched from the command line. Accepts the
   * following arguments:
   * <p>
   * --transport=NETTY|NETTY_TLS|OKHTTP Identifies the concrete implementation of the
   * transport. <br>
   * --serverHost=The host of the remote server.<br>
   * --serverPort=$port_number The port of the remote server.<br>
   * --test_case=empty_unary|server_streaming The client test to run.<br>
   * --grpc_version=1|2 Use gRPC v2 protocol. Default is 1.
   */
  public static void main(String[] args) throws Exception {
    Map<String, String> argMap = parseArgs(args);
    Transport transport = getTransport(argMap);
    String serverHost = getServerHost(argMap);
    int serverPort = getPort(argMap);
    String testCase = getTestCase(argMap);

    com.google.net.stubby.transport.AbstractStream.GRPC_V2_PROTOCOL =
        getGrpcVersion(argMap) == 2; 

    final Tester tester = new Tester(transport, serverHost, serverPort);
    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        System.out.println("Shutting down");
        try {
          tester.teardown();
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });

    tester.setup();
    System.out.println("Running test " + testCase);
    try {
      runTest(tester, testCase);
    } catch (Exception ex) {
      ex.printStackTrace();
      tester.teardown();
      System.exit(1);
    }
    System.out.println("Test completed.");
    tester.teardown();
  }

  private static Transport getTransport(Map<String, String> argMap) {
    String value = argMap.get(TRANSPORT_ARG.toLowerCase());
    Preconditions.checkNotNull(value, "%s argument must be provided.", TRANSPORT_ARG);
    Transport transport = Transport.valueOf(value.toUpperCase().trim());
    System.out.println(TRANSPORT_ARG + " set to: " + transport);
    return transport;
  }

  private static String getServerHost(Map<String, String> argMap) {
    String value = argMap.get(SERVER_HOST_ARG.toLowerCase());
    if (value == null) {
      throw new IllegalArgumentException(
          "Must provide " + SERVER_HOST_ARG + " command-line argument");
    }
    System.out.println(SERVER_HOST_ARG + " set to: " + value);
    return value;
  }

  private static int getPort(Map<String, String> argMap) {
    String value = argMap.get(SERVER_PORT_ARG.toLowerCase());
    if (value == null) {
      throw new IllegalArgumentException(
          "Must provide numeric " + SERVER_PORT_ARG + " command-line argument");
    }
    int port = Integer.parseInt(value);
    System.out.println(SERVER_PORT_ARG + " set to port: " + port);
    return port;
  }

  private static String getTestCase(Map<String, String> argMap) {
    String value = argMap.get(TEST_CASE_ARG);
    if (value == null) {
      throw new IllegalArgumentException(
          "Must provide " + TEST_CASE_ARG + " command-line argument");
    }
    System.out.println(TEST_CASE_ARG + " set to: " + value);
    return value;
  }

  private static int getGrpcVersion(Map<String, String> argMap) {
    String value = argMap.get(GRPC_VERSION_ARG.toLowerCase());
    if (value == null) {
      return 1;
    }
    int version = Integer.parseInt(value);
    System.out.println(GRPC_VERSION_ARG + " set to version: " + version);
    return version;
  }

  private static Map<String, String> parseArgs(String[] args) {
    Map<String, String> argMap = Maps.newHashMap();
    for (String arg : args) {
      String[] parts = arg.split("=");
      Preconditions.checkArgument(parts.length == 2, "Failed parsing argument: %s", arg);
      argMap.put(parts[0].toLowerCase().trim(), parts[1].trim());
    }

    return argMap;
  }

  private static void runTest(Tester tester, String testCase) throws Exception {
    if ("empty_unary".equals(testCase)) {
      tester.emptyUnary();
    } else if ("large_unary".equals(testCase)) {
      tester.largeUnary();
    } else if ("client_streaming".equals(testCase)) {
      tester.clientStreaming();
    } else if ("server_streaming".equals(testCase)) {
      tester.serverStreaming();
    } else if ("ping_pong".equals(testCase)) {
      tester.pingPong();
    } else {
      throw new IllegalArgumentException("Unknown test case: " + testCase);
    }
  }

  private static class Tester extends AbstractTransportTest {
    private final Transport transport;
    private final String host;
    private final int port;

    public Tester(Transport transport, String host, int port) {
      this.transport = transport;
      this.host = host;
      this.port = port;
    }

    @Override
    protected ChannelImpl createChannel() {
      return transport.createChannel(host, port);
    }
  }
}
