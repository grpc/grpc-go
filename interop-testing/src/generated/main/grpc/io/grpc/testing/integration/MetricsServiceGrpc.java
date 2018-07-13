package io.grpc.testing.integration;

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
    comments = "Source: grpc/testing/metrics.proto")
public final class MetricsServiceGrpc {

  private MetricsServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.MetricsService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.Metrics.EmptyMessage,
      io.grpc.testing.integration.Metrics.GaugeResponse> getGetAllGaugesMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetAllGauges",
      requestType = io.grpc.testing.integration.Metrics.EmptyMessage.class,
      responseType = io.grpc.testing.integration.Metrics.GaugeResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.Metrics.EmptyMessage,
      io.grpc.testing.integration.Metrics.GaugeResponse> getGetAllGaugesMethod() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.Metrics.EmptyMessage, io.grpc.testing.integration.Metrics.GaugeResponse> getGetAllGaugesMethod;
    if ((getGetAllGaugesMethod = MetricsServiceGrpc.getGetAllGaugesMethod) == null) {
      synchronized (MetricsServiceGrpc.class) {
        if ((getGetAllGaugesMethod = MetricsServiceGrpc.getGetAllGaugesMethod) == null) {
          MetricsServiceGrpc.getGetAllGaugesMethod = getGetAllGaugesMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.Metrics.EmptyMessage, io.grpc.testing.integration.Metrics.GaugeResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.MetricsService", "GetAllGauges"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Metrics.EmptyMessage.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Metrics.GaugeResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new MetricsServiceMethodDescriptorSupplier("GetAllGauges"))
                  .build();
          }
        }
     }
     return getGetAllGaugesMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.Metrics.GaugeRequest,
      io.grpc.testing.integration.Metrics.GaugeResponse> getGetGaugeMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetGauge",
      requestType = io.grpc.testing.integration.Metrics.GaugeRequest.class,
      responseType = io.grpc.testing.integration.Metrics.GaugeResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.Metrics.GaugeRequest,
      io.grpc.testing.integration.Metrics.GaugeResponse> getGetGaugeMethod() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.Metrics.GaugeRequest, io.grpc.testing.integration.Metrics.GaugeResponse> getGetGaugeMethod;
    if ((getGetGaugeMethod = MetricsServiceGrpc.getGetGaugeMethod) == null) {
      synchronized (MetricsServiceGrpc.class) {
        if ((getGetGaugeMethod = MetricsServiceGrpc.getGetGaugeMethod) == null) {
          MetricsServiceGrpc.getGetGaugeMethod = getGetGaugeMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.Metrics.GaugeRequest, io.grpc.testing.integration.Metrics.GaugeResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.MetricsService", "GetGauge"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Metrics.GaugeRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Metrics.GaugeResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new MetricsServiceMethodDescriptorSupplier("GetGauge"))
                  .build();
          }
        }
     }
     return getGetGaugeMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static MetricsServiceStub newStub(io.grpc.Channel channel) {
    return new MetricsServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static MetricsServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new MetricsServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static MetricsServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new MetricsServiceFutureStub(channel);
  }

  /**
   */
  public static abstract class MetricsServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * Returns the values of all the gauges that are currently being maintained by
     * the service
     * </pre>
     */
    public void getAllGauges(io.grpc.testing.integration.Metrics.EmptyMessage request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetAllGaugesMethod(), responseObserver);
    }

    /**
     * <pre>
     * Returns the value of one gauge
     * </pre>
     */
    public void getGauge(io.grpc.testing.integration.Metrics.GaugeRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetGaugeMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getGetAllGaugesMethod(),
            asyncServerStreamingCall(
              new MethodHandlers<
                io.grpc.testing.integration.Metrics.EmptyMessage,
                io.grpc.testing.integration.Metrics.GaugeResponse>(
                  this, METHODID_GET_ALL_GAUGES)))
          .addMethod(
            getGetGaugeMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.testing.integration.Metrics.GaugeRequest,
                io.grpc.testing.integration.Metrics.GaugeResponse>(
                  this, METHODID_GET_GAUGE)))
          .build();
    }
  }

  /**
   */
  public static final class MetricsServiceStub extends io.grpc.stub.AbstractStub<MetricsServiceStub> {
    private MetricsServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private MetricsServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected MetricsServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new MetricsServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * Returns the values of all the gauges that are currently being maintained by
     * the service
     * </pre>
     */
    public void getAllGauges(io.grpc.testing.integration.Metrics.EmptyMessage request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(getGetAllGaugesMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Returns the value of one gauge
     * </pre>
     */
    public void getGauge(io.grpc.testing.integration.Metrics.GaugeRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetGaugeMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   */
  public static final class MetricsServiceBlockingStub extends io.grpc.stub.AbstractStub<MetricsServiceBlockingStub> {
    private MetricsServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private MetricsServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected MetricsServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new MetricsServiceBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * Returns the values of all the gauges that are currently being maintained by
     * the service
     * </pre>
     */
    public java.util.Iterator<io.grpc.testing.integration.Metrics.GaugeResponse> getAllGauges(
        io.grpc.testing.integration.Metrics.EmptyMessage request) {
      return blockingServerStreamingCall(
          getChannel(), getGetAllGaugesMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Returns the value of one gauge
     * </pre>
     */
    public io.grpc.testing.integration.Metrics.GaugeResponse getGauge(io.grpc.testing.integration.Metrics.GaugeRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetGaugeMethod(), getCallOptions(), request);
    }
  }

  /**
   */
  public static final class MetricsServiceFutureStub extends io.grpc.stub.AbstractStub<MetricsServiceFutureStub> {
    private MetricsServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private MetricsServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected MetricsServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new MetricsServiceFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * Returns the value of one gauge
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Metrics.GaugeResponse> getGauge(
        io.grpc.testing.integration.Metrics.GaugeRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetGaugeMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_GET_ALL_GAUGES = 0;
  private static final int METHODID_GET_GAUGE = 1;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final MetricsServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(MetricsServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_GET_ALL_GAUGES:
          serviceImpl.getAllGauges((io.grpc.testing.integration.Metrics.EmptyMessage) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse>) responseObserver);
          break;
        case METHODID_GET_GAUGE:
          serviceImpl.getGauge((io.grpc.testing.integration.Metrics.GaugeRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse>) responseObserver);
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

  private static abstract class MetricsServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    MetricsServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.testing.integration.Metrics.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("MetricsService");
    }
  }

  private static final class MetricsServiceFileDescriptorSupplier
      extends MetricsServiceBaseDescriptorSupplier {
    MetricsServiceFileDescriptorSupplier() {}
  }

  private static final class MetricsServiceMethodDescriptorSupplier
      extends MetricsServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    MetricsServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (MetricsServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new MetricsServiceFileDescriptorSupplier())
              .addMethod(getGetAllGaugesMethod())
              .addMethod(getGetGaugeMethod())
              .build();
        }
      }
    }
    return result;
  }
}
