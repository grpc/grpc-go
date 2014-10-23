package com.google.net.stubby.testing.integration;

import static com.google.net.stubby.testing.integration.Messages.PayloadType.COMPRESSABLE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.common.collect.Maps;
import com.google.net.stubby.Channel;
import com.google.net.stubby.stub.StreamRecorder;
import com.google.net.stubby.testing.integration.Messages.SimpleRequest;
import com.google.net.stubby.testing.integration.Messages.SimpleResponse;
import com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest;
import com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse;

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
  private static final String STUB_TYPE_ARG = "--stub_type";
  private static final String MESSAGE_TYPE_ARG = "--call_type";
  private static final String GRPC_VERSION_ARG = "--grpc_version";

  /**
   * Stub types
   */
  private enum StubType {
    BLOCKING, ASYNC
  }

  /**
   * Call types
   */
  private enum CallType {
    UNARY, STREAMING_OUTPUT, STREAMING_INPUT, FULL_DUPLEX, HALF_DUPLEX
  }

  private static final SimpleRequest UNARY_CALL_REQUEST;
  private static final StreamingOutputCallRequest STREAMING_OUTPUT_CALL_REQUEST;
  static {
    SimpleRequest.Builder unaryBuilder = SimpleRequest.newBuilder();
    unaryBuilder.setResponseType(COMPRESSABLE).setResponseSize(10);
    UNARY_CALL_REQUEST = unaryBuilder.build();

    ImmutableList<Integer> responseSizes = ImmutableList.of(50, 100, 150, 200);
    StreamingOutputCallRequest.Builder streamingOutputBuilder =
        StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size)
          .setIntervalUs(0);
    }
    STREAMING_OUTPUT_CALL_REQUEST = streamingOutputBuilder.build();
  }

  public static Channel startClient(ClientBootstrap.Transport transport,
                                    String serverHost, int serverPort) throws Exception {

    final ClientBootstrap client = new ClientBootstrap(transport, serverHost, serverPort);

    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        try {
          System.out.println("Shutting down");
          client.stop();
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });

    // Start the client.
    Channel channel = client.start();
    System.out.println("Client started");
    return channel;
  }

  public static void runScenarioBlockingUnary(ClientBootstrap.Transport transport,
                                              String serverHost, int serverPort) throws Exception {

    Channel channel = startClient(transport, serverHost, serverPort); 
    final TestServiceGrpc.TestServiceBlockingClient grpcStub
        = TestServiceGrpc.newBlockingStub(channel); 
    try {
      System.out.println("Blocking stub: sending unary call...");

      final SimpleResponse response = grpcStub.unaryCall(UNARY_CALL_REQUEST);
      assertNotNull(response);
      assertEquals(COMPRESSABLE, response.getPayload().getType());
      assertEquals(10, response.getPayload().getBody().size());

      System.out.println("Blocking stub: received expected unary response.");
    } catch (Exception e) {
      e.printStackTrace();
      System.exit(-1);
    }
  }

  public static void runScenarioAsyncUnary(ClientBootstrap.Transport transport,
                                           String serverHost, int serverPort) throws Exception {

    Channel channel = startClient(transport, serverHost, serverPort); 
    final TestServiceGrpc.TestService asyncStub
        = TestServiceGrpc.newStub(channel);
    try {
      System.out.println("Non-blocking stub: sending unary call...");

      StreamRecorder<SimpleResponse> recorder = StreamRecorder.create();
      asyncStub.unaryCall(UNARY_CALL_REQUEST, recorder);
      assertTrue("Call timeout", recorder.awaitCompletion(10, TimeUnit.SECONDS));
      assertEquals(1, recorder.getValues().size());
      assertEquals(10, recorder.getValues().get(0).getPayload().getBody().size());
      System.out.println("Non-blocking stub: received expected async response.");
    } catch (Exception e) {
      e.printStackTrace();
      System.exit(-1);
    }
  }

  public static void runScenarioAsyncStreamingOutput(ClientBootstrap.Transport transport,
                                                     String serverHost,
                                                     int serverPort) throws Exception {

    Channel channel = startClient(transport, serverHost, serverPort);
    final TestServiceGrpc.TestService asyncStub
        = TestServiceGrpc.newStub(channel);

    try {
      System.out.println("Non-blocking stub: sending streaming request...");

      StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
      asyncStub.streamingOutputCall(STREAMING_OUTPUT_CALL_REQUEST, recorder);
      assertTrue("Call timeout", recorder.awaitCompletion(10, TimeUnit.SECONDS));
      ImmutableList<Integer> responseSizes = ImmutableList.of(50, 100, 150, 200);
      assertEquals(responseSizes.size(), recorder.getValues().size());
      for (int ix = 0; ix < recorder.getValues().size(); ++ix) {
        StreamingOutputCallResponse response = recorder.getValues().get(ix);
        assertEquals(COMPRESSABLE, response.getPayload().getType());
        int length = response.getPayload().getBody().size();
        assertEquals("comparison failed at index " + ix, responseSizes.get(ix).intValue(), length);
        System.out.println("Non-blocking stub: received expected async response.");
      }
    } catch (Exception e) {
      e.printStackTrace();
      System.exit(-1);
    }
  }

  /**
   * The main application allowing this client to be launched from the command line. Accepts the
   * following arguments:
   * <p>
   * --transport=<HTTP|NETTY|OKHTTP|SHM_STUBBY> Identifies the concrete implementation of the
   * transport. <br>
   * --serverHost=The host of the remote server.<br>
   * --serverPort=<port number> The port of the remote server.<br>
   * --stub_type=<BLOCKING|ASYNC> The type of stub to be created.<br>
   * --call_type=<UNARY|STREAMING_OUTPUT> The type of call to be sent. <br>
   * --grpc_version=<1|2> Use gRPC v2 protocol. Default is v1. 
   */
  public static void main(String[] args) throws Exception {
    Map<String, String> argMap = parseArgs(args);
    ClientBootstrap.Transport transport = getTransport(argMap);
    StubType stubType = getStubType(argMap);
    CallType callType = getCallType(argMap);
    String serverHost = getServerHost(argMap);
    int serverPort = getPort(argMap);

    com.google.net.stubby.newtransport.AbstractStream.GRPC_V2_PROTOCOL =
        getGrpcVersion(argMap) == 2; 
    switch (stubType) {
      case BLOCKING:
        switch (callType) {
          case UNARY:
            runScenarioBlockingUnary(transport, serverHost, serverPort);
            break;
          default:
            throw new IllegalArgumentException(
                "Unsupported combo: stub type " + stubType + " call type " + callType);
        }
        break;
      case ASYNC:
        switch (callType) {
          case UNARY:
            runScenarioAsyncUnary(transport, serverHost, serverPort);
            break;
          case STREAMING_OUTPUT:
            runScenarioAsyncStreamingOutput(transport, serverHost, serverPort);
            break;
          default:
            throw new IllegalArgumentException(
                "Unsupported combo: stub type " + stubType + " call type " + callType);
        }
        break;
      default:
        throw new IllegalArgumentException("Unsupported stub type: " + stubType);
    }
    System.out.println("Test completed.");
    System.exit(0);
  }

  private static ClientBootstrap.Transport getTransport(Map<String, String> argMap) {
    String value = argMap.get(TRANSPORT_ARG.toLowerCase());
    Preconditions.checkNotNull(value, "%s argument must be provided.", TRANSPORT_ARG);
    ClientBootstrap.Transport transport = ClientBootstrap.Transport.valueOf(value.toUpperCase().trim());
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

  private static StubType getStubType(Map<String, String> argMap) {
    String value = argMap.get(STUB_TYPE_ARG.toLowerCase());
    if (value == null) {
      return StubType.BLOCKING;
    }
    StubType stubType = StubType.valueOf(value.toUpperCase().trim());
    System.out.println(STUB_TYPE_ARG + " set to: " + stubType);
    return stubType;
  }

  private static CallType getCallType(Map<String, String> argMap) {
    String value = argMap.get(MESSAGE_TYPE_ARG.toLowerCase());
    if (value == null) {
      return CallType.UNARY;
    }
    CallType callType = CallType.valueOf(value.toUpperCase().trim());
    System.out.println(MESSAGE_TYPE_ARG + " set to: " + callType);
    return callType;
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
}
