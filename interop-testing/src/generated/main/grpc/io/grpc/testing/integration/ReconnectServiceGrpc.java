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
public class ReconnectServiceGrpc {

  private ReconnectServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.ReconnectService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
      com.google.protobuf.EmptyProtos.Empty> METHOD_START =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.ReconnectService", "Start"),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
      io.grpc.testing.integration.Messages.ReconnectInfo> METHOD_STOP =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.ReconnectService", "Stop"),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Messages.ReconnectInfo.getDefaultInstance()));

  public static ReconnectServiceStub newStub(io.grpc.Channel channel) {
    return new ReconnectServiceStub(channel);
  }

  public static ReconnectServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new ReconnectServiceBlockingStub(channel);
  }

  public static ReconnectServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new ReconnectServiceFutureStub(channel);
  }

  public static interface ReconnectService {

    public void start(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver);

    public void stop(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.ReconnectInfo> responseObserver);
  }

  public static interface ReconnectServiceBlockingClient {

    public com.google.protobuf.EmptyProtos.Empty start(com.google.protobuf.EmptyProtos.Empty request);

    public io.grpc.testing.integration.Messages.ReconnectInfo stop(com.google.protobuf.EmptyProtos.Empty request);
  }

  public static interface ReconnectServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> start(
        com.google.protobuf.EmptyProtos.Empty request);

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Messages.ReconnectInfo> stop(
        com.google.protobuf.EmptyProtos.Empty request);
  }

  public static class ReconnectServiceStub extends io.grpc.stub.AbstractStub<ReconnectServiceStub>
      implements ReconnectService {
    private ReconnectServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReconnectServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReconnectServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReconnectServiceStub(channel, callOptions);
    }

    @java.lang.Override
    public void start(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_START, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public void stop(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.ReconnectInfo> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_STOP, getCallOptions()), request, responseObserver);
    }
  }

  public static class ReconnectServiceBlockingStub extends io.grpc.stub.AbstractStub<ReconnectServiceBlockingStub>
      implements ReconnectServiceBlockingClient {
    private ReconnectServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReconnectServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReconnectServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReconnectServiceBlockingStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.protobuf.EmptyProtos.Empty start(com.google.protobuf.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          getChannel().newCall(METHOD_START, getCallOptions()), request);
    }

    @java.lang.Override
    public io.grpc.testing.integration.Messages.ReconnectInfo stop(com.google.protobuf.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          getChannel().newCall(METHOD_STOP, getCallOptions()), request);
    }
  }

  public static class ReconnectServiceFutureStub extends io.grpc.stub.AbstractStub<ReconnectServiceFutureStub>
      implements ReconnectServiceFutureClient {
    private ReconnectServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReconnectServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReconnectServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReconnectServiceFutureStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> start(
        com.google.protobuf.EmptyProtos.Empty request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_START, getCallOptions()), request);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Messages.ReconnectInfo> stop(
        com.google.protobuf.EmptyProtos.Empty request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_STOP, getCallOptions()), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final ReconnectService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
      .addMethod(
        METHOD_START,
        asyncUnaryCall(
          new io.grpc.stub.ServerCalls.UnaryMethod<
              com.google.protobuf.EmptyProtos.Empty,
              com.google.protobuf.EmptyProtos.Empty>() {
            @java.lang.Override
            public void invoke(
                com.google.protobuf.EmptyProtos.Empty request,
                io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
              serviceImpl.start(request, responseObserver);
            }
          }))
      .addMethod(
        METHOD_STOP,
        asyncUnaryCall(
          new io.grpc.stub.ServerCalls.UnaryMethod<
              com.google.protobuf.EmptyProtos.Empty,
              io.grpc.testing.integration.Messages.ReconnectInfo>() {
            @java.lang.Override
            public void invoke(
                com.google.protobuf.EmptyProtos.Empty request,
                io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.ReconnectInfo> responseObserver) {
              serviceImpl.stop(request, responseObserver);
            }
          })).build();
  }
}
