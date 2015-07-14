package io.grpc.testing.integration;

import static io.grpc.stub.ClientCalls.asyncUnaryCall;
import static io.grpc.stub.ClientCalls.asyncServerStreamingCall;
import static io.grpc.stub.ClientCalls.asyncClientStreamingCall;
import static io.grpc.stub.ClientCalls.duplexStreamingCall;
import static io.grpc.stub.ClientCalls.blockingUnaryCall;
import static io.grpc.stub.ClientCalls.blockingServerStreamingCall;
import static io.grpc.stub.ClientCalls.unaryFutureCall;
import static io.grpc.stub.ServerCalls.asyncUnaryCall;
import static io.grpc.stub.ServerCalls.asyncServerStreamingCall;
import static io.grpc.stub.ServerCalls.asyncClientStreamingCall;
import static io.grpc.stub.ServerCalls.asyncDuplexStreamingCall;

@javax.annotation.Generated("by gRPC proto compiler")
public class TestServiceGrpc {

  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
      com.google.protobuf.EmptyProtos.Empty> METHOD_EMPTY_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          "grpc.testing.TestService", "EmptyCall",
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.PARSER));
  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          "grpc.testing.TestService", "UnaryCall",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.SimpleRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.SimpleResponse.PARSER));
  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_STREAMING_OUTPUT_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING,
          "grpc.testing.TestService", "StreamingOutputCall",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallResponse.PARSER));
  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest,
      io.grpc.testing.integration.Messages.StreamingInputCallResponse> METHOD_STREAMING_INPUT_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING,
          "grpc.testing.TestService", "StreamingInputCall",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingInputCallRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingInputCallResponse.PARSER));
  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_FULL_DUPLEX_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.DUPLEX_STREAMING,
          "grpc.testing.TestService", "FullDuplexCall",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallResponse.PARSER));
  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_HALF_DUPLEX_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.DUPLEX_STREAMING,
          "grpc.testing.TestService", "HalfDuplexCall",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallResponse.PARSER));

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

  // The default service descriptor
  private static final TestServiceServiceDescriptor CONFIG =
      new TestServiceServiceDescriptor();

  @javax.annotation.concurrent.Immutable
  public static class TestServiceServiceDescriptor extends
      io.grpc.stub.AbstractServiceDescriptor<TestServiceServiceDescriptor> {
    public final io.grpc.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
        com.google.protobuf.EmptyProtos.Empty> emptyCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
        io.grpc.testing.integration.Messages.SimpleResponse> unaryCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
        io.grpc.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest,
        io.grpc.testing.integration.Messages.StreamingInputCallResponse> streamingInputCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
        io.grpc.testing.integration.Messages.StreamingOutputCallResponse> fullDuplexCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
        io.grpc.testing.integration.Messages.StreamingOutputCallResponse> halfDuplexCall;

    private TestServiceServiceDescriptor() {
      emptyCall = METHOD_EMPTY_CALL;
      unaryCall = METHOD_UNARY_CALL;
      streamingOutputCall = METHOD_STREAMING_OUTPUT_CALL;
      streamingInputCall = METHOD_STREAMING_INPUT_CALL;
      fullDuplexCall = METHOD_FULL_DUPLEX_CALL;
      halfDuplexCall = METHOD_HALF_DUPLEX_CALL;
    }

    @SuppressWarnings("unchecked")
    private TestServiceServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      emptyCall = (io.grpc.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
          com.google.protobuf.EmptyProtos.Empty>) methodMap.get(
          CONFIG.emptyCall.getFullMethodName());
      unaryCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
          io.grpc.testing.integration.Messages.SimpleResponse>) methodMap.get(
          CONFIG.unaryCall.getFullMethodName());
      streamingOutputCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
          io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.streamingOutputCall.getFullMethodName());
      streamingInputCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest,
          io.grpc.testing.integration.Messages.StreamingInputCallResponse>) methodMap.get(
          CONFIG.streamingInputCall.getFullMethodName());
      fullDuplexCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
          io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.fullDuplexCall.getFullMethodName());
      halfDuplexCall = (io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
          io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.halfDuplexCall.getFullMethodName());
    }

    @java.lang.Override
    protected TestServiceServiceDescriptor build(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      return new TestServiceServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public java.util.Collection<io.grpc.MethodDescriptor<?, ?>> methods() {
      return com.google.common.collect.ImmutableList.<io.grpc.MethodDescriptor<?, ?>>of(
          emptyCall,
          unaryCall,
          streamingOutputCall,
          streamingInputCall,
          fullDuplexCall,
          halfDuplexCall);
    }
  }

  public static interface TestService {

    public void emptyCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver);

    public void unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver);

    public void streamingOutputCall(io.grpc.testing.integration.Messages.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver);
  }

  public static interface TestServiceBlockingClient {

    public com.google.protobuf.EmptyProtos.Empty emptyCall(com.google.protobuf.EmptyProtos.Empty request);

    public io.grpc.testing.integration.Messages.SimpleResponse unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request);

    public java.util.Iterator<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.Messages.StreamingOutputCallRequest request);
  }

  public static interface TestServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> emptyCall(
        com.google.protobuf.EmptyProtos.Empty request);

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Messages.SimpleResponse> unaryCall(
        io.grpc.testing.integration.Messages.SimpleRequest request);
  }

  public static class TestServiceStub extends
      io.grpc.stub.AbstractStub<TestServiceStub, TestServiceServiceDescriptor>
      implements TestService {
    private TestServiceStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    private TestServiceStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      super(channel, config, callOptions);
    }

    @java.lang.Override
    protected TestServiceStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      return new TestServiceStub(channel, config, callOptions);
    }

    @java.lang.Override
    public void emptyCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.emptyCall, callOptions), request, responseObserver);
    }

    @java.lang.Override
    public void unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.unaryCall, callOptions), request, responseObserver);
    }

    @java.lang.Override
    public void streamingOutputCall(io.grpc.testing.integration.Messages.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      asyncServerStreamingCall(
          channel.newCall(config.streamingOutputCall, callOptions), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse> responseObserver) {
      return asyncClientStreamingCall(
          channel.newCall(config.streamingInputCall, callOptions), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.fullDuplexCall, callOptions), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.halfDuplexCall, callOptions), responseObserver);
    }
  }

  public static class TestServiceBlockingStub extends
      io.grpc.stub.AbstractStub<TestServiceBlockingStub, TestServiceServiceDescriptor>
      implements TestServiceBlockingClient {
    private TestServiceBlockingStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    private TestServiceBlockingStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      super(channel, config, callOptions);
    }

    @java.lang.Override
    protected TestServiceBlockingStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      return new TestServiceBlockingStub(channel, config, callOptions);
    }

    @java.lang.Override
    public com.google.protobuf.EmptyProtos.Empty emptyCall(com.google.protobuf.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          channel.newCall(config.emptyCall, callOptions), request);
    }

    @java.lang.Override
    public io.grpc.testing.integration.Messages.SimpleResponse unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          channel.newCall(config.unaryCall, callOptions), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.Messages.StreamingOutputCallRequest request) {
      return blockingServerStreamingCall(
          channel.newCall(config.streamingOutputCall, callOptions), request);
    }
  }

  public static class TestServiceFutureStub extends
      io.grpc.stub.AbstractStub<TestServiceFutureStub, TestServiceServiceDescriptor>
      implements TestServiceFutureClient {
    private TestServiceFutureStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    private TestServiceFutureStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      super(channel, config, callOptions);
    }

    @java.lang.Override
    protected TestServiceFutureStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      return new TestServiceFutureStub(channel, config, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> emptyCall(
        com.google.protobuf.EmptyProtos.Empty request) {
      return unaryFutureCall(
          channel.newCall(config.emptyCall, callOptions), request);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Messages.SimpleResponse> unaryCall(
        io.grpc.testing.integration.Messages.SimpleRequest request) {
      return unaryFutureCall(
          channel.newCall(config.unaryCall, callOptions), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder("grpc.testing.TestService")
      .addMethod(io.grpc.ServerMethodDefinition.create(
          METHOD_EMPTY_CALL,
          asyncUnaryCall(
            new io.grpc.stub.ServerCalls.UnaryMethod<
                com.google.protobuf.EmptyProtos.Empty,
                com.google.protobuf.EmptyProtos.Empty>() {
              @java.lang.Override
              public void invoke(
                  com.google.protobuf.EmptyProtos.Empty request,
                  io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
                serviceImpl.emptyCall(request, responseObserver);
              }
            })))
      .addMethod(io.grpc.ServerMethodDefinition.create(
          METHOD_UNARY_CALL,
          asyncUnaryCall(
            new io.grpc.stub.ServerCalls.UnaryMethod<
                io.grpc.testing.integration.Messages.SimpleRequest,
                io.grpc.testing.integration.Messages.SimpleResponse>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.testing.integration.Messages.SimpleRequest request,
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver) {
                serviceImpl.unaryCall(request, responseObserver);
              }
            })))
      .addMethod(io.grpc.ServerMethodDefinition.create(
          METHOD_STREAMING_OUTPUT_CALL,
          asyncServerStreamingCall(
            new io.grpc.stub.ServerCalls.ServerStreamingMethod<
                io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
                io.grpc.testing.integration.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.testing.integration.Messages.StreamingOutputCallRequest request,
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
                serviceImpl.streamingOutputCall(request, responseObserver);
              }
            })))
      .addMethod(io.grpc.ServerMethodDefinition.create(
          METHOD_STREAMING_INPUT_CALL,
          asyncClientStreamingCall(
            new io.grpc.stub.ServerCalls.ClientStreamingMethod<
                io.grpc.testing.integration.Messages.StreamingInputCallRequest,
                io.grpc.testing.integration.Messages.StreamingInputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse> responseObserver) {
                return serviceImpl.streamingInputCall(responseObserver);
              }
            })))
      .addMethod(io.grpc.ServerMethodDefinition.create(
          METHOD_FULL_DUPLEX_CALL,
          asyncDuplexStreamingCall(
            new io.grpc.stub.ServerCalls.DuplexStreamingMethod<
                io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
                io.grpc.testing.integration.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
                return serviceImpl.fullDuplexCall(responseObserver);
              }
            })))
      .addMethod(io.grpc.ServerMethodDefinition.create(
          METHOD_HALF_DUPLEX_CALL,
          asyncDuplexStreamingCall(
            new io.grpc.stub.ServerCalls.DuplexStreamingMethod<
                io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
                io.grpc.testing.integration.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
                return serviceImpl.halfDuplexCall(responseObserver);
              }
            }))).build();
  }
}
