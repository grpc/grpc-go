package io.grpc.testing.integration;

import static io.grpc.stub.ClientCalls.asyncUnaryCall;
import static io.grpc.stub.ClientCalls.asyncServerStreamingCall;
import static io.grpc.stub.ClientCalls.asyncClientStreamingCall;
import static io.grpc.stub.ClientCalls.asyncBidiStreamingCall;
import static io.grpc.stub.ClientCalls.blockingUnaryCall;
import static io.grpc.stub.ClientCalls.blockingServerStreamingCall;
import static io.grpc.stub.ClientCalls.futureUnaryCall;
import static io.grpc.MethodDescriptor.generateFullMethodName;
import static io.grpc.stub.ServerCalls.asyncUnaryCall;
import static io.grpc.stub.ServerCalls.asyncServerStreamingCall;
import static io.grpc.stub.ServerCalls.asyncClientStreamingCall;
import static io.grpc.stub.ServerCalls.asyncBidiStreamingCall;

@javax.annotation.Generated("by gRPC proto compiler")
public class TestServiceGrpc {

  private TestServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.TestService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
      com.google.protobuf.EmptyProtos.Empty> METHOD_EMPTY_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.TestService", "EmptyCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.TestService", "UnaryCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.SimpleRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.SimpleResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_STREAMING_OUTPUT_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "StreamingOutputCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest,
      io.grpc.testing.integration.Messages.StreamingInputCallResponse> METHOD_STREAMING_INPUT_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "StreamingInputCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingInputCallRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingInputCallResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_FULL_DUPLEX_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "FullDuplexCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_HALF_DUPLEX_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "HalfDuplexCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.StreamingOutputCallResponse.getDefaultInstance()));

  public static TestServiceStub newStub(io.grpc.Channel channel) {
    return new TestServiceStub(channel);
  }

  public static TestServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new TestServiceBlockingStub(channel);
  }

  public static TestServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new TestServiceFutureStub(channel);
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

  public static class TestServiceStub extends io.grpc.stub.AbstractStub<TestServiceStub>
      implements TestService {
    private TestServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private TestServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected TestServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new TestServiceStub(channel, callOptions);
    }

    @java.lang.Override
    public void emptyCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_EMPTY_CALL, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public void unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public void streamingOutputCall(io.grpc.testing.integration.Messages.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(METHOD_STREAMING_OUTPUT_CALL, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse> responseObserver) {
      return asyncClientStreamingCall(
          getChannel().newCall(METHOD_STREAMING_INPUT_CALL, getCallOptions()), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_FULL_DUPLEX_CALL, getCallOptions()), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_HALF_DUPLEX_CALL, getCallOptions()), responseObserver);
    }
  }

  public static class TestServiceBlockingStub extends io.grpc.stub.AbstractStub<TestServiceBlockingStub>
      implements TestServiceBlockingClient {
    private TestServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private TestServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected TestServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new TestServiceBlockingStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.protobuf.EmptyProtos.Empty emptyCall(com.google.protobuf.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          getChannel().newCall(METHOD_EMPTY_CALL, getCallOptions()), request);
    }

    @java.lang.Override
    public io.grpc.testing.integration.Messages.SimpleResponse unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.Messages.StreamingOutputCallRequest request) {
      return blockingServerStreamingCall(
          getChannel().newCall(METHOD_STREAMING_OUTPUT_CALL, getCallOptions()), request);
    }
  }

  public static class TestServiceFutureStub extends io.grpc.stub.AbstractStub<TestServiceFutureStub>
      implements TestServiceFutureClient {
    private TestServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private TestServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected TestServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new TestServiceFutureStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> emptyCall(
        com.google.protobuf.EmptyProtos.Empty request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_EMPTY_CALL, getCallOptions()), request);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Messages.SimpleResponse> unaryCall(
        io.grpc.testing.integration.Messages.SimpleRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
      .addMethod(
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
          }))
      .addMethod(
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
          }))
      .addMethod(
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
          }))
      .addMethod(
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
          }))
      .addMethod(
        METHOD_FULL_DUPLEX_CALL,
        asyncBidiStreamingCall(
          new io.grpc.stub.ServerCalls.BidiStreamingMethod<
              io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
              io.grpc.testing.integration.Messages.StreamingOutputCallResponse>() {
            @java.lang.Override
            public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> invoke(
                io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
              return serviceImpl.fullDuplexCall(responseObserver);
            }
          }))
      .addMethod(
        METHOD_HALF_DUPLEX_CALL,
        asyncBidiStreamingCall(
          new io.grpc.stub.ServerCalls.BidiStreamingMethod<
              io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
              io.grpc.testing.integration.Messages.StreamingOutputCallResponse>() {
            @java.lang.Override
            public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> invoke(
                io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
              return serviceImpl.halfDuplexCall(responseObserver);
            }
          })).build();
  }
}
