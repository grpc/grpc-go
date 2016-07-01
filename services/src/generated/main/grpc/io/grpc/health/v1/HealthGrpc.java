package io.grpc.health.v1;

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
    value = "by gRPC proto compiler (version 1.0.0-SNAPSHOT)",
    comments = "Source: health.proto")
public class HealthGrpc {

  private HealthGrpc() {}

  public static final String SERVICE_NAME = "grpc.health.v1.Health";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.health.v1.HealthCheckRequest,
      io.grpc.health.v1.HealthCheckResponse> METHOD_CHECK =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.health.v1.Health", "Check"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.health.v1.HealthCheckRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.health.v1.HealthCheckResponse.getDefaultInstance()));

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static HealthStub newStub(io.grpc.Channel channel) {
    return new HealthStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static HealthBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new HealthBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary and streaming output calls on the service
   */
  public static HealthFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new HealthFutureStub(channel);
  }

  /**
   */
  @java.lang.Deprecated public static interface Health {

    /**
     */
    public void check(io.grpc.health.v1.HealthCheckRequest request,
        io.grpc.stub.StreamObserver<io.grpc.health.v1.HealthCheckResponse> responseObserver);
  }

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1469")
  public static abstract class HealthImplBase implements Health, io.grpc.BindableService {

    @java.lang.Override
    public void check(io.grpc.health.v1.HealthCheckRequest request,
        io.grpc.stub.StreamObserver<io.grpc.health.v1.HealthCheckResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_CHECK, responseObserver);
    }

    @java.lang.Override public io.grpc.ServerServiceDefinition bindService() {
      return HealthGrpc.bindService(this);
    }
  }

  /**
   */
  @java.lang.Deprecated public static interface HealthBlockingClient {

    /**
     */
    public io.grpc.health.v1.HealthCheckResponse check(io.grpc.health.v1.HealthCheckRequest request);
  }

  /**
   */
  @java.lang.Deprecated public static interface HealthFutureClient {

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.health.v1.HealthCheckResponse> check(
        io.grpc.health.v1.HealthCheckRequest request);
  }

  public static class HealthStub extends io.grpc.stub.AbstractStub<HealthStub>
      implements Health {
    private HealthStub(io.grpc.Channel channel) {
      super(channel);
    }

    private HealthStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HealthStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new HealthStub(channel, callOptions);
    }

    @java.lang.Override
    public void check(io.grpc.health.v1.HealthCheckRequest request,
        io.grpc.stub.StreamObserver<io.grpc.health.v1.HealthCheckResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_CHECK, getCallOptions()), request, responseObserver);
    }
  }

  public static class HealthBlockingStub extends io.grpc.stub.AbstractStub<HealthBlockingStub>
      implements HealthBlockingClient {
    private HealthBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private HealthBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HealthBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new HealthBlockingStub(channel, callOptions);
    }

    @java.lang.Override
    public io.grpc.health.v1.HealthCheckResponse check(io.grpc.health.v1.HealthCheckRequest request) {
      return blockingUnaryCall(
          getChannel(), METHOD_CHECK, getCallOptions(), request);
    }
  }

  public static class HealthFutureStub extends io.grpc.stub.AbstractStub<HealthFutureStub>
      implements HealthFutureClient {
    private HealthFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private HealthFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HealthFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new HealthFutureStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.health.v1.HealthCheckResponse> check(
        io.grpc.health.v1.HealthCheckRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_CHECK, getCallOptions()), request);
    }
  }

  @java.lang.Deprecated public static abstract class AbstractHealth extends HealthImplBase {}

  private static final int METHODID_CHECK = 0;

  private static class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final Health serviceImpl;
    private final int methodId;

    public MethodHandlers(Health serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_CHECK:
          serviceImpl.check((io.grpc.health.v1.HealthCheckRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.health.v1.HealthCheckResponse>) responseObserver);
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
        METHOD_CHECK);
  }

  @java.lang.Deprecated public static io.grpc.ServerServiceDefinition bindService(
      final Health serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
        .addMethod(
          METHOD_CHECK,
          asyncUnaryCall(
            new MethodHandlers<
              io.grpc.health.v1.HealthCheckRequest,
              io.grpc.health.v1.HealthCheckResponse>(
                serviceImpl, METHODID_CHECK)))
        .build();
  }
}
