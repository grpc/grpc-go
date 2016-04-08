package io.grpc.grpclb;

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

@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: load_balancer.proto")
public class LoadBalancerGrpc {

  private LoadBalancerGrpc() {}

  public static final String SERVICE_NAME = "grpc.lb.v1.LoadBalancer";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.grpclb.LoadBalanceRequest,
      io.grpc.grpclb.LoadBalanceResponse> METHOD_BALANCE_LOAD =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.lb.v1.LoadBalancer", "BalanceLoad"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.grpclb.LoadBalanceRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.grpclb.LoadBalanceResponse.getDefaultInstance()));

  public static LoadBalancerStub newStub(io.grpc.Channel channel) {
    return new LoadBalancerStub(channel);
  }

  public static LoadBalancerBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new LoadBalancerBlockingStub(channel);
  }

  public static LoadBalancerFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new LoadBalancerFutureStub(channel);
  }

  public static interface LoadBalancer {

    public io.grpc.stub.StreamObserver<io.grpc.grpclb.LoadBalanceRequest> balanceLoad(
        io.grpc.stub.StreamObserver<io.grpc.grpclb.LoadBalanceResponse> responseObserver);
  }

  public static abstract class AbstractLoadBalancer implements LoadBalancer, io.grpc.BindableService {

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.grpclb.LoadBalanceRequest> balanceLoad(
        io.grpc.stub.StreamObserver<io.grpc.grpclb.LoadBalanceResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_BALANCE_LOAD, responseObserver);
    }

    @java.lang.Override public io.grpc.ServerServiceDefinition bindService() {
      return LoadBalancerGrpc.bindService(this);
    }
  }

  public static interface LoadBalancerBlockingClient {
  }

  public static interface LoadBalancerFutureClient {
  }

  public static class LoadBalancerStub extends io.grpc.stub.AbstractStub<LoadBalancerStub>
      implements LoadBalancer {
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

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.grpclb.LoadBalanceRequest> balanceLoad(
        io.grpc.stub.StreamObserver<io.grpc.grpclb.LoadBalanceResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_BALANCE_LOAD, getCallOptions()), responseObserver);
    }
  }

  public static class LoadBalancerBlockingStub extends io.grpc.stub.AbstractStub<LoadBalancerBlockingStub>
      implements LoadBalancerBlockingClient {
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

  public static class LoadBalancerFutureStub extends io.grpc.stub.AbstractStub<LoadBalancerFutureStub>
      implements LoadBalancerFutureClient {
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

  private static class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final LoadBalancer serviceImpl;
    private final int methodId;

    public MethodHandlers(LoadBalancer serviceImpl, int methodId) {
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
              (io.grpc.stub.StreamObserver<io.grpc.grpclb.LoadBalanceResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final LoadBalancer serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
        .addMethod(
          METHOD_BALANCE_LOAD,
          asyncBidiStreamingCall(
            new MethodHandlers<
              io.grpc.grpclb.LoadBalanceRequest,
              io.grpc.grpclb.LoadBalanceResponse>(
                serviceImpl, METHODID_BALANCE_LOAD)))
        .build();
  }
}
