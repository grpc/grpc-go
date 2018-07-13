package io.grpc.benchmarks.proto;

import static io.grpc.MethodDescriptor.generateFullMethodName;
import static io.grpc.stub.ClientCalls.asyncBidiStreamingCall;
import static io.grpc.stub.ClientCalls.asyncClientStreamingCall;
import static io.grpc.stub.ClientCalls.asyncServerStreamingCall;
import static io.grpc.stub.ClientCalls.asyncUnaryCall;
import static io.grpc.stub.ClientCalls.blockingServerStreamingCall;
import static io.grpc.stub.ClientCalls.blockingUnaryCall;
import static io.grpc.stub.ClientCalls.futureUnaryCall;
import static io.grpc.stub.ServerCalls.asyncBidiStreamingCall;
import static io.grpc.stub.ServerCalls.asyncClientStreamingCall;
import static io.grpc.stub.ServerCalls.asyncServerStreamingCall;
import static io.grpc.stub.ServerCalls.asyncUnaryCall;
import static io.grpc.stub.ServerCalls.asyncUnimplementedStreamingCall;
import static io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall;

/**
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: grpc/testing/services.proto")
public final class ReportQpsScenarioServiceGrpc {

  private ReportQpsScenarioServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.ReportQpsScenarioService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.ScenarioResult,
      io.grpc.benchmarks.proto.Control.Void> getReportScenarioMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "ReportScenario",
      requestType = io.grpc.benchmarks.proto.Control.ScenarioResult.class,
      responseType = io.grpc.benchmarks.proto.Control.Void.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.ScenarioResult,
      io.grpc.benchmarks.proto.Control.Void> getReportScenarioMethod() {
    io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.ScenarioResult, io.grpc.benchmarks.proto.Control.Void> getReportScenarioMethod;
    if ((getReportScenarioMethod = ReportQpsScenarioServiceGrpc.getReportScenarioMethod) == null) {
      synchronized (ReportQpsScenarioServiceGrpc.class) {
        if ((getReportScenarioMethod = ReportQpsScenarioServiceGrpc.getReportScenarioMethod) == null) {
          ReportQpsScenarioServiceGrpc.getReportScenarioMethod = getReportScenarioMethod = 
              io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Control.ScenarioResult, io.grpc.benchmarks.proto.Control.Void>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.ReportQpsScenarioService", "ReportScenario"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Control.ScenarioResult.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Control.Void.getDefaultInstance()))
                  .setSchemaDescriptor(new ReportQpsScenarioServiceMethodDescriptorSupplier("ReportScenario"))
                  .build();
          }
        }
     }
     return getReportScenarioMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static ReportQpsScenarioServiceStub newStub(io.grpc.Channel channel) {
    return new ReportQpsScenarioServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static ReportQpsScenarioServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new ReportQpsScenarioServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static ReportQpsScenarioServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new ReportQpsScenarioServiceFutureStub(channel);
  }

  /**
   */
  public static abstract class ReportQpsScenarioServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * Report results of a QPS test benchmark scenario.
     * </pre>
     */
    public void reportScenario(io.grpc.benchmarks.proto.Control.ScenarioResult request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.Void> responseObserver) {
      asyncUnimplementedUnaryCall(getReportScenarioMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getReportScenarioMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Control.ScenarioResult,
                io.grpc.benchmarks.proto.Control.Void>(
                  this, METHODID_REPORT_SCENARIO)))
          .build();
    }
  }

  /**
   */
  public static final class ReportQpsScenarioServiceStub extends io.grpc.stub.AbstractStub<ReportQpsScenarioServiceStub> {
    private ReportQpsScenarioServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReportQpsScenarioServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReportQpsScenarioServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReportQpsScenarioServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * Report results of a QPS test benchmark scenario.
     * </pre>
     */
    public void reportScenario(io.grpc.benchmarks.proto.Control.ScenarioResult request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Control.Void> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getReportScenarioMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   */
  public static final class ReportQpsScenarioServiceBlockingStub extends io.grpc.stub.AbstractStub<ReportQpsScenarioServiceBlockingStub> {
    private ReportQpsScenarioServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReportQpsScenarioServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReportQpsScenarioServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReportQpsScenarioServiceBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * Report results of a QPS test benchmark scenario.
     * </pre>
     */
    public io.grpc.benchmarks.proto.Control.Void reportScenario(io.grpc.benchmarks.proto.Control.ScenarioResult request) {
      return blockingUnaryCall(
          getChannel(), getReportScenarioMethod(), getCallOptions(), request);
    }
  }

  /**
   */
  public static final class ReportQpsScenarioServiceFutureStub extends io.grpc.stub.AbstractStub<ReportQpsScenarioServiceFutureStub> {
    private ReportQpsScenarioServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReportQpsScenarioServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReportQpsScenarioServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReportQpsScenarioServiceFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * Report results of a QPS test benchmark scenario.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Control.Void> reportScenario(
        io.grpc.benchmarks.proto.Control.ScenarioResult request) {
      return futureUnaryCall(
          getChannel().newCall(getReportScenarioMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_REPORT_SCENARIO = 0;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final ReportQpsScenarioServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(ReportQpsScenarioServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_REPORT_SCENARIO:
          serviceImpl.reportScenario((io.grpc.benchmarks.proto.Control.ScenarioResult) request,
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
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class ReportQpsScenarioServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    ReportQpsScenarioServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.benchmarks.proto.Services.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("ReportQpsScenarioService");
    }
  }

  private static final class ReportQpsScenarioServiceFileDescriptorSupplier
      extends ReportQpsScenarioServiceBaseDescriptorSupplier {
    ReportQpsScenarioServiceFileDescriptorSupplier() {}
  }

  private static final class ReportQpsScenarioServiceMethodDescriptorSupplier
      extends ReportQpsScenarioServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    ReportQpsScenarioServiceMethodDescriptorSupplier(String methodName) {
      this.methodName = methodName;
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.MethodDescriptor getMethodDescriptor() {
      return getServiceDescriptor().findMethodByName(methodName);
    }
  }

  private static volatile io.grpc.ServiceDescriptor serviceDescriptor;

  public static io.grpc.ServiceDescriptor getServiceDescriptor() {
    io.grpc.ServiceDescriptor result = serviceDescriptor;
    if (result == null) {
      synchronized (ReportQpsScenarioServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new ReportQpsScenarioServiceFileDescriptorSupplier())
              .addMethod(getReportScenarioMethod())
              .build();
        }
      }
    }
    return result;
  }
}
