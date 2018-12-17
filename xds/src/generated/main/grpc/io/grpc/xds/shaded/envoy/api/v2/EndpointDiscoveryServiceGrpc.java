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
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: envoy/api/v2/eds.proto")
public final class EndpointDiscoveryServiceGrpc {

  private EndpointDiscoveryServiceGrpc() {}

  public static final String SERVICE_NAME = "envoy.api.v2.EndpointDiscoveryService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getStreamEndpointsMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "StreamEndpoints",
      requestType = io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest.class,
      responseType = io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getStreamEndpointsMethod() {
    io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getStreamEndpointsMethod;
    if ((getStreamEndpointsMethod = EndpointDiscoveryServiceGrpc.getStreamEndpointsMethod) == null) {
      synchronized (EndpointDiscoveryServiceGrpc.class) {
        if ((getStreamEndpointsMethod = EndpointDiscoveryServiceGrpc.getStreamEndpointsMethod) == null) {
          EndpointDiscoveryServiceGrpc.getStreamEndpointsMethod = getStreamEndpointsMethod = 
              io.grpc.MethodDescriptor.<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "envoy.api.v2.EndpointDiscoveryService", "StreamEndpoints"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new EndpointDiscoveryServiceMethodDescriptorSupplier("StreamEndpoints"))
                  .build();
          }
        }
     }
     return getStreamEndpointsMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getFetchEndpointsMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "FetchEndpoints",
      requestType = io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest.class,
      responseType = io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
      io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getFetchEndpointsMethod() {
    io.grpc.MethodDescriptor<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> getFetchEndpointsMethod;
    if ((getFetchEndpointsMethod = EndpointDiscoveryServiceGrpc.getFetchEndpointsMethod) == null) {
      synchronized (EndpointDiscoveryServiceGrpc.class) {
        if ((getFetchEndpointsMethod = EndpointDiscoveryServiceGrpc.getFetchEndpointsMethod) == null) {
          EndpointDiscoveryServiceGrpc.getFetchEndpointsMethod = getFetchEndpointsMethod = 
              io.grpc.MethodDescriptor.<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest, io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "envoy.api.v2.EndpointDiscoveryService", "FetchEndpoints"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new EndpointDiscoveryServiceMethodDescriptorSupplier("FetchEndpoints"))
                  .build();
          }
        }
     }
     return getFetchEndpointsMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static EndpointDiscoveryServiceStub newStub(io.grpc.Channel channel) {
    return new EndpointDiscoveryServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static EndpointDiscoveryServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new EndpointDiscoveryServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static EndpointDiscoveryServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new EndpointDiscoveryServiceFutureStub(channel);
  }

  /**
   */
  public static abstract class EndpointDiscoveryServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * The resource_names field in DiscoveryRequest specifies a list of clusters
     * to subscribe to updates for.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest> streamEndpoints(
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getStreamEndpointsMethod(), responseObserver);
    }

    /**
     */
    public void fetchEndpoints(io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest request,
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getFetchEndpointsMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getStreamEndpointsMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
                io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>(
                  this, METHODID_STREAM_ENDPOINTS)))
          .addMethod(
            getFetchEndpointsMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest,
                io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>(
                  this, METHODID_FETCH_ENDPOINTS)))
          .build();
    }
  }

  /**
   */
  public static final class EndpointDiscoveryServiceStub extends io.grpc.stub.AbstractStub<EndpointDiscoveryServiceStub> {
    private EndpointDiscoveryServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private EndpointDiscoveryServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected EndpointDiscoveryServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new EndpointDiscoveryServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * The resource_names field in DiscoveryRequest specifies a list of clusters
     * to subscribe to updates for.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest> streamEndpoints(
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getStreamEndpointsMethod(), getCallOptions()), responseObserver);
    }

    /**
     */
    public void fetchEndpoints(io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest request,
        io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getFetchEndpointsMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   */
  public static final class EndpointDiscoveryServiceBlockingStub extends io.grpc.stub.AbstractStub<EndpointDiscoveryServiceBlockingStub> {
    private EndpointDiscoveryServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private EndpointDiscoveryServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected EndpointDiscoveryServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new EndpointDiscoveryServiceBlockingStub(channel, callOptions);
    }

    /**
     */
    public io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse fetchEndpoints(io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest request) {
      return blockingUnaryCall(
          getChannel(), getFetchEndpointsMethod(), getCallOptions(), request);
    }
  }

  /**
   */
  public static final class EndpointDiscoveryServiceFutureStub extends io.grpc.stub.AbstractStub<EndpointDiscoveryServiceFutureStub> {
    private EndpointDiscoveryServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private EndpointDiscoveryServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected EndpointDiscoveryServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new EndpointDiscoveryServiceFutureStub(channel, callOptions);
    }

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse> fetchEndpoints(
        io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getFetchEndpointsMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_FETCH_ENDPOINTS = 0;
  private static final int METHODID_STREAM_ENDPOINTS = 1;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final EndpointDiscoveryServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(EndpointDiscoveryServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_FETCH_ENDPOINTS:
          serviceImpl.fetchEndpoints((io.grpc.xds.shaded.envoy.api.v2.DiscoveryRequest) request,
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
        case METHODID_STREAM_ENDPOINTS:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.streamEndpoints(
              (io.grpc.stub.StreamObserver<io.grpc.xds.shaded.envoy.api.v2.DiscoveryResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class EndpointDiscoveryServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    EndpointDiscoveryServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.xds.shaded.envoy.api.v2.Eds.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("EndpointDiscoveryService");
    }
  }

  private static final class EndpointDiscoveryServiceFileDescriptorSupplier
      extends EndpointDiscoveryServiceBaseDescriptorSupplier {
    EndpointDiscoveryServiceFileDescriptorSupplier() {}
  }

  private static final class EndpointDiscoveryServiceMethodDescriptorSupplier
      extends EndpointDiscoveryServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    EndpointDiscoveryServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (EndpointDiscoveryServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new EndpointDiscoveryServiceFileDescriptorSupplier())
              .addMethod(getStreamEndpointsMethod())
              .addMethod(getFetchEndpointsMethod())
              .build();
        }
      }
    }
    return result;
  }
}
