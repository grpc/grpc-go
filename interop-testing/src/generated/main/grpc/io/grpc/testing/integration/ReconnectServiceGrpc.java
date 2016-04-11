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
    value = "by gRPC proto compiler (version 0.14.0-SNAPSHOT)",
    comments = "Source: io/grpc/testing/integration/test.proto")
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

  public static abstract class AbstractReconnectService implements ReconnectService, io.grpc.BindableService {

    @java.lang.Override
    public void start(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_START, responseObserver);
    }

    @java.lang.Override
    public void stop(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.ReconnectInfo> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_STOP, responseObserver);
    }

    @java.lang.Override public io.grpc.ServerServiceDefinition bindService() {
      return ReconnectServiceGrpc.bindService(this);
    }
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
          getChannel(), METHOD_START, getCallOptions(), request);
    }

    @java.lang.Override
    public io.grpc.testing.integration.Messages.ReconnectInfo stop(com.google.protobuf.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          getChannel(), METHOD_STOP, getCallOptions(), request);
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

  private static final int METHODID_START = 0;
  private static final int METHODID_STOP = 1;

  private static class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final ReconnectService serviceImpl;
    private final int methodId;

    public MethodHandlers(ReconnectService serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_START:
          serviceImpl.start((com.google.protobuf.EmptyProtos.Empty) request,
              (io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty>) responseObserver);
          break;
        case METHODID_STOP:
          serviceImpl.stop((com.google.protobuf.EmptyProtos.Empty) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.ReconnectInfo>) responseObserver);
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
        default:
          throw new AssertionError();
      }
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final ReconnectService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
        .addMethod(
          METHOD_START,
          asyncUnaryCall(
            new MethodHandlers<
              com.google.protobuf.EmptyProtos.Empty,
              com.google.protobuf.EmptyProtos.Empty>(
                serviceImpl, METHODID_START)))
        .addMethod(
          METHOD_STOP,
          asyncUnaryCall(
            new MethodHandlers<
              com.google.protobuf.EmptyProtos.Empty,
              io.grpc.testing.integration.Messages.ReconnectInfo>(
                serviceImpl, METHODID_STOP)))
        .build();
  }
}
