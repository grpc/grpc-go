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
import static io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall;
import static io.grpc.stub.ServerCalls.asyncUnimplementedStreamingCall;

@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: io/grpc/testing/integration/test.proto")
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

  public static abstract class AbstractTestService implements TestService, io.grpc.BindableService {

    @java.lang.Override
    public void emptyCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_EMPTY_CALL, responseObserver);
    }

    @java.lang.Override
    public void unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_UNARY_CALL, responseObserver);
    }

    @java.lang.Override
    public void streamingOutputCall(io.grpc.testing.integration.Messages.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_STREAMING_OUTPUT_CALL, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_STREAMING_INPUT_CALL, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_FULL_DUPLEX_CALL, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_HALF_DUPLEX_CALL, responseObserver);
    }

    @java.lang.Override public io.grpc.ServerServiceDefinition bindService() {
      return TestServiceGrpc.bindService(this);
    }
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
          getChannel(), METHOD_EMPTY_CALL, getCallOptions(), request);
    }

    @java.lang.Override
    public io.grpc.testing.integration.Messages.SimpleResponse unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel(), METHOD_UNARY_CALL, getCallOptions(), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.Messages.StreamingOutputCallRequest request) {
      return blockingServerStreamingCall(
          getChannel(), METHOD_STREAMING_OUTPUT_CALL, getCallOptions(), request);
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

  private static final int METHODID_EMPTY_CALL = 0;
  private static final int METHODID_UNARY_CALL = 1;
  private static final int METHODID_STREAMING_OUTPUT_CALL = 2;
  private static final int METHODID_STREAMING_INPUT_CALL = 3;
  private static final int METHODID_FULL_DUPLEX_CALL = 4;
  private static final int METHODID_HALF_DUPLEX_CALL = 5;

  private static class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final TestService serviceImpl;
    private final int methodId;

    public MethodHandlers(TestService serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_EMPTY_CALL:
          serviceImpl.emptyCall((com.google.protobuf.EmptyProtos.Empty) request,
              (io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty>) responseObserver);
          break;
        case METHODID_UNARY_CALL:
          serviceImpl.unaryCall((io.grpc.testing.integration.Messages.SimpleRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse>) responseObserver);
          break;
        case METHODID_STREAMING_OUTPUT_CALL:
          serviceImpl.streamingOutputCall((io.grpc.testing.integration.Messages.StreamingOutputCallRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) responseObserver);
          break;
        default:
          throw new AssertionError();
      }
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public io.grpc.stub.StreamObserver<Req> invoke(
        io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_STREAMING_INPUT_CALL:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.streamingInputCall(
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse>) responseObserver);
        case METHODID_FULL_DUPLEX_CALL:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.fullDuplexCall(
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) responseObserver);
        case METHODID_HALF_DUPLEX_CALL:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.halfDuplexCall(
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
        .addMethod(
          METHOD_EMPTY_CALL,
          asyncUnaryCall(
            new MethodHandlers<
              com.google.protobuf.EmptyProtos.Empty,
              com.google.protobuf.EmptyProtos.Empty>(
                serviceImpl, METHODID_EMPTY_CALL)))
        .addMethod(
          METHOD_UNARY_CALL,
          asyncUnaryCall(
            new MethodHandlers<
              io.grpc.testing.integration.Messages.SimpleRequest,
              io.grpc.testing.integration.Messages.SimpleResponse>(
                serviceImpl, METHODID_UNARY_CALL)))
        .addMethod(
          METHOD_STREAMING_OUTPUT_CALL,
          asyncServerStreamingCall(
            new MethodHandlers<
              io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
              io.grpc.testing.integration.Messages.StreamingOutputCallResponse>(
                serviceImpl, METHODID_STREAMING_OUTPUT_CALL)))
        .addMethod(
          METHOD_STREAMING_INPUT_CALL,
          asyncClientStreamingCall(
            new MethodHandlers<
              io.grpc.testing.integration.Messages.StreamingInputCallRequest,
              io.grpc.testing.integration.Messages.StreamingInputCallResponse>(
                serviceImpl, METHODID_STREAMING_INPUT_CALL)))
        .addMethod(
          METHOD_FULL_DUPLEX_CALL,
          asyncBidiStreamingCall(
            new MethodHandlers<
              io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
              io.grpc.testing.integration.Messages.StreamingOutputCallResponse>(
                serviceImpl, METHODID_FULL_DUPLEX_CALL)))
        .addMethod(
          METHOD_HALF_DUPLEX_CALL,
          asyncBidiStreamingCall(
            new MethodHandlers<
              io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
              io.grpc.testing.integration.Messages.StreamingOutputCallResponse>(
                serviceImpl, METHODID_HALF_DUPLEX_CALL)))
        .build();
  }
}
