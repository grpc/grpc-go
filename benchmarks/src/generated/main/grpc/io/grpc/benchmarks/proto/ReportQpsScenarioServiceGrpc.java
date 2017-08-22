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
    value = "by gRPC proto compiler (version 1.7.0-SNAPSHOT)",
    comments = "Source: services.proto")
public final class ReportQpsScenarioServiceGrpc {

  private ReportQpsScenarioServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.ReportQpsScenarioService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Control.ScenarioResult,
      io.grpc.benchmarks.proto.Control.Void> METHOD_REPORT_SCENARIO =
      io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Control.ScenarioResult, io.grpc.benchmarks.proto.Control.Void>newBuilder()
          .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
          .setFullMethodName(generateFullMethodName(
              "grpc.testing.ReportQpsScenarioService", "ReportScenario"))
          .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Control.ScenarioResult.getDefaultInstance()))
          .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Control.Void.getDefaultInstance()))
          .build();

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
      asyncUnimplementedUnaryCall(METHOD_REPORT_SCENARIO, responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            METHOD_REPORT_SCENARIO,
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
          getChannel().newCall(METHOD_REPORT_SCENARIO, getCallOptions()), request, responseObserver);
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
          getChannel(), METHOD_REPORT_SCENARIO, getCallOptions(), request);
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
          getChannel().newCall(METHOD_REPORT_SCENARIO, getCallOptions()), request);
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

  private static final class ReportQpsScenarioServiceDescriptorSupplier implements io.grpc.protobuf.ProtoFileDescriptorSupplier {
    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.benchmarks.proto.Services.getDescriptor();
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
              .setSchemaDescriptor(new ReportQpsScenarioServiceDescriptorSupplier())
              .addMethod(METHOD_REPORT_SCENARIO)
              .build();
        }
      }
    }
    return result;
  }
}
