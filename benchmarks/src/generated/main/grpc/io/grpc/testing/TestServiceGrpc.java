package io.grpc.testing;

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
  public static final io.grpc.MethodDescriptor<io.grpc.testing.SimpleRequest,
      io.grpc.testing.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.TestService", "UnaryCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.SimpleRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.SimpleResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.SimpleRequest,
      io.grpc.testing.SimpleResponse> METHOD_STREAMING_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "StreamingCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.SimpleRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.SimpleResponse.getDefaultInstance()));

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

    public void unaryCall(io.grpc.testing.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver);
  }

  public static interface TestServiceBlockingClient {

    public io.grpc.testing.SimpleResponse unaryCall(io.grpc.testing.SimpleRequest request);
  }

  public static interface TestServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.SimpleResponse> unaryCall(
        io.grpc.testing.SimpleRequest request);
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
    public void unaryCall(io.grpc.testing.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_STREAMING_CALL, getCallOptions()), responseObserver);
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
    public io.grpc.testing.SimpleResponse unaryCall(io.grpc.testing.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request);
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
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.SimpleResponse> unaryCall(
        io.grpc.testing.SimpleRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
      .addMethod(
        METHOD_UNARY_CALL,
        asyncUnaryCall(
          new io.grpc.stub.ServerCalls.UnaryMethod<
              io.grpc.testing.SimpleRequest,
              io.grpc.testing.SimpleResponse>() {
            @java.lang.Override
            public void invoke(
                io.grpc.testing.SimpleRequest request,
                io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver) {
              serviceImpl.unaryCall(request, responseObserver);
            }
          }))
      .addMethod(
        METHOD_STREAMING_CALL,
        asyncBidiStreamingCall(
          new io.grpc.stub.ServerCalls.BidiStreamingMethod<
              io.grpc.testing.SimpleRequest,
              io.grpc.testing.SimpleResponse>() {
            @java.lang.Override
            public io.grpc.stub.StreamObserver<io.grpc.testing.SimpleRequest> invoke(
                io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver) {
              return serviceImpl.streamingCall(responseObserver);
            }
          })).build();
  }
}
