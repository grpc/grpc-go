package io.grpc.instrumentation.v1alpha;

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
    comments = "Source: grpc/instrumentation/v1alpha/monitoring.proto")
public final class MonitoringGrpc {

  private MonitoringGrpc() {}

  public static final String SERVICE_NAME = "grpc.instrumentation.v1alpha.Monitoring";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetCanonicalRpcStatsMethod()} instead. 
  public static final io.grpc.MethodDescriptor<com.google.protobuf.Empty,
      io.grpc.instrumentation.v1alpha.CanonicalRpcStats> METHOD_GET_CANONICAL_RPC_STATS = getGetCanonicalRpcStatsMethodHelper();

  private static volatile io.grpc.MethodDescriptor<com.google.protobuf.Empty,
      io.grpc.instrumentation.v1alpha.CanonicalRpcStats> getGetCanonicalRpcStatsMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<com.google.protobuf.Empty,
      io.grpc.instrumentation.v1alpha.CanonicalRpcStats> getGetCanonicalRpcStatsMethod() {
    return getGetCanonicalRpcStatsMethodHelper();
  }

  private static io.grpc.MethodDescriptor<com.google.protobuf.Empty,
      io.grpc.instrumentation.v1alpha.CanonicalRpcStats> getGetCanonicalRpcStatsMethodHelper() {
    io.grpc.MethodDescriptor<com.google.protobuf.Empty, io.grpc.instrumentation.v1alpha.CanonicalRpcStats> getGetCanonicalRpcStatsMethod;
    if ((getGetCanonicalRpcStatsMethod = MonitoringGrpc.getGetCanonicalRpcStatsMethod) == null) {
      synchronized (MonitoringGrpc.class) {
        if ((getGetCanonicalRpcStatsMethod = MonitoringGrpc.getGetCanonicalRpcStatsMethod) == null) {
          MonitoringGrpc.getGetCanonicalRpcStatsMethod = getGetCanonicalRpcStatsMethod = 
              io.grpc.MethodDescriptor.<com.google.protobuf.Empty, io.grpc.instrumentation.v1alpha.CanonicalRpcStats>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.instrumentation.v1alpha.Monitoring", "GetCanonicalRpcStats"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  com.google.protobuf.Empty.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.CanonicalRpcStats.getDefaultInstance()))
                  .setSchemaDescriptor(new MonitoringMethodDescriptorSupplier("GetCanonicalRpcStats"))
                  .build();
          }
        }
     }
     return getGetCanonicalRpcStatsMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetStatsMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest,
      io.grpc.instrumentation.v1alpha.StatsResponse> METHOD_GET_STATS = getGetStatsMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest,
      io.grpc.instrumentation.v1alpha.StatsResponse> getGetStatsMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest,
      io.grpc.instrumentation.v1alpha.StatsResponse> getGetStatsMethod() {
    return getGetStatsMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest,
      io.grpc.instrumentation.v1alpha.StatsResponse> getGetStatsMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest, io.grpc.instrumentation.v1alpha.StatsResponse> getGetStatsMethod;
    if ((getGetStatsMethod = MonitoringGrpc.getGetStatsMethod) == null) {
      synchronized (MonitoringGrpc.class) {
        if ((getGetStatsMethod = MonitoringGrpc.getGetStatsMethod) == null) {
          MonitoringGrpc.getGetStatsMethod = getGetStatsMethod = 
              io.grpc.MethodDescriptor.<io.grpc.instrumentation.v1alpha.StatsRequest, io.grpc.instrumentation.v1alpha.StatsResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.instrumentation.v1alpha.Monitoring", "GetStats"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.StatsRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.StatsResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new MonitoringMethodDescriptorSupplier("GetStats"))
                  .build();
          }
        }
     }
     return getGetStatsMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getWatchStatsMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest,
      io.grpc.instrumentation.v1alpha.StatsResponse> METHOD_WATCH_STATS = getWatchStatsMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest,
      io.grpc.instrumentation.v1alpha.StatsResponse> getWatchStatsMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest,
      io.grpc.instrumentation.v1alpha.StatsResponse> getWatchStatsMethod() {
    return getWatchStatsMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest,
      io.grpc.instrumentation.v1alpha.StatsResponse> getWatchStatsMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.StatsRequest, io.grpc.instrumentation.v1alpha.StatsResponse> getWatchStatsMethod;
    if ((getWatchStatsMethod = MonitoringGrpc.getWatchStatsMethod) == null) {
      synchronized (MonitoringGrpc.class) {
        if ((getWatchStatsMethod = MonitoringGrpc.getWatchStatsMethod) == null) {
          MonitoringGrpc.getWatchStatsMethod = getWatchStatsMethod = 
              io.grpc.MethodDescriptor.<io.grpc.instrumentation.v1alpha.StatsRequest, io.grpc.instrumentation.v1alpha.StatsResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.instrumentation.v1alpha.Monitoring", "WatchStats"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.StatsRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.StatsResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new MonitoringMethodDescriptorSupplier("WatchStats"))
                  .build();
          }
        }
     }
     return getWatchStatsMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetRequestTracesMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.TraceRequest,
      io.grpc.instrumentation.v1alpha.TraceResponse> METHOD_GET_REQUEST_TRACES = getGetRequestTracesMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.TraceRequest,
      io.grpc.instrumentation.v1alpha.TraceResponse> getGetRequestTracesMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.TraceRequest,
      io.grpc.instrumentation.v1alpha.TraceResponse> getGetRequestTracesMethod() {
    return getGetRequestTracesMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.TraceRequest,
      io.grpc.instrumentation.v1alpha.TraceResponse> getGetRequestTracesMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.TraceRequest, io.grpc.instrumentation.v1alpha.TraceResponse> getGetRequestTracesMethod;
    if ((getGetRequestTracesMethod = MonitoringGrpc.getGetRequestTracesMethod) == null) {
      synchronized (MonitoringGrpc.class) {
        if ((getGetRequestTracesMethod = MonitoringGrpc.getGetRequestTracesMethod) == null) {
          MonitoringGrpc.getGetRequestTracesMethod = getGetRequestTracesMethod = 
              io.grpc.MethodDescriptor.<io.grpc.instrumentation.v1alpha.TraceRequest, io.grpc.instrumentation.v1alpha.TraceResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.instrumentation.v1alpha.Monitoring", "GetRequestTraces"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.TraceRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.TraceResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new MonitoringMethodDescriptorSupplier("GetRequestTraces"))
                  .build();
          }
        }
     }
     return getGetRequestTracesMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetCustomMonitoringDataMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.MonitoringDataGroup,
      io.grpc.instrumentation.v1alpha.CustomMonitoringData> METHOD_GET_CUSTOM_MONITORING_DATA = getGetCustomMonitoringDataMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.MonitoringDataGroup,
      io.grpc.instrumentation.v1alpha.CustomMonitoringData> getGetCustomMonitoringDataMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.MonitoringDataGroup,
      io.grpc.instrumentation.v1alpha.CustomMonitoringData> getGetCustomMonitoringDataMethod() {
    return getGetCustomMonitoringDataMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.MonitoringDataGroup,
      io.grpc.instrumentation.v1alpha.CustomMonitoringData> getGetCustomMonitoringDataMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.instrumentation.v1alpha.MonitoringDataGroup, io.grpc.instrumentation.v1alpha.CustomMonitoringData> getGetCustomMonitoringDataMethod;
    if ((getGetCustomMonitoringDataMethod = MonitoringGrpc.getGetCustomMonitoringDataMethod) == null) {
      synchronized (MonitoringGrpc.class) {
        if ((getGetCustomMonitoringDataMethod = MonitoringGrpc.getGetCustomMonitoringDataMethod) == null) {
          MonitoringGrpc.getGetCustomMonitoringDataMethod = getGetCustomMonitoringDataMethod = 
              io.grpc.MethodDescriptor.<io.grpc.instrumentation.v1alpha.MonitoringDataGroup, io.grpc.instrumentation.v1alpha.CustomMonitoringData>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.instrumentation.v1alpha.Monitoring", "GetCustomMonitoringData"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.MonitoringDataGroup.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.instrumentation.v1alpha.CustomMonitoringData.getDefaultInstance()))
                  .setSchemaDescriptor(new MonitoringMethodDescriptorSupplier("GetCustomMonitoringData"))
                  .build();
          }
        }
     }
     return getGetCustomMonitoringDataMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static MonitoringStub newStub(io.grpc.Channel channel) {
    return new MonitoringStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static MonitoringBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new MonitoringBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static MonitoringFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new MonitoringFutureStub(channel);
  }

  /**
   */
  public static abstract class MonitoringImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * Return canonical RPC stats
     * </pre>
     */
    public void getCanonicalRpcStats(com.google.protobuf.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.CanonicalRpcStats> responseObserver) {
      asyncUnimplementedUnaryCall(getGetCanonicalRpcStatsMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Query the server for specific stats
     * </pre>
     */
    public void getStats(io.grpc.instrumentation.v1alpha.StatsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.StatsResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetStatsMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Request the server to stream back snapshots of the requested stats
     * </pre>
     */
    public void watchStats(io.grpc.instrumentation.v1alpha.StatsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.StatsResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getWatchStatsMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Return request traces.
     * </pre>
     */
    public void getRequestTraces(io.grpc.instrumentation.v1alpha.TraceRequest request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.TraceResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetRequestTracesMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Return application-defined groups of monitoring data.
     * This is a low level facility to allow extension of the monitoring API to
     * application-specific monitoring data. Frameworks may use this to define
     * additional groups of monitoring data made available by servers.
     * </pre>
     */
    public void getCustomMonitoringData(io.grpc.instrumentation.v1alpha.MonitoringDataGroup request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.CustomMonitoringData> responseObserver) {
      asyncUnimplementedUnaryCall(getGetCustomMonitoringDataMethodHelper(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getGetCanonicalRpcStatsMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                com.google.protobuf.Empty,
                io.grpc.instrumentation.v1alpha.CanonicalRpcStats>(
                  this, METHODID_GET_CANONICAL_RPC_STATS)))
          .addMethod(
            getGetStatsMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.instrumentation.v1alpha.StatsRequest,
                io.grpc.instrumentation.v1alpha.StatsResponse>(
                  this, METHODID_GET_STATS)))
          .addMethod(
            getWatchStatsMethodHelper(),
            asyncServerStreamingCall(
              new MethodHandlers<
                io.grpc.instrumentation.v1alpha.StatsRequest,
                io.grpc.instrumentation.v1alpha.StatsResponse>(
                  this, METHODID_WATCH_STATS)))
          .addMethod(
            getGetRequestTracesMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.instrumentation.v1alpha.TraceRequest,
                io.grpc.instrumentation.v1alpha.TraceResponse>(
                  this, METHODID_GET_REQUEST_TRACES)))
          .addMethod(
            getGetCustomMonitoringDataMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.instrumentation.v1alpha.MonitoringDataGroup,
                io.grpc.instrumentation.v1alpha.CustomMonitoringData>(
                  this, METHODID_GET_CUSTOM_MONITORING_DATA)))
          .build();
    }
  }

  /**
   */
  public static final class MonitoringStub extends io.grpc.stub.AbstractStub<MonitoringStub> {
    private MonitoringStub(io.grpc.Channel channel) {
      super(channel);
    }

    private MonitoringStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected MonitoringStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new MonitoringStub(channel, callOptions);
    }

    /**
     * <pre>
     * Return canonical RPC stats
     * </pre>
     */
    public void getCanonicalRpcStats(com.google.protobuf.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.CanonicalRpcStats> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetCanonicalRpcStatsMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Query the server for specific stats
     * </pre>
     */
    public void getStats(io.grpc.instrumentation.v1alpha.StatsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.StatsResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetStatsMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Request the server to stream back snapshots of the requested stats
     * </pre>
     */
    public void watchStats(io.grpc.instrumentation.v1alpha.StatsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.StatsResponse> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(getWatchStatsMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Return request traces.
     * </pre>
     */
    public void getRequestTraces(io.grpc.instrumentation.v1alpha.TraceRequest request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.TraceResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetRequestTracesMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Return application-defined groups of monitoring data.
     * This is a low level facility to allow extension of the monitoring API to
     * application-specific monitoring data. Frameworks may use this to define
     * additional groups of monitoring data made available by servers.
     * </pre>
     */
    public void getCustomMonitoringData(io.grpc.instrumentation.v1alpha.MonitoringDataGroup request,
        io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.CustomMonitoringData> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetCustomMonitoringDataMethodHelper(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   */
  public static final class MonitoringBlockingStub extends io.grpc.stub.AbstractStub<MonitoringBlockingStub> {
    private MonitoringBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private MonitoringBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected MonitoringBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new MonitoringBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * Return canonical RPC stats
     * </pre>
     */
    public io.grpc.instrumentation.v1alpha.CanonicalRpcStats getCanonicalRpcStats(com.google.protobuf.Empty request) {
      return blockingUnaryCall(
          getChannel(), getGetCanonicalRpcStatsMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Query the server for specific stats
     * </pre>
     */
    public io.grpc.instrumentation.v1alpha.StatsResponse getStats(io.grpc.instrumentation.v1alpha.StatsRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetStatsMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Request the server to stream back snapshots of the requested stats
     * </pre>
     */
    public java.util.Iterator<io.grpc.instrumentation.v1alpha.StatsResponse> watchStats(
        io.grpc.instrumentation.v1alpha.StatsRequest request) {
      return blockingServerStreamingCall(
          getChannel(), getWatchStatsMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Return request traces.
     * </pre>
     */
    public io.grpc.instrumentation.v1alpha.TraceResponse getRequestTraces(io.grpc.instrumentation.v1alpha.TraceRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetRequestTracesMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Return application-defined groups of monitoring data.
     * This is a low level facility to allow extension of the monitoring API to
     * application-specific monitoring data. Frameworks may use this to define
     * additional groups of monitoring data made available by servers.
     * </pre>
     */
    public io.grpc.instrumentation.v1alpha.CustomMonitoringData getCustomMonitoringData(io.grpc.instrumentation.v1alpha.MonitoringDataGroup request) {
      return blockingUnaryCall(
          getChannel(), getGetCustomMonitoringDataMethodHelper(), getCallOptions(), request);
    }
  }

  /**
   */
  public static final class MonitoringFutureStub extends io.grpc.stub.AbstractStub<MonitoringFutureStub> {
    private MonitoringFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private MonitoringFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected MonitoringFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new MonitoringFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * Return canonical RPC stats
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.instrumentation.v1alpha.CanonicalRpcStats> getCanonicalRpcStats(
        com.google.protobuf.Empty request) {
      return futureUnaryCall(
          getChannel().newCall(getGetCanonicalRpcStatsMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Query the server for specific stats
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.instrumentation.v1alpha.StatsResponse> getStats(
        io.grpc.instrumentation.v1alpha.StatsRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetStatsMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Return request traces.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.instrumentation.v1alpha.TraceResponse> getRequestTraces(
        io.grpc.instrumentation.v1alpha.TraceRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetRequestTracesMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Return application-defined groups of monitoring data.
     * This is a low level facility to allow extension of the monitoring API to
     * application-specific monitoring data. Frameworks may use this to define
     * additional groups of monitoring data made available by servers.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.instrumentation.v1alpha.CustomMonitoringData> getCustomMonitoringData(
        io.grpc.instrumentation.v1alpha.MonitoringDataGroup request) {
      return futureUnaryCall(
          getChannel().newCall(getGetCustomMonitoringDataMethodHelper(), getCallOptions()), request);
    }
  }

  private static final int METHODID_GET_CANONICAL_RPC_STATS = 0;
  private static final int METHODID_GET_STATS = 1;
  private static final int METHODID_WATCH_STATS = 2;
  private static final int METHODID_GET_REQUEST_TRACES = 3;
  private static final int METHODID_GET_CUSTOM_MONITORING_DATA = 4;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final MonitoringImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(MonitoringImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_GET_CANONICAL_RPC_STATS:
          serviceImpl.getCanonicalRpcStats((com.google.protobuf.Empty) request,
              (io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.CanonicalRpcStats>) responseObserver);
          break;
        case METHODID_GET_STATS:
          serviceImpl.getStats((io.grpc.instrumentation.v1alpha.StatsRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.StatsResponse>) responseObserver);
          break;
        case METHODID_WATCH_STATS:
          serviceImpl.watchStats((io.grpc.instrumentation.v1alpha.StatsRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.StatsResponse>) responseObserver);
          break;
        case METHODID_GET_REQUEST_TRACES:
          serviceImpl.getRequestTraces((io.grpc.instrumentation.v1alpha.TraceRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.TraceResponse>) responseObserver);
          break;
        case METHODID_GET_CUSTOM_MONITORING_DATA:
          serviceImpl.getCustomMonitoringData((io.grpc.instrumentation.v1alpha.MonitoringDataGroup) request,
              (io.grpc.stub.StreamObserver<io.grpc.instrumentation.v1alpha.CustomMonitoringData>) responseObserver);
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

  private static abstract class MonitoringBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    MonitoringBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.instrumentation.v1alpha.MonitoringProto.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("Monitoring");
    }
  }

  private static final class MonitoringFileDescriptorSupplier
      extends MonitoringBaseDescriptorSupplier {
    MonitoringFileDescriptorSupplier() {}
  }

  private static final class MonitoringMethodDescriptorSupplier
      extends MonitoringBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    MonitoringMethodDescriptorSupplier(String methodName) {
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
      synchronized (MonitoringGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new MonitoringFileDescriptorSupplier())
              .addMethod(getGetCanonicalRpcStatsMethodHelper())
              .addMethod(getGetStatsMethodHelper())
              .addMethod(getWatchStatsMethodHelper())
              .addMethod(getGetRequestTracesMethodHelper())
              .addMethod(getGetCustomMonitoringDataMethodHelper())
              .build();
        }
      }
    }
    return result;
  }
}
