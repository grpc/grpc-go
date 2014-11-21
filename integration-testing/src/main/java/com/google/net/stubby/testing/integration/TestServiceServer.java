package com.google.net.stubby.testing.integration;

import com.google.common.base.Preconditions;
import com.google.common.collect.Maps;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.net.stubby.ServerImpl;
import com.google.net.stubby.ServerInterceptors;
import com.google.net.stubby.testing.TestUtils;
import com.google.net.stubby.transport.netty.NettyServerBuilder;

import io.netty.handler.ssl.SslContext;

import java.io.File;
import java.io.IOException;
import java.net.Socket;
import java.util.Map;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.logging.Logger;

/**
 * Server that manages startup/shutdown of a single {@code TestService}. The server can be started
 * with any one of the supported transports.
 */
public class TestServiceServer {
  private static final int DEFAULT_NUM_THREADS = 100;
  private static final int STARTUP_TIMEOUT_MILLS = 5000;
  private static final String RPC_PORT_ARG = "--port";
  private static final String TRANSPORT_ARG = "--transport";
  private static final String GRPC_VERSION_ARG = "--grpc_version";
  private final TestServiceImpl testService;

  /** Supported transport protocols */
  public enum Transport {
    HTTP2_NETTY, HTTP2_NETTY_TLS
  }

  private static final Logger log = Logger.getLogger(TestServiceServer.class.getName());

  private final ScheduledExecutorService executor;
  private final int port;
  private final ServerController serverController;

  /**
   * Constructs the GRPC server.
   *
   * @param transport the transport over which to send GRPC frames.
   * @param port the port to be used for RPC communications.
   */
  public TestServiceServer(Transport transport, int port)
      throws Exception {
    Preconditions.checkNotNull(transport, "transport");
    this.executor = Executors.newScheduledThreadPool(DEFAULT_NUM_THREADS);
    this.port = port;

    // Create the GRPC service.
    testService = new TestServiceImpl(executor);

    switch (transport) {
      case HTTP2_NETTY:
        serverController = new Http2NettyController(false);
        break;
      case HTTP2_NETTY_TLS:
        serverController = new Http2NettyController(true);
        break;
      default:
        throw new IllegalArgumentException("Unsupported transport: " + transport);
    }
  }

  public void start() throws Exception {
    serverController.start();
    assertStart();
    log.info("GRPC server started.");
  }

  public void stop() throws Exception {
    log.info("GRPC server stopping...");
    serverController.stop();
    MoreExecutors.shutdownAndAwaitTermination(executor, 5, TimeUnit.SECONDS);
  }

  /**
   * The main application allowing this server to be launched from the command line. Accepts the
   * following arguments:
   * <p>
   * --transport=<HTTP|HTTP2_NETTY|HTTP2_OKHTTP> Identifies the transport
   * over which GRPC frames should be sent. <br>
   * --port=<port number> The port number for RPC communications.
   * --grpc_version=<1|2> Use gRPC v2 protocol. Default is v1.
   */
  public static void main(String[] args) throws Exception {
    Map<String, String> argMap = parseArgs(args);
    Transport transport = getTransport(argMap);
    int port = getPort(RPC_PORT_ARG, argMap);

    com.google.net.stubby.transport.AbstractStream.GRPC_V2_PROTOCOL =
        getGrpcVersion(argMap) == 2;

    final TestServiceServer server = new TestServiceServer(transport, port);

    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        try {
          server.stop();
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });
    server.start();
  }

  private static Transport getTransport(Map<String, String> argMap) {
    String value = argMap.get(TRANSPORT_ARG.toLowerCase());
    Preconditions.checkNotNull(value, "%s argument must be provided.", TRANSPORT_ARG);
    Transport transport = Transport.valueOf(value.toUpperCase().trim());
    System.out.println(TRANSPORT_ARG + " set to: " + transport);
    return transport;
  }

  private static int getPort(String argName, Map<String, String> argMap) {
    String value = argMap.get(argName.toLowerCase());
    if (value != null) {
      int port = Integer.parseInt(value);
      System.out.println(argName + " set to port: " + port);
      return port;
    }

    int port = Util.pickUnusedPort();
    System.out.println(argName + " not, provided. Using port: " + port);
    return port;
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

  /**
   * A simple interface for an object controlling the lifecycle of the underlying server
   * implementation.
   */
  private interface ServerController {

    void start() throws Exception;

    void stop() throws Exception;
  }

  /**
   * A controller for a Netty-based Http2 server.
   */
  private class Http2NettyController implements ServerController {
    private final ServerImpl server;

    public Http2NettyController(boolean enableSSL) throws Exception {
      SslContext sslContext = null;
      if (enableSSL) {
        String dir = "integration-testing/certs";
        sslContext = SslContext.newServerContext(
            new File(dir + "/server1.pem"),
            new File(dir + "/server1.key"));
      }
      server = NettyServerBuilder.forPort(port)
          .executor(executor)
          .sslContext(sslContext)
          .addService(ServerInterceptors.intercept(TestServiceGrpc.bindService(testService),
                TestUtils.echoRequestHeadersInterceptor(Util.METADATA_KEY)))
          .build();
    }

    @Override
    public void start() throws Exception {
      executor.execute(new Runnable() {
        @Override
        public void run() {
          server.startAsync();
          server.awaitRunning();
        }
      });
    }

    @Override
    public void stop() throws Exception {
      server.stopAsync();
      server.awaitTerminated();
    }
  }

  // Asserts that the server has started listening.
  private void assertStart() throws Exception {
    long endTimeMillis = System.currentTimeMillis() + STARTUP_TIMEOUT_MILLS;
    boolean timedOut = false;
    while (!serverListening("localhost", port)) {
      if (System.currentTimeMillis() > endTimeMillis) {
        timedOut = true;
        break;
      }
    }
    if (timedOut) {
      throw new RuntimeException("Failed to start server.");
    }
    log.info("Server is listening on port: " + port);
  }

  private boolean serverListening(String host, int port) {
    Socket socket = null;
    try {
      socket = new Socket(host, port);
      return true;
    } catch (IOException e) {
      return false;
    } finally {
      if (socket != null) {
        try { socket.close(); } catch (IOException expected) {}
      }
    }
  }
}
