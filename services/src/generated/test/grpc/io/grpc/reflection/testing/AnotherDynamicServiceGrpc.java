package io.grpc.reflection.testing;

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
 * AnotherDynamicService
 * </pre>
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: io/grpc/reflection/testing/dynamic_reflection_test.proto")
public final class AnotherDynamicServiceGrpc {

  private AnotherDynamicServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.reflection.testing.AnotherDynamicService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.reflection.testing.DynamicRequest,
      io.grpc.reflection.testing.DynamicReply> getMethodMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "Method",
      requestType = io.grpc.reflection.testing.DynamicRequest.class,
      responseType = io.grpc.reflection.testing.DynamicReply.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.reflection.testing.DynamicRequest,
      io.grpc.reflection.testing.DynamicReply> getMethodMethod() {
    io.grpc.MethodDescriptor<io.grpc.reflection.testing.DynamicRequest, io.grpc.reflection.testing.DynamicReply> getMethodMethod;
    if ((getMethodMethod = AnotherDynamicServiceGrpc.getMethodMethod) == null) {
      synchronized (AnotherDynamicServiceGrpc.class) {
        if ((getMethodMethod = AnotherDynamicServiceGrpc.getMethodMethod) == null) {
          AnotherDynamicServiceGrpc.getMethodMethod = getMethodMethod = 
              io.grpc.MethodDescriptor.<io.grpc.reflection.testing.DynamicRequest, io.grpc.reflection.testing.DynamicReply>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.reflection.testing.AnotherDynamicService", "Method"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.reflection.testing.DynamicRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.reflection.testing.DynamicReply.getDefaultInstance()))
                  .setSchemaDescriptor(new AnotherDynamicServiceMethodDescriptorSupplier("Method"))
                  .build();
          }
        }
     }
     return getMethodMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static AnotherDynamicServiceStub newStub(io.grpc.Channel channel) {
    return new AnotherDynamicServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static AnotherDynamicServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new AnotherDynamicServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static AnotherDynamicServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new AnotherDynamicServiceFutureStub(channel);
  }

  /**
   * <pre>
   * AnotherDynamicService
   * </pre>
   */
  public static abstract class AnotherDynamicServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * A method
     * </pre>
     */
    public void method(io.grpc.reflection.testing.DynamicRequest request,
        io.grpc.stub.StreamObserver<io.grpc.reflection.testing.DynamicReply> responseObserver) {
      asyncUnimplementedUnaryCall(getMethodMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getMethodMethod(),
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
   * AnotherDynamicService
   * </pre>
   */
  public static final class AnotherDynamicServiceStub extends io.grpc.stub.AbstractStub<AnotherDynamicServiceStub> {
    private AnotherDynamicServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private AnotherDynamicServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected AnotherDynamicServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new AnotherDynamicServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * A method
     * </pre>
     */
    public void method(io.grpc.reflection.testing.DynamicRequest request,
        io.grpc.stub.StreamObserver<io.grpc.reflection.testing.DynamicReply> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getMethodMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   * <pre>
   * AnotherDynamicService
   * </pre>
   */
  public static final class AnotherDynamicServiceBlockingStub extends io.grpc.stub.AbstractStub<AnotherDynamicServiceBlockingStub> {
    private AnotherDynamicServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private AnotherDynamicServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected AnotherDynamicServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new AnotherDynamicServiceBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * A method
     * </pre>
     */
    public io.grpc.reflection.testing.DynamicReply method(io.grpc.reflection.testing.DynamicRequest request) {
      return blockingUnaryCall(
          getChannel(), getMethodMethod(), getCallOptions(), request);
    }
  }

  /**
   * <pre>
   * AnotherDynamicService
   * </pre>
   */
  public static final class AnotherDynamicServiceFutureStub extends io.grpc.stub.AbstractStub<AnotherDynamicServiceFutureStub> {
    private AnotherDynamicServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private AnotherDynamicServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected AnotherDynamicServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new AnotherDynamicServiceFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * A method
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.reflection.testing.DynamicReply> method(
        io.grpc.reflection.testing.DynamicRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getMethodMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_METHOD = 0;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final AnotherDynamicServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(AnotherDynamicServiceImplBase serviceImpl, int methodId) {
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

  private static abstract class AnotherDynamicServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    AnotherDynamicServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.reflection.testing.DynamicReflectionTestProto.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("AnotherDynamicService");
    }
  }

  private static final class AnotherDynamicServiceFileDescriptorSupplier
      extends AnotherDynamicServiceBaseDescriptorSupplier {
    AnotherDynamicServiceFileDescriptorSupplier() {}
  }

  private static final class AnotherDynamicServiceMethodDescriptorSupplier
      extends AnotherDynamicServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    AnotherDynamicServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (AnotherDynamicServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new AnotherDynamicServiceFileDescriptorSupplier())
              .addMethod(getMethodMethod())
              .build();
        }
      }
    }
    return result;
  }
}
