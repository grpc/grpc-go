package io.envoyproxy.envoy.service.discovery.v2;

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
 * See https://github.com/lyft/envoy-api#apis for a description of the role of
 * ADS and how it is intended to be used by a management server. ADS requests
 * have the same structure as their singleton xDS counterparts, but can
 * multiplex many resource types on a single stream. The type_url in the
 * DiscoveryRequest/DiscoveryResponse provides sufficient information to recover
 * the multiplexed singleton APIs at the Envoy instance and management server.
 * </pre>
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: envoy/service/discovery/v2/ads.proto")
public final class AggregatedDiscoveryServiceGrpc {

  private AggregatedDiscoveryServiceGrpc() {}

  public static final String SERVICE_NAME = "envoy.service.discovery.v2.AggregatedDiscoveryService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.envoyproxy.envoy.api.v2.DiscoveryRequest,
      io.envoyproxy.envoy.api.v2.DiscoveryResponse> getStreamAggregatedResourcesMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "StreamAggregatedResources",
      requestType = io.envoyproxy.envoy.api.v2.DiscoveryRequest.class,
      responseType = io.envoyproxy.envoy.api.v2.DiscoveryResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.envoyproxy.envoy.api.v2.DiscoveryRequest,
      io.envoyproxy.envoy.api.v2.DiscoveryResponse> getStreamAggregatedResourcesMethod() {
    io.grpc.MethodDescriptor<io.envoyproxy.envoy.api.v2.DiscoveryRequest, io.envoyproxy.envoy.api.v2.DiscoveryResponse> getStreamAggregatedResourcesMethod;
    if ((getStreamAggregatedResourcesMethod = AggregatedDiscoveryServiceGrpc.getStreamAggregatedResourcesMethod) == null) {
      synchronized (AggregatedDiscoveryServiceGrpc.class) {
        if ((getStreamAggregatedResourcesMethod = AggregatedDiscoveryServiceGrpc.getStreamAggregatedResourcesMethod) == null) {
          AggregatedDiscoveryServiceGrpc.getStreamAggregatedResourcesMethod = getStreamAggregatedResourcesMethod = 
              io.grpc.MethodDescriptor.<io.envoyproxy.envoy.api.v2.DiscoveryRequest, io.envoyproxy.envoy.api.v2.DiscoveryResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "envoy.service.discovery.v2.AggregatedDiscoveryService", "StreamAggregatedResources"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.envoyproxy.envoy.api.v2.DiscoveryRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.envoyproxy.envoy.api.v2.DiscoveryResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new AggregatedDiscoveryServiceMethodDescriptorSupplier("StreamAggregatedResources"))
                  .build();
          }
        }
     }
     return getStreamAggregatedResourcesMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest,
      io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse> getIncrementalAggregatedResourcesMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "IncrementalAggregatedResources",
      requestType = io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest.class,
      responseType = io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest,
      io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse> getIncrementalAggregatedResourcesMethod() {
    io.grpc.MethodDescriptor<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest, io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse> getIncrementalAggregatedResourcesMethod;
    if ((getIncrementalAggregatedResourcesMethod = AggregatedDiscoveryServiceGrpc.getIncrementalAggregatedResourcesMethod) == null) {
      synchronized (AggregatedDiscoveryServiceGrpc.class) {
        if ((getIncrementalAggregatedResourcesMethod = AggregatedDiscoveryServiceGrpc.getIncrementalAggregatedResourcesMethod) == null) {
          AggregatedDiscoveryServiceGrpc.getIncrementalAggregatedResourcesMethod = getIncrementalAggregatedResourcesMethod = 
              io.grpc.MethodDescriptor.<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest, io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "envoy.service.discovery.v2.AggregatedDiscoveryService", "IncrementalAggregatedResources"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new AggregatedDiscoveryServiceMethodDescriptorSupplier("IncrementalAggregatedResources"))
                  .build();
          }
        }
     }
     return getIncrementalAggregatedResourcesMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static AggregatedDiscoveryServiceStub newStub(io.grpc.Channel channel) {
    return new AggregatedDiscoveryServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static AggregatedDiscoveryServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new AggregatedDiscoveryServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static AggregatedDiscoveryServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new AggregatedDiscoveryServiceFutureStub(channel);
  }

