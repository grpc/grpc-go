package io.grpc.xds.shaded.envoy.api.v2;

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
 * <pre>
 * Return list of all clusters this proxy will load balance to.
 * </pre>
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: envoy/api/v2/cds.proto")
public final class ClusterDiscoveryServiceGrpc {

  private ClusterDiscoveryServiceGrpc() {}

  public static final String SERVICE_NAME = "envoy.api.v2.ClusterDiscoveryService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getStreamClustersMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "StreamClusters",
      requestType = io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest.class,
      responseType = io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getStreamClustersMethod() {
    io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getStreamClustersMethod;
    if ((getStreamClustersMethod = ClusterDiscoveryServiceGrpc.getStreamClustersMethod) == null) {
      synchronized (ClusterDiscoveryServiceGrpc.class) {
        if ((getStreamClustersMethod = ClusterDiscoveryServiceGrpc.getStreamClustersMethod) == null) {
          ClusterDiscoveryServiceGrpc.getStreamClustersMethod = getStreamClustersMethod = 
              io.grpc.MethodDescriptor.<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "envoy.api.v2.ClusterDiscoveryService", "StreamClusters"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ClusterDiscoveryServiceMethodDescriptorSupplier("StreamClusters"))
                  .build();
          }
        }
     }
     return getStreamClustersMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse> getIncrementalClustersMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "IncrementalClusters",
      requestType = io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest.class,
      responseType = io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse> getIncrementalClustersMethod() {
    io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse> getIncrementalClustersMethod;
    if ((getIncrementalClustersMethod = ClusterDiscoveryServiceGrpc.getIncrementalClustersMethod) == null) {
      synchronized (ClusterDiscoveryServiceGrpc.class) {
        if ((getIncrementalClustersMethod = ClusterDiscoveryServiceGrpc.getIncrementalClustersMethod) == null) {
          ClusterDiscoveryServiceGrpc.getIncrementalClustersMethod = getIncrementalClustersMethod = 
              io.grpc.MethodDescriptor.<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "envoy.api.v2.ClusterDiscoveryService", "IncrementalClusters"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ClusterDiscoveryServiceMethodDescriptorSupplier("IncrementalClusters"))
                  .build();
          }
        }
     }
     return getIncrementalClustersMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getFetchClustersMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "FetchClusters",
      requestType = io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest.class,
      responseType = io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getFetchClustersMethod() {
    io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getFetchClustersMethod;
    if ((getFetchClustersMethod = ClusterDiscoveryServiceGrpc.getFetchClustersMethod) == null) {
      synchronized (ClusterDiscoveryServiceGrpc.class) {
        if ((getFetchClustersMethod = ClusterDiscoveryServiceGrpc.getFetchClustersMethod) == null) {
          ClusterDiscoveryServiceGrpc.getFetchClustersMethod = getFetchClustersMethod = 
              io.grpc.MethodDescriptor.<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "envoy.api.v2.ClusterDiscoveryService", "FetchClusters"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ClusterDiscoveryServiceMethodDescriptorSupplier("FetchClusters"))
                  .build();
          }
        }
     }
     return getFetchClustersMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static ClusterDiscoveryServiceStub newStub(io.grpc.Channel channel) {
    return new ClusterDiscoveryServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static ClusterDiscoveryServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new ClusterDiscoveryServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static ClusterDiscoveryServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new ClusterDiscoveryServiceFutureStub(channel);
  }

  /**
   * <pre>
   * Return list of all clusters this proxy will load balance to.
   * </pre>
   */
  public static abstract class ClusterDiscoveryServiceImplBase implements io.grpc.BindableService {

    /**
     */
    public io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest> streamClusters(
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getStreamClustersMethod(), responseObserver);
    }

    /**
     */
    public io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest> incrementalClusters(
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getIncrementalClustersMethod(), responseObserver);
    }

    /**
     */
    public void fetchClusters(io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest request,
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getFetchClustersMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getStreamClustersMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
                io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>(
                  this, METHODID_STREAM_CLUSTERS)))
          .addMethod(
            getIncrementalClustersMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest,
                io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse>(
                  this, METHODID_INCREMENTAL_CLUSTERS)))
          .addMethod(
            getFetchClustersMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
                io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>(
                  this, METHODID_FETCH_CLUSTERS)))
          .build();
    }
  }

  /**
   * <pre>
   * Return list of all clusters this proxy will load balance to.
   * </pre>
   */
  public static final class ClusterDiscoveryServiceStub extends io.grpc.stub.AbstractStub<ClusterDiscoveryServiceStub> {
    private ClusterDiscoveryServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ClusterDiscoveryServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ClusterDiscoveryServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ClusterDiscoveryServiceStub(channel, callOptions);
    }

    /**
     */
    public io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest> streamClusters(
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getStreamClustersMethod(), getCallOptions()), responseObserver);
    }

    /**
     */
    public io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryRequest> incrementalClusters(
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getIncrementalClustersMethod(), getCallOptions()), responseObserver);
    }

    /**
     */
    public void fetchClusters(io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest request,
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getFetchClustersMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   * <pre>
   * Return list of all clusters this proxy will load balance to.
   * </pre>
   */
  public static final class ClusterDiscoveryServiceBlockingStub extends io.grpc.stub.AbstractStub<ClusterDiscoveryServiceBlockingStub> {
    private ClusterDiscoveryServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ClusterDiscoveryServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ClusterDiscoveryServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ClusterDiscoveryServiceBlockingStub(channel, callOptions);
    }

    /**
     */
    public io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse fetchClusters(io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest request) {
      return blockingUnaryCall(
          getChannel(), getFetchClustersMethod(), getCallOptions(), request);
    }
  }

  /**
   * <pre>
   * Return list of all clusters this proxy will load balance to.
   * </pre>
   */
  public static final class ClusterDiscoveryServiceFutureStub extends io.grpc.stub.AbstractStub<ClusterDiscoveryServiceFutureStub> {
    private ClusterDiscoveryServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ClusterDiscoveryServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ClusterDiscoveryServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ClusterDiscoveryServiceFutureStub(channel, callOptions);
    }

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> fetchClusters(
        io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getFetchClustersMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_FETCH_CLUSTERS = 0;
  private static final int METHODID_STREAM_CLUSTERS = 1;
  private static final int METHODID_INCREMENTAL_CLUSTERS = 2;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final ClusterDiscoveryServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(ClusterDiscoveryServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_FETCH_CLUSTERS:
          serviceImpl.fetchClusters((io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>) responseObserver);
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
        case METHODID_STREAM_CLUSTERS:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.streamClusters(
              (io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>) responseObserver);
        case METHODID_INCREMENTAL_CLUSTERS:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.incrementalClusters(
              (io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.IncrementalDiscoveryResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class ClusterDiscoveryServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    ClusterDiscoveryServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.xds.shaded.envoy.api.v2.Cds.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("ClusterDiscoveryService");
    }
  }

  private static final class ClusterDiscoveryServiceFileDescriptorSupplier
      extends ClusterDiscoveryServiceBaseDescriptorSupplier {
    ClusterDiscoveryServiceFileDescriptorSupplier() {}
  }

  private static final class ClusterDiscoveryServiceMethodDescriptorSupplier
      extends ClusterDiscoveryServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    ClusterDiscoveryServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (ClusterDiscoveryServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new ClusterDiscoveryServiceFileDescriptorSupplier())
              .addMethod(getStreamClustersMethod())
              .addMethod(getIncrementalClustersMethod())
              .addMethod(getFetchClustersMethod())
              .build();
        }
      }
    }
    return result;
  }
}
