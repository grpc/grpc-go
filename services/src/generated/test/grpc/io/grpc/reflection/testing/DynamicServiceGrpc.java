package io.grpc.reflection.testing;

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
 * A DynamicService
 * </pre>
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler (version 1.3.0-SNAPSHOT)",
    comments = "Source: io/grpc/reflection/testing/dynamic_reflection_test.proto")
public final class DynamicServiceGrpc {

  private DynamicServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.reflection.testing.DynamicService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.reflection.testing.DynamicRequest,
      io.grpc.reflection.testing.DynamicReply> METHOD_METHOD =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.reflection.testing.DynamicService", "Method"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.reflection.testing.DynamicRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.reflection.testing.DynamicReply.getDefaultInstance()));

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static DynamicServiceStub newStub(io.grpc.Channel channel) {
    return new DynamicServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static DynamicServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new DynamicServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary and streaming output calls on the service
   */
  public static DynamicServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new DynamicServiceFutureStub(channel);
  }

  /**
   * <pre>
   * A DynamicService
   * </pre>
   */
  public static abstract class DynamicServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * A method
     * </pre>
     */
    public void method(io.grpc.reflection.testing.DynamicRequest request,
        io.grpc.stub.StreamObserver<io.grpc.reflection.testing.DynamicReply> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_METHOD, responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            METHOD_METHOD,
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.reflection.testing.DynamicRequest,
                io.grpc.reflection.testing.DynamicReply>(
                  this, METHODID_METHOD)))
          .build();
    }
  }

  /**
   * <pre>
   * A DynamicService
   * </pre>
   */
  public static final class DynamicServiceStub extends io.grpc.stub.AbstractStub<DynamicServiceStub> {
    private DynamicServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private DynamicServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected DynamicServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new DynamicServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * A method
     * </pre>
     */
    public void method(io.grpc.reflection.testing.DynamicRequest request,
        io.grpc.stub.StreamObserver<io.grpc.reflection.testing.DynamicReply> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_METHOD, getCallOptions()), request, responseObserver);
    }
  }

  /**
   * <pre>
   * A DynamicService
   * </pre>
   */
  public static final class DynamicServiceBlockingStub extends io.grpc.stub.AbstractStub<DynamicServiceBlockingStub> {
    private DynamicServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private DynamicServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected DynamicServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new DynamicServiceBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * A method
     * </pre>
     */
    public io.grpc.reflection.testing.DynamicReply method(io.grpc.reflection.testing.DynamicRequest request) {
      return blockingUnaryCall(
          getChannel(), METHOD_METHOD, getCallOptions(), request);
    }
  }

  /**
   * <pre>
   * A DynamicService
   * </pre>
   */
  public static final class DynamicServiceFutureStub extends io.grpc.stub.AbstractStub<DynamicServiceFutureStub> {
    private DynamicServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private DynamicServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected DynamicServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new DynamicServiceFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * A method
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.reflection.testing.DynamicReply> method(
        io.grpc.reflection.testing.DynamicRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_METHOD, getCallOptions()), request);
    }
  }

  private static final int METHODID_METHOD = 0;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final DynamicServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(DynamicServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_METHOD:
          serviceImpl.method((io.grpc.reflection.testing.DynamicRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.reflection.testing.DynamicReply>) responseObserver);
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

  private static final class DynamicServiceDescriptorSupplier implements io.grpc.protobuf.ProtoFileDescriptorSupplier {
    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.reflection.testing.DynamicReflectionTestProto.getDescriptor();
    }
  }

  private static volatile io.grpc.ServiceDescriptor serviceDescriptor;

  public static io.grpc.ServiceDescriptor getServiceDescriptor() {
    io.grpc.ServiceDescriptor result = serviceDescriptor;
    if (result == null) {
      synchronized (DynamicServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new DynamicServiceDescriptorSupplier())
              .addMethod(METHOD_METHOD)
              .build();
        }
      }
    }
    return result;
  }
}
