package io.grpc.testing.integration;

import static io.grpc.stub.Calls.createMethodDescriptor;
import static io.grpc.stub.Calls.asyncUnaryCall;
import static io.grpc.stub.Calls.asyncServerStreamingCall;
import static io.grpc.stub.Calls.asyncClientStreamingCall;
import static io.grpc.stub.Calls.duplexStreamingCall;
import static io.grpc.stub.Calls.blockingUnaryCall;
import static io.grpc.stub.Calls.blockingServerStreamingCall;
import static io.grpc.stub.Calls.unaryFutureCall;
import static io.grpc.stub.ServerCalls.createMethodDefinition;
import static io.grpc.stub.ServerCalls.asyncUnaryRequestCall;
import static io.grpc.stub.ServerCalls.asyncStreamingRequestCall;

@javax.annotation.Generated("by gRPC proto compiler")
public class TestServiceGrpc {

  private static final io.grpc.stub.Method<io.grpc.testing.integration.Test.SimpleRequest,
      io.grpc.testing.integration.Test.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.UNARY, "UnaryCall",
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.SimpleRequest.PARSER),
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.SimpleResponse.PARSER));
  private static final io.grpc.stub.Method<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
      io.grpc.testing.integration.Test.StreamingOutputCallResponse> METHOD_STREAMING_OUTPUT_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.SERVER_STREAMING, "StreamingOutputCall",
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.StreamingOutputCallRequest.PARSER),
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.StreamingOutputCallResponse.PARSER));
  private static final io.grpc.stub.Method<io.grpc.testing.integration.Test.StreamingInputCallRequest,
      io.grpc.testing.integration.Test.StreamingInputCallResponse> METHOD_STREAMING_INPUT_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.CLIENT_STREAMING, "StreamingInputCall",
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.StreamingInputCallRequest.PARSER),
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.StreamingInputCallResponse.PARSER));
  private static final io.grpc.stub.Method<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
      io.grpc.testing.integration.Test.StreamingOutputCallResponse> METHOD_FULL_DUPLEX_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "FullDuplexCall",
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.StreamingOutputCallRequest.PARSER),
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.StreamingOutputCallResponse.PARSER));
  private static final io.grpc.stub.Method<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
      io.grpc.testing.integration.Test.StreamingOutputCallResponse> METHOD_HALF_DUPLEX_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "HalfDuplexCall",
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.StreamingOutputCallRequest.PARSER),
          io.grpc.proto.ProtoUtils.marshaller(io.grpc.testing.integration.Test.StreamingOutputCallResponse.PARSER));

  public static TestServiceStub newStub(io.grpc.Channel channel) {
    return new TestServiceStub(channel, CONFIG);
  }

  public static TestServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new TestServiceBlockingStub(channel, CONFIG);
  }

  public static TestServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new TestServiceFutureStub(channel, CONFIG);
  }

  public static final TestServiceServiceDescriptor CONFIG =
      new TestServiceServiceDescriptor();

  @javax.annotation.concurrent.Immutable
  public static class TestServiceServiceDescriptor extends
      io.grpc.stub.AbstractServiceDescriptor<TestServiceServiceDescriptor> {
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.SimpleRequest,
        io.grpc.testing.integration.Test.SimpleResponse> unaryCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
        io.grpc.testing.integration.Test.StreamingOutputCallResponse> streamingOutputCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.StreamingInputCallRequest,
        io.grpc.testing.integration.Test.StreamingInputCallResponse> streamingInputCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
        io.grpc.testing.integration.Test.StreamingOutputCallResponse> fullDuplexCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
        io.grpc.testing.integration.Test.StreamingOutputCallResponse> halfDuplexCall;

    private TestServiceServiceDescriptor() {
      unaryCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_UNARY_CALL);
      streamingOutputCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_STREAMING_OUTPUT_CALL);
      streamingInputCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_STREAMING_INPUT_CALL);
      fullDuplexCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_FULL_DUPLEX_CALL);
      halfDuplexCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_HALF_DUPLEX_CALL);
    }

    @SuppressWarnings("unchecked")
    private TestServiceServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      unaryCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.SimpleRequest,
          io.grpc.testing.integration.Test.SimpleResponse>) methodMap.get(
          CONFIG.unaryCall.getName());
      streamingOutputCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
          io.grpc.testing.integration.Test.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.streamingOutputCall.getName());
      streamingInputCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.StreamingInputCallRequest,
          io.grpc.testing.integration.Test.StreamingInputCallResponse>) methodMap.get(
          CONFIG.streamingInputCall.getName());
      fullDuplexCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
          io.grpc.testing.integration.Test.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.fullDuplexCall.getName());
      halfDuplexCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Test.StreamingOutputCallRequest,
          io.grpc.testing.integration.Test.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.halfDuplexCall.getName());
    }

    @java.lang.Override
    protected TestServiceServiceDescriptor build(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      return new TestServiceServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public com.google.common.collect.ImmutableList<io.grpc.MethodDescriptor<?, ?>> methods() {
      return com.google.common.collect.ImmutableList.<io.grpc.MethodDescriptor<?, ?>>of(
          unaryCall,
          streamingOutputCall,
          streamingInputCall,
          fullDuplexCall,
          halfDuplexCall);
    }
  }

  public static interface TestService {

    public void unaryCall(io.grpc.testing.integration.Test.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.SimpleResponse> responseObserver);

    public void streamingOutputCall(io.grpc.testing.integration.Test.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingInputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver);
  }

  public static interface TestServiceBlockingClient {

    public io.grpc.testing.integration.Test.SimpleResponse unaryCall(io.grpc.testing.integration.Test.SimpleRequest request);

    public java.util.Iterator<io.grpc.testing.integration.Test.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.Test.StreamingOutputCallRequest request);
  }

  public static interface TestServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Test.SimpleResponse> unaryCall(
        io.grpc.testing.integration.Test.SimpleRequest request);
  }

  public static class TestServiceStub extends
      io.grpc.stub.AbstractStub<TestServiceStub, TestServiceServiceDescriptor>
      implements TestService {
    private TestServiceStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceStub(channel, config);
    }

    @java.lang.Override
    public void unaryCall(io.grpc.testing.integration.Test.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.unaryCall), request, responseObserver);
    }

    @java.lang.Override
    public void streamingOutputCall(io.grpc.testing.integration.Test.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver) {
      asyncServerStreamingCall(
          channel.newCall(config.streamingOutputCall), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingInputCallResponse> responseObserver) {
      return asyncClientStreamingCall(
          channel.newCall(config.streamingInputCall), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.fullDuplexCall), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.halfDuplexCall), responseObserver);
    }
  }

  public static class TestServiceBlockingStub extends
      io.grpc.stub.AbstractStub<TestServiceBlockingStub, TestServiceServiceDescriptor>
      implements TestServiceBlockingClient {
    private TestServiceBlockingStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceBlockingStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceBlockingStub(channel, config);
    }

    @java.lang.Override
    public io.grpc.testing.integration.Test.SimpleResponse unaryCall(io.grpc.testing.integration.Test.SimpleRequest request) {
      return blockingUnaryCall(
          channel.newCall(config.unaryCall), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.testing.integration.Test.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.Test.StreamingOutputCallRequest request) {
      return blockingServerStreamingCall(
          channel.newCall(config.streamingOutputCall), request);
    }
  }

  public static class TestServiceFutureStub extends
      io.grpc.stub.AbstractStub<TestServiceFutureStub, TestServiceServiceDescriptor>
      implements TestServiceFutureClient {
    private TestServiceFutureStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceFutureStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceFutureStub(channel, config);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Test.SimpleResponse> unaryCall(
        io.grpc.testing.integration.Test.SimpleRequest request) {
      return unaryFutureCall(
          channel.newCall(config.unaryCall), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder("grpc.testing.TestService")
      .addMethod(createMethodDefinition(
          METHOD_UNARY_CALL,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.testing.integration.Test.SimpleRequest,
                io.grpc.testing.integration.Test.SimpleResponse>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.testing.integration.Test.SimpleRequest request,
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.SimpleResponse> responseObserver) {
                serviceImpl.unaryCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_STREAMING_OUTPUT_CALL,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.testing.integration.Test.StreamingOutputCallRequest,
                io.grpc.testing.integration.Test.StreamingOutputCallResponse>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.testing.integration.Test.StreamingOutputCallRequest request,
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver) {
                serviceImpl.streamingOutputCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_STREAMING_INPUT_CALL,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                io.grpc.testing.integration.Test.StreamingInputCallRequest,
                io.grpc.testing.integration.Test.StreamingInputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingInputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingInputCallResponse> responseObserver) {
                return serviceImpl.streamingInputCall(responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_FULL_DUPLEX_CALL,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                io.grpc.testing.integration.Test.StreamingOutputCallRequest,
                io.grpc.testing.integration.Test.StreamingOutputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver) {
                return serviceImpl.fullDuplexCall(responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_HALF_DUPLEX_CALL,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                io.grpc.testing.integration.Test.StreamingOutputCallRequest,
                io.grpc.testing.integration.Test.StreamingOutputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Test.StreamingOutputCallResponse> responseObserver) {
                return serviceImpl.halfDuplexCall(responseObserver);
              }
            }))).build();
  }
}
