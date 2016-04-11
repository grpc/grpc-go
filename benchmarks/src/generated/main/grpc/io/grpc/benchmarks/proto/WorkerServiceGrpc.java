package io.grpc.benchmarks.proto;

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
    comments = "Source: services.proto")
public class WorkerServiceGrpc {

  private WorkerServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.WorkerService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.ServerArgs,
      io.grpc.benchmarks.proto.Control.ServerStatus> METHOD_RUN_SERVER =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.WorkerService", "RunServer"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.ServerArgs.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.ServerStatus.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.ClientArgs,
      io.grpc.benchmarks.proto.Control.ClientStatus> METHOD_RUN_CLIENT =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.WorkerService", "RunClient"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.ClientArgs.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.ClientStatus.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.CoreRequest,
      io.grpc.benchmarks.proto.Control.CoreResponse> METHOD_CORE_COUNT =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.WorkerService", "CoreCount"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.CoreRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.CoreResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.Void,
      io.grpc.benchmarks.proto.Control.Void> METHOD_QUIT_WORKER =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.WorkerService", "QuitWorker"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.Void.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.Void.getDefaultInstance()));

  public static WorkerServiceStub newStub(io.grpc.Channel channel) {
    return new WorkerServiceStub(channel);
  }

  public static WorkerServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new WorkerServiceBlockingStub(channel);
  }

  public static WorkerServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new WorkerServiceFutureStub(channel);
  }

  public static interface WorkerService {

    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerArgs> runServer(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerStatus> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientArgs> runClient(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientStatus> responseObserver);

    public void coreCount(io.grpc.benchmarks.proto.Control.CoreRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.CoreResponse> responseObserver);

    public void quitWorker(io.grpc.benchmarks.proto.Control.Void request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.Void> responseObserver);
  }

  public static abstract class AbstractWorkerService implements WorkerService, io.grpc.BindableService {

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerArgs> runServer(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerStatus> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_RUN_SERVER, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientArgs> runClient(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientStatus> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_RUN_CLIENT, responseObserver);
    }

    @java.lang.Override
    public void coreCount(io.grpc.benchmarks.proto.Control.CoreRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.CoreResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_CORE_COUNT, responseObserver);
    }

    @java.lang.Override
    public void quitWorker(io.grpc.benchmarks.proto.Control.Void request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.Void> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_QUIT_WORKER, responseObserver);
    }

    @java.lang.Override public io.grpc.ServerServiceDefinition bindService() {
      return WorkerServiceGrpc.bindService(this);
    }
  }

  public static interface WorkerServiceBlockingClient {

    public io.grpc.benchmarks.proto.Control.CoreResponse coreCount(io.grpc.benchmarks.proto.Control.CoreRequest request);

    public io.grpc.benchmarks.proto.Control.Void quitWorker(io.grpc.benchmarks.proto.Control.Void request);
  }

  public static interface WorkerServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Control.CoreResponse> coreCount(
        io.grpc.benchmarks.proto.Control.CoreRequest request);

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Control.Void> quitWorker(
        io.grpc.benchmarks.proto.Control.Void request);
  }

  public static class WorkerServiceStub extends io.grpc.stub.AbstractStub<WorkerServiceStub>
      implements WorkerService {
    private WorkerServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private WorkerServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected WorkerServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new WorkerServiceStub(channel, callOptions);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerArgs> runServer(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerStatus> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_RUN_SERVER, getCallOptions()), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientArgs> runClient(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientStatus> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_RUN_CLIENT, getCallOptions()), responseObserver);
    }

    @java.lang.Override
    public void coreCount(io.grpc.benchmarks.proto.Control.CoreRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.CoreResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_CORE_COUNT, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public void quitWorker(io.grpc.benchmarks.proto.Control.Void request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.Void> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_QUIT_WORKER, getCallOptions()), request, responseObserver);
    }
  }

  public static class WorkerServiceBlockingStub extends io.grpc.stub.AbstractStub<WorkerServiceBlockingStub>
      implements WorkerServiceBlockingClient {
    private WorkerServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private WorkerServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected WorkerServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new WorkerServiceBlockingStub(channel, callOptions);
    }

    @java.lang.Override
    public io.grpc.benchmarks.proto.Control.CoreResponse coreCount(io.grpc.benchmarks.proto.Control.CoreRequest request) {
      return blockingUnaryCall(
          getChannel(), METHOD_CORE_COUNT, getCallOptions(), request);
    }

    @java.lang.Override
    public io.grpc.benchmarks.proto.Control.Void quitWorker(io.grpc.benchmarks.proto.Control.Void request) {
      return blockingUnaryCall(
          getChannel(), METHOD_QUIT_WORKER, getCallOptions(), request);
    }
  }

  public static class WorkerServiceFutureStub extends io.grpc.stub.AbstractStub<WorkerServiceFutureStub>
      implements WorkerServiceFutureClient {
    private WorkerServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private WorkerServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected WorkerServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new WorkerServiceFutureStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Control.CoreResponse> coreCount(
        io.grpc.benchmarks.proto.Control.CoreRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_CORE_COUNT, getCallOptions()), request);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Control.Void> quitWorker(
        io.grpc.benchmarks.proto.Control.Void request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_QUIT_WORKER, getCallOptions()), request);
    }
  }

  private static final int METHODID_CORE_COUNT = 0;
  private static final int METHODID_QUIT_WORKER = 1;
  private static final int METHODID_RUN_SERVER = 2;
  private static final int METHODID_RUN_CLIENT = 3;

  private static class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final WorkerService serviceImpl;
    private final int methodId;

    public MethodHandlers(WorkerService serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_CORE_COUNT:
          serviceImpl.coreCount((io.grpc.benchmarks.proto.Control.CoreRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.CoreResponse>) responseObserver);
          break;
        case METHODID_QUIT_WORKER:
          serviceImpl.quitWorker((io.grpc.benchmarks.proto.Control.Void) request,
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.Void>) responseObserver);
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
        case METHODID_RUN_SERVER:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.runServer(
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerStatus>) responseObserver);
        case METHODID_RUN_CLIENT:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.runClient(
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientStatus>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final WorkerService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
        .addMethod(
          METHOD_RUN_SERVER,
          asyncBidiStreamingCall(
            new MethodHandlers<
              io.grpc.benchmarks.proto.Control.ServerArgs,
              io.grpc.benchmarks.proto.Control.ServerStatus>(
                serviceImpl, METHODID_RUN_SERVER)))
        .addMethod(
          METHOD_RUN_CLIENT,
          asyncBidiStreamingCall(
            new MethodHandlers<
              io.grpc.benchmarks.proto.Control.ClientArgs,
              io.grpc.benchmarks.proto.Control.ClientStatus>(
                serviceImpl, METHODID_RUN_CLIENT)))
        .addMethod(
          METHOD_CORE_COUNT,
          asyncUnaryCall(
            new MethodHandlers<
              io.grpc.benchmarks.proto.Control.CoreRequest,
              io.grpc.benchmarks.proto.Control.CoreResponse>(
                serviceImpl, METHODID_CORE_COUNT)))
        .addMethod(
          METHOD_QUIT_WORKER,
          asyncUnaryCall(
            new MethodHandlers<
              io.grpc.benchmarks.proto.Control.Void,
              io.grpc.benchmarks.proto.Control.Void>(
                serviceImpl, METHODID_QUIT_WORKER)))
        .build();
  }
}
