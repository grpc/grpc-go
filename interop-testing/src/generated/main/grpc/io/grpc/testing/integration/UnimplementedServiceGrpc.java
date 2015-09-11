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
public class UnimplementedServiceGrpc {

  private UnimplementedServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.UnimplementedService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
      com.google.protobuf.EmptyProtos.Empty> METHOD_UNIMPLEMENTED_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.UnimplementedService", "UnimplementedCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()));

  public static UnimplementedServiceStub newStub(io.grpc.Channel channel) {
    return new UnimplementedServiceStub(channel);
  }

  public static UnimplementedServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new UnimplementedServiceBlockingStub(channel);
  }

  public static UnimplementedServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new UnimplementedServiceFutureStub(channel);
  }

  public static interface UnimplementedService {

    public void unimplementedCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver);
  }

  public static interface UnimplementedServiceBlockingClient {

    public com.google.protobuf.EmptyProtos.Empty unimplementedCall(com.google.protobuf.EmptyProtos.Empty request);
  }

  public static interface UnimplementedServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> unimplementedCall(
        com.google.protobuf.EmptyProtos.Empty request);
  }

  public static class UnimplementedServiceStub extends io.grpc.stub.AbstractStub<UnimplementedServiceStub>
      implements UnimplementedService {
    private UnimplementedServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private UnimplementedServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected UnimplementedServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new UnimplementedServiceStub(channel, callOptions);
    }

    @java.lang.Override
    public void unimplementedCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_UNIMPLEMENTED_CALL, getCallOptions()), request, responseObserver);
    }
  }

  public static class UnimplementedServiceBlockingStub extends io.grpc.stub.AbstractStub<UnimplementedServiceBlockingStub>
      implements UnimplementedServiceBlockingClient {
    private UnimplementedServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private UnimplementedServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected UnimplementedServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new UnimplementedServiceBlockingStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.protobuf.EmptyProtos.Empty unimplementedCall(com.google.protobuf.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          getChannel().newCall(METHOD_UNIMPLEMENTED_CALL, getCallOptions()), request);
    }
  }

  public static class UnimplementedServiceFutureStub extends io.grpc.stub.AbstractStub<UnimplementedServiceFutureStub>
      implements UnimplementedServiceFutureClient {
    private UnimplementedServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private UnimplementedServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected UnimplementedServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new UnimplementedServiceFutureStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> unimplementedCall(
        com.google.protobuf.EmptyProtos.Empty request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_UNIMPLEMENTED_CALL, getCallOptions()), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final UnimplementedService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
      .addMethod(
        METHOD_UNIMPLEMENTED_CALL,
        asyncUnaryCall(
          new io.grpc.stub.ServerCalls.UnaryMethod<
              com.google.protobuf.EmptyProtos.Empty,
              com.google.protobuf.EmptyProtos.Empty>() {
            @java.lang.Override
            public void invoke(
                com.google.protobuf.EmptyProtos.Empty request,
                io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
              serviceImpl.unimplementedCall(request, responseObserver);
            }
          })).build();
  }
}
