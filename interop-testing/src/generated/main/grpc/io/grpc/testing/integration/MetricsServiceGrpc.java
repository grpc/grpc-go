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

/**
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler (version 0.15.0-SNAPSHOT)",
    comments = "Source: io/grpc/testing/integration/metrics.proto")
public class MetricsServiceGrpc {

  private MetricsServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.MetricsService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Metrics.EmptyMessage,
      io.grpc.testing.integration.Metrics.GaugeResponse> METHOD_GET_ALL_GAUGES =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING,
          generateFullMethodName(
              "grpc.testing.MetricsService", "GetAllGauges"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Metrics.EmptyMessage.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Metrics.GaugeResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Metrics.GaugeRequest,
      io.grpc.testing.integration.Metrics.GaugeResponse> METHOD_GET_GAUGE =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.MetricsService", "GetGauge"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Metrics.GaugeRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.integration.Metrics.GaugeResponse.getDefaultInstance()));

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
   * Creates a new ListenableFuture-style stub that supports unary and streaming output calls on the service
   */
  public static MetricsServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new MetricsServiceFutureStub(channel);
  }

  /**
   */
  @java.lang.Deprecated public static interface MetricsService {

    /**
     * <pre>
     * Returns the values of all the gauges that are currently being maintained by
     * the service
     * </pre>
     */
    public void getAllGauges(io.grpc.testing.integration.Metrics.EmptyMessage request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver);

    /**
     * <pre>
     * Returns the value of one gauge
     * </pre>
     */
    public void getGauge(io.grpc.testing.integration.Metrics.GaugeRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver);
  }

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1469")
  public static abstract class MetricsServiceImplBase implements MetricsService, io.grpc.BindableService {

    @java.lang.Override
    public void getAllGauges(io.grpc.testing.integration.Metrics.EmptyMessage request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_GET_ALL_GAUGES, responseObserver);
    }

    @java.lang.Override
    public void getGauge(io.grpc.testing.integration.Metrics.GaugeRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_GET_GAUGE, responseObserver);
    }

    @java.lang.Override public io.grpc.ServerServiceDefinition bindService() {
      return MetricsServiceGrpc.bindService(this);
    }
  }

  /**
   */
  @java.lang.Deprecated public static interface MetricsServiceBlockingClient {

    /**
     * <pre>
     * Returns the values of all the gauges that are currently being maintained by
     * the service
     * </pre>
     */
    public java.util.Iterator<io.grpc.testing.integration.Metrics.GaugeResponse> getAllGauges(
        io.grpc.testing.integration.Metrics.EmptyMessage request);

    /**
     * <pre>
     * Returns the value of one gauge
     * </pre>
     */
    public io.grpc.testing.integration.Metrics.GaugeResponse getGauge(io.grpc.testing.integration.Metrics.GaugeRequest request);
  }

  /**
   */
  @java.lang.Deprecated public static interface MetricsServiceFutureClient {

    /**
     * <pre>
     * Returns the value of one gauge
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Metrics.GaugeResponse> getGauge(
        io.grpc.testing.integration.Metrics.GaugeRequest request);
  }

  public static class MetricsServiceStub extends io.grpc.stub.AbstractStub<MetricsServiceStub>
      implements MetricsService {
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

    @java.lang.Override
    public void getAllGauges(io.grpc.testing.integration.Metrics.EmptyMessage request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(METHOD_GET_ALL_GAUGES, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public void getGauge(io.grpc.testing.integration.Metrics.GaugeRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Metrics.GaugeResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_GET_GAUGE, getCallOptions()), request, responseObserver);
    }
  }

  public static class MetricsServiceBlockingStub extends io.grpc.stub.AbstractStub<MetricsServiceBlockingStub>
      implements MetricsServiceBlockingClient {
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

    @java.lang.Override
    public java.util.Iterator<io.grpc.testing.integration.Metrics.GaugeResponse> getAllGauges(
        io.grpc.testing.integration.Metrics.EmptyMessage request) {
      return blockingServerStreamingCall(
          getChannel(), METHOD_GET_ALL_GAUGES, getCallOptions(), request);
    }

    @java.lang.Override
    public io.grpc.testing.integration.Metrics.GaugeResponse getGauge(io.grpc.testing.integration.Metrics.GaugeRequest request) {
      return blockingUnaryCall(
          getChannel(), METHOD_GET_GAUGE, getCallOptions(), request);
    }
  }

  public static class MetricsServiceFutureStub extends io.grpc.stub.AbstractStub<MetricsServiceFutureStub>
      implements MetricsServiceFutureClient {
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

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Metrics.GaugeResponse> getGauge(
        io.grpc.testing.integration.Metrics.GaugeRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_GET_GAUGE, getCallOptions()), request);
    }
  }

  @java.lang.Deprecated public static abstract class AbstractMetricsService extends MetricsServiceImplBase {}

  private static final int METHODID_GET_ALL_GAUGES = 0;
  private static final int METHODID_GET_GAUGE = 1;

  private static class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final MetricsService serviceImpl;
    private final int methodId;

    public MethodHandlers(MetricsService serviceImpl, int methodId) {
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

  public static io.grpc.ServiceDescriptor getServiceDescriptor() {
    return new io.grpc.ServiceDescriptor(SERVICE_NAME,
        METHOD_GET_ALL_GAUGES,
        METHOD_GET_GAUGE);
  }

  @java.lang.Deprecated public static io.grpc.ServerServiceDefinition bindService(
      final MetricsService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
        .addMethod(
          METHOD_GET_ALL_GAUGES,
          asyncServerStreamingCall(
            new MethodHandlers<
              io.grpc.testing.integration.Metrics.EmptyMessage,
              io.grpc.testing.integration.Metrics.GaugeResponse>(
                serviceImpl, METHODID_GET_ALL_GAUGES)))
        .addMethod(
          METHOD_GET_GAUGE,
          asyncUnaryCall(
            new MethodHandlers<
              io.grpc.testing.integration.Metrics.GaugeRequest,
              io.grpc.testing.integration.Metrics.GaugeResponse>(
                serviceImpl, METHODID_GET_GAUGE)))
        .build();
  }
}
