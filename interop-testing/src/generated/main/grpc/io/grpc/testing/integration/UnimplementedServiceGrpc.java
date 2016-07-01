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
 * <pre>
 * A simple service NOT implemented at servers so clients can test for
 * that case.
 * </pre>
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler (version 1.0.0-SNAPSHOT)",
    comments = "Source: io/grpc/testing/integration/test.proto")
public class UnimplementedServiceGrpc {

  private UnimplementedServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.UnimplementedService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
      com.google.protobuf.EmptyProtos.Empty> METHOD_UNIMPLEMENTED_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.UnimplementedService", "UnimplementedCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.getDefaultInstance()));

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static UnimplementedServiceStub newStub(io.grpc.Channel channel) {
    return new UnimplementedServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static UnimplementedServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new UnimplementedServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary and streaming output calls on the service
   */
  public static UnimplementedServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new UnimplementedServiceFutureStub(channel);
  }

  /**
   * <pre>
   * A simple service NOT implemented at servers so clients can test for
   * that case.
   * </pre>
   */
  @java.lang.Deprecated public static interface UnimplementedService {

    /**
     * <pre>
     * A call that no server should implement
     * </pre>
     */
    public void unimplementedCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver);
  }

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1469")
  public static abstract class UnimplementedServiceImplBase implements UnimplementedService, io.grpc.BindableService {

    @java.lang.Override
    public void unimplementedCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_UNIMPLEMENTED_CALL, responseObserver);
    }

    @java.lang.Override public io.grpc.ServerServiceDefinition bindService() {
      return UnimplementedServiceGrpc.bindService(this);
    }
  }

  /**
   * <pre>
   * A simple service NOT implemented at servers so clients can test for
   * that case.
   * </pre>
   */
  @java.lang.Deprecated public static interface UnimplementedServiceBlockingClient {

    /**
     * <pre>
     * A call that no server should implement
     * </pre>
     */
    public com.google.protobuf.EmptyProtos.Empty unimplementedCall(com.google.protobuf.EmptyProtos.Empty request);
  }

  /**
   * <pre>
   * A simple service NOT implemented at servers so clients can test for
   * that case.
   * </pre>
   */
  @java.lang.Deprecated public static interface UnimplementedServiceFutureClient {

    /**
     * <pre>
     * A call that no server should implement
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> unimplementedCall(
        com.google.protobuf.EmptyProtos.Empty request);
  }

  public static class UnimplementedServiceStub extends io.grpc.stub.AbstractStub<UnimplementedServiceStub>
      implements UnimplementedService {
    private UnimplementedServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private UnimplementedServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected UnimplementedServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new UnimplementedServiceStub(channel, callOptions);
    }

    @java.lang.Override
    public void unimplementedCall(com.google.protobuf.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_UNIMPLEMENTED_CALL, getCallOptions()), request, responseObserver);
    }
  }

  public static class UnimplementedServiceBlockingStub extends io.grpc.stub.AbstractStub<UnimplementedServiceBlockingStub>
      implements UnimplementedServiceBlockingClient {
    private UnimplementedServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private UnimplementedServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected UnimplementedServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new UnimplementedServiceBlockingStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.protobuf.EmptyProtos.Empty unimplementedCall(com.google.protobuf.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          getChannel(), METHOD_UNIMPLEMENTED_CALL, getCallOptions(), request);
    }
  }

  public static class UnimplementedServiceFutureStub extends io.grpc.stub.AbstractStub<UnimplementedServiceFutureStub>
      implements UnimplementedServiceFutureClient {
    private UnimplementedServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private UnimplementedServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected UnimplementedServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new UnimplementedServiceFutureStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> unimplementedCall(
        com.google.protobuf.EmptyProtos.Empty request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_UNIMPLEMENTED_CALL, getCallOptions()), request);
    }
  }

  @java.lang.Deprecated public static abstract class AbstractUnimplementedService extends UnimplementedServiceImplBase {}

  private static final int METHODID_UNIMPLEMENTED_CALL = 0;

  private static class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final UnimplementedService serviceImpl;
    private final int methodId;

    public MethodHandlers(UnimplementedService serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_UNIMPLEMENTED_CALL:
          serviceImpl.unimplementedCall((com.google.protobuf.EmptyProtos.Empty) request,
              (io.grpc.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty>) responseObserver);
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
        METHOD_UNIMPLEMENTED_CALL);
  }

  @java.lang.Deprecated public static io.grpc.ServerServiceDefinition bindService(
      final UnimplementedService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
        .addMethod(
          METHOD_UNIMPLEMENTED_CALL,
          asyncUnaryCall(
            new MethodHandlers<
              com.google.protobuf.EmptyProtos.Empty,
              com.google.protobuf.EmptyProtos.Empty>(
                serviceImpl, METHODID_UNIMPLEMENTED_CALL)))
        .build();
  }
}
