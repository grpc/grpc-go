package io.grpc.lb.v1;

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
    comments = "Source: grpc/lb/v1/load_balancer.proto")
public final class LoadBalancerGrpc {

  private LoadBalancerGrpc() {}

  public static final String SERVICE_NAME = "grpc.lb.v1.LoadBalancer";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.lb.v1.LoadBalanceRequest,
      io.grpc.lb.v1.LoadBalanceResponse> getBalanceLoadMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "BalanceLoad",
      requestType = io.grpc.lb.v1.LoadBalanceRequest.class,
      responseType = io.grpc.lb.v1.LoadBalanceResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.lb.v1.LoadBalanceRequest,
      io.grpc.lb.v1.LoadBalanceResponse> getBalanceLoadMethod() {
    io.grpc.MethodDescriptor<io.grpc.lb.v1.LoadBalanceRequest, io.grpc.lb.v1.LoadBalanceResponse> getBalanceLoadMethod;
    if ((getBalanceLoadMethod = LoadBalancerGrpc.getBalanceLoadMethod) == null) {
      synchronized (LoadBalancerGrpc.class) {
        if ((getBalanceLoadMethod = LoadBalancerGrpc.getBalanceLoadMethod) == null) {
          LoadBalancerGrpc.getBalanceLoadMethod = getBalanceLoadMethod = 
              io.grpc.MethodDescriptor.<io.grpc.lb.v1.LoadBalanceRequest, io.grpc.lb.v1.LoadBalanceResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.lb.v1.LoadBalancer", "BalanceLoad"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.lb.v1.LoadBalanceRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.lb.v1.LoadBalanceResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new LoadBalancerMethodDescriptorSupplier("BalanceLoad"))
                  .build();
          }
        }
     }
     return getBalanceLoadMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static LoadBalancerStub newStub(io.grpc.Channel channel) {
    return new LoadBalancerStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static LoadBalancerBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new LoadBalancerBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static LoadBalancerFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new LoadBalancerFutureStub(channel);
  }

  /**
   */
  public static abstract class LoadBalancerImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * Bidirectional rpc to get a list of servers.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.lb.v1.LoadBalanceRequest> balanceLoad(
        io.grpc.stub.StreamObserver<io.grpc.lb.v1.LoadBalanceResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getBalanceLoadMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getBalanceLoadMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.lb.v1.LoadBalanceRequest,
                io.grpc.lb.v1.LoadBalanceResponse>(
                  this, METHODID_BALANCE_LOAD)))
          .build();
    }
  }

  /**
   */
  public static final class LoadBalancerStub extends io.grpc.stub.AbstractStub<LoadBalancerStub> {
    private LoadBalancerStub(io.grpc.Channel channel) {
      super(channel);
    }

    private LoadBalancerStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected LoadBalancerStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new LoadBalancerStub(channel, callOptions);
    }

    /**
     * <pre>
     * Bidirectional rpc to get a list of servers.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.lb.v1.LoadBalanceRequest> balanceLoad(
        io.grpc.stub.StreamObserver<io.grpc.lb.v1.LoadBalanceResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getBalanceLoadMethod(), getCallOptions()), responseObserver);
    }
  }

  /**
   */
  public static final class LoadBalancerBlockingStub extends io.grpc.stub.AbstractStub<LoadBalancerBlockingStub> {
    private LoadBalancerBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private LoadBalancerBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected LoadBalancerBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new LoadBalancerBlockingStub(channel, callOptions);
    }
  }

  /**
   */
  public static final class LoadBalancerFutureStub extends io.grpc.stub.AbstractStub<LoadBalancerFutureStub> {
    private LoadBalancerFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private LoadBalancerFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected LoadBalancerFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new LoadBalancerFutureStub(channel, callOptions);
    }
  }

  private static final int METHODID_BALANCE_LOAD = 0;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final LoadBalancerImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(LoadBalancerImplBase serviceImpl, int methodId) {
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
        case METHODID_BALANCE_LOAD:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.balanceLoad(
              (io.grpc.stub.StreamObserver<io.grpc.lb.v1.LoadBalanceResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class LoadBalancerBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    LoadBalancerBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.lb.v1.LoadBalancerProto.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("LoadBalancer");
    }
  }

  private static final class LoadBalancerFileDescriptorSupplier
      extends LoadBalancerBaseDescriptorSupplier {
    LoadBalancerFileDescriptorSupplier() {}
  }

  private static final class LoadBalancerMethodDescriptorSupplier
      extends LoadBalancerBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    LoadBalancerMethodDescriptorSupplier(String methodName) {
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
      synchronized (LoadBalancerGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new LoadBalancerFileDescriptorSupplier())
              .addMethod(getBalanceLoadMethod())
              .build();
        }
      }
    }
    return result;
  }
}
