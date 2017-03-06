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

/**
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler (version 1.3.0-SNAPSHOT)",
    comments = "Source: services.proto")
public final class WorkerServiceGrpc {

  private WorkerServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.WorkerService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.ServerArgs,
      io.grpc.benchmarks.proto.Control.ServerStatus> METHOD_RUN_SERVER =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.WorkerService", "RunServer"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.ServerArgs.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.ServerStatus.getDefaultInstance()));
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.ClientArgs,
      io.grpc.benchmarks.proto.Control.ClientStatus> METHOD_RUN_CLIENT =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.WorkerService", "RunClient"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.ClientArgs.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.ClientStatus.getDefaultInstance()));
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.CoreRequest,
      io.grpc.benchmarks.proto.Control.CoreResponse> METHOD_CORE_COUNT =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.WorkerService", "CoreCount"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.CoreRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.CoreResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.Void,
      io.grpc.benchmarks.proto.Control.Void> METHOD_QUIT_WORKER =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.WorkerService", "QuitWorker"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.Void.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Control.Void.getDefaultInstance()));

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static WorkerServiceStub newStub(io.grpc.Channel channel) {
    return new WorkerServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static WorkerServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new WorkerServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary and streaming output calls on the service
   */
  public static WorkerServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new WorkerServiceFutureStub(channel);
  }

  /**
   */
  public static abstract class WorkerServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * Start server with specified workload.
     * First request sent specifies the ServerConfig followed by ServerStatus
     * response. After that, a "Mark" can be sent anytime to request the latest
     * stats. Closing the stream will initiate shutdown of the test server
     * and once the shutdown has finished, the OK status is sent to terminate
     * this RPC.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerArgs> runServer(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerStatus> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_RUN_SERVER, responseObserver);
    }

    /**
     * <pre>
     * Start client with specified workload.
     * First request sent specifies the ClientConfig followed by ClientStatus
     * response. After that, a "Mark" can be sent anytime to request the latest
     * stats. Closing the stream will initiate shutdown of the test client
     * and once the shutdown has finished, the OK status is sent to terminate
     * this RPC.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientArgs> runClient(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientStatus> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_RUN_CLIENT, responseObserver);
    }

    /**
     * <pre>
     * Just return the core count - unary call
     * </pre>
     */
    public void coreCount(io.grpc.benchmarks.proto.Control.CoreRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.CoreResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_CORE_COUNT, responseObserver);
    }

    /**
     * <pre>
     * Quit this worker
     * </pre>
     */
    public void quitWorker(io.grpc.benchmarks.proto.Control.Void request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.Void> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_QUIT_WORKER, responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            METHOD_RUN_SERVER,
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Control.ServerArgs,
                io.grpc.benchmarks.proto.Control.ServerStatus>(
                  this, METHODID_RUN_SERVER)))
          .addMethod(
            METHOD_RUN_CLIENT,
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Control.ClientArgs,
                io.grpc.benchmarks.proto.Control.ClientStatus>(
                  this, METHODID_RUN_CLIENT)))
          .addMethod(
            METHOD_CORE_COUNT,
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Control.CoreRequest,
                io.grpc.benchmarks.proto.Control.CoreResponse>(
                  this, METHODID_CORE_COUNT)))
          .addMethod(
            METHOD_QUIT_WORKER,
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Control.Void,
                io.grpc.benchmarks.proto.Control.Void>(
                  this, METHODID_QUIT_WORKER)))
          .build();
    }
  }

  /**
   */
  public static final class WorkerServiceStub extends io.grpc.stub.AbstractStub<WorkerServiceStub> {
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

    /**
     * <pre>
     * Start server with specified workload.
     * First request sent specifies the ServerConfig followed by ServerStatus
     * response. After that, a "Mark" can be sent anytime to request the latest
     * stats. Closing the stream will initiate shutdown of the test server
     * and once the shutdown has finished, the OK status is sent to terminate
     * this RPC.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerArgs> runServer(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ServerStatus> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_RUN_SERVER, getCallOptions()), responseObserver);
    }

    /**
     * <pre>
     * Start client with specified workload.
     * First request sent specifies the ClientConfig followed by ClientStatus
     * response. After that, a "Mark" can be sent anytime to request the latest
     * stats. Closing the stream will initiate shutdown of the test client
     * and once the shutdown has finished, the OK status is sent to terminate
     * this RPC.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientArgs> runClient(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.ClientStatus> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_RUN_CLIENT, getCallOptions()), responseObserver);
    }

    /**
     * <pre>
     * Just return the core count - unary call
     * </pre>
     */
    public void coreCount(io.grpc.benchmarks.proto.Control.CoreRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.CoreResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_CORE_COUNT, getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Quit this worker
     * </pre>
     */
    public void quitWorker(io.grpc.benchmarks.proto.Control.Void request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.Void> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_QUIT_WORKER, getCallOptions()), request, responseObserver);
    }
  }

  /**
   */
  public static final class WorkerServiceBlockingStub extends io.grpc.stub.AbstractStub<WorkerServiceBlockingStub> {
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

    /**
     * <pre>
     * Just return the core count - unary call
     * </pre>
     */
    public io.grpc.benchmarks.proto.Control.CoreResponse coreCount(io.grpc.benchmarks.proto.Control.CoreRequest request) {
      return blockingUnaryCall(
          getChannel(), METHOD_CORE_COUNT, getCallOptions(), request);
    }

    /**
     * <pre>
     * Quit this worker
     * </pre>
     */
    public io.grpc.benchmarks.proto.Control.Void quitWorker(io.grpc.benchmarks.proto.Control.Void request) {
      return blockingUnaryCall(
          getChannel(), METHOD_QUIT_WORKER, getCallOptions(), request);
    }
  }

  /**
   */
  public static final class WorkerServiceFutureStub extends io.grpc.stub.AbstractStub<WorkerServiceFutureStub> {
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

    /**
     * <pre>
     * Just return the core count - unary call
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Control.CoreResponse> coreCount(
        io.grpc.benchmarks.proto.Control.CoreRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_CORE_COUNT, getCallOptions()), request);
    }

    /**
     * <pre>
     * Quit this worker
     * </pre>
     */
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

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final WorkerServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(WorkerServiceImplBase serviceImpl, int methodId) {
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

  private static final class WorkerServiceDescriptorSupplier implements io.grpc.protobuf.ProtoFileDescriptorSupplier {
    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.benchmarks.proto.Services.getDescriptor();
    }
  }

  private static volatile io.grpc.ServiceDescriptor serviceDescriptor;

  public static io.grpc.ServiceDescriptor getServiceDescriptor() {
    io.grpc.ServiceDescriptor result = serviceDescriptor;
    if (result == null) {
      synchronized (WorkerServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new WorkerServiceDescriptorSupplier())
              .addMethod(METHOD_RUN_SERVER)
              .addMethod(METHOD_RUN_CLIENT)
              .addMethod(METHOD_CORE_COUNT)
              .addMethod(METHOD_QUIT_WORKER)
              .build();
        }
      }
    }
    return result;
  }
}