  /**
   * <pre>
   * See https://github.com/lyft/envoy-api#apis for a description of the role of
   * ADS and how it is intended to be used by a management server. ADS requests
   * have the same structure as their singleton xDS counterparts, but can
   * multiplex many resource types on a single stream. The type_url in the
   * DiscoveryRequest/DiscoveryResponse provides sufficient information to recover
   * the multiplexed singleton APIs at the Envoy instance and management server.
   * </pre>
   */
  public static abstract class AggregatedDiscoveryServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * This is a gRPC-only API.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.DiscoveryRequest> streamAggregatedResources(
        io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.DiscoveryResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getStreamAggregatedResourcesMethod(), responseObserver);
    }

    /**
     */
    public io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest> incrementalAggregatedResources(
        io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getIncrementalAggregatedResourcesMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getStreamAggregatedResourcesMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.envoyproxy.envoy.api.v2.DiscoveryRequest,
                io.envoyproxy.envoy.api.v2.DiscoveryResponse>(
                  this, METHODID_STREAM_AGGREGATED_RESOURCES)))
          .addMethod(
            getIncrementalAggregatedResourcesMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest,
                io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse>(
                  this, METHODID_INCREMENTAL_AGGREGATED_RESOURCES)))
          .build();
    }
  }

  /**
   * <pre>
   * See https://github.com/lyft/envoy-api#apis for a description of the role of
   * ADS and how it is intended to be used by a management server. ADS requests
   * have the same structure as their singleton xDS counterparts, but can
   * multiplex many resource types on a single stream. The type_url in the
   * DiscoveryRequest/DiscoveryResponse provides sufficient information to recover
   * the multiplexed singleton APIs at the Envoy instance and management server.
   * </pre>
   */
  public static final class AggregatedDiscoveryServiceStub extends io.grpc.stub.AbstractStub<AggregatedDiscoveryServiceStub> {
    private AggregatedDiscoveryServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private AggregatedDiscoveryServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected AggregatedDiscoveryServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new AggregatedDiscoveryServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * This is a gRPC-only API.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.DiscoveryRequest> streamAggregatedResources(
        io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.DiscoveryResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getStreamAggregatedResourcesMethod(), getCallOptions()), responseObserver);
    }

    /**
     */
    public io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryRequest> incrementalAggregatedResources(
        io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getIncrementalAggregatedResourcesMethod(), getCallOptions()), responseObserver);
    }
  }

  /**
   * <pre>
   * See https://github.com/lyft/envoy-api#apis for a description of the role of
   * ADS and how it is intended to be used by a management server. ADS requests
   * have the same structure as their singleton xDS counterparts, but can
   * multiplex many resource types on a single stream. The type_url in the
   * DiscoveryRequest/DiscoveryResponse provides sufficient information to recover
   * the multiplexed singleton APIs at the Envoy instance and management server.
   * </pre>
   */
  public static final class AggregatedDiscoveryServiceBlockingStub extends io.grpc.stub.AbstractStub<AggregatedDiscoveryServiceBlockingStub> {
    private AggregatedDiscoveryServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private AggregatedDiscoveryServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected AggregatedDiscoveryServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new AggregatedDiscoveryServiceBlockingStub(channel, callOptions);
    }
  }

  /**
   * <pre>
   * See https://github.com/lyft/envoy-api#apis for a description of the role of
   * ADS and how it is intended to be used by a management server. ADS requests
   * have the same structure as their singleton xDS counterparts, but can
   * multiplex many resource types on a single stream. The type_url in the
   * DiscoveryRequest/DiscoveryResponse provides sufficient information to recover
   * the multiplexed singleton APIs at the Envoy instance and management server.
   * </pre>
   */
  public static final class AggregatedDiscoveryServiceFutureStub extends io.grpc.stub.AbstractStub<AggregatedDiscoveryServiceFutureStub> {
    private AggregatedDiscoveryServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private AggregatedDiscoveryServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected AggregatedDiscoveryServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new AggregatedDiscoveryServiceFutureStub(channel, callOptions);
    }
  }

  private static final int METHODID_STREAM_AGGREGATED_RESOURCES = 0;
  private static final int METHODID_INCREMENTAL_AGGREGATED_RESOURCES = 1;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final AggregatedDiscoveryServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(AggregatedDiscoveryServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        default:
          throw new AssertionError();
      }
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public io.grpc.stub.StreamObserver<Req> invoke(
        io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_STREAM_AGGREGATED_RESOURCES:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.streamAggregatedResources(
              (io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.DiscoveryResponse>) responseObserver);
        case METHODID_INCREMENTAL_AGGREGATED_RESOURCES:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.incrementalAggregatedResources(
              (io.grpc.stub.StreamObserver<io.envoyproxy.envoy.api.v2.IncrementalDiscoveryResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class AggregatedDiscoveryServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    AggregatedDiscoveryServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.envoyproxy.envoy.service.discovery.v2.AdsProto.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("AggregatedDiscoveryService");
    }
  }

  private static final class AggregatedDiscoveryServiceFileDescriptorSupplier
      extends AggregatedDiscoveryServiceBaseDescriptorSupplier {
    AggregatedDiscoveryServiceFileDescriptorSupplier() {}
  }

  private static final class AggregatedDiscoveryServiceMethodDescriptorSupplier
      extends AggregatedDiscoveryServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    AggregatedDiscoveryServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (AggregatedDiscoveryServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new AggregatedDiscoveryServiceFileDescriptorSupplier())
              .addMethod(getStreamAggregatedResourcesMethod())
              .addMethod(getIncrementalAggregatedResourcesMethod())
              .build();
        }
      }
    }
    return result;
  }
}
