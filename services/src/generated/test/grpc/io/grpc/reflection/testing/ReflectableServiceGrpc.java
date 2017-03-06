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
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler (version 1.3.0-SNAPSHOT)",
    comments = "Source: io/grpc/reflection/testing/reflection_test.proto")
public final class ReflectableServiceGrpc {

  private ReflectableServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.reflection.testing.ReflectableService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.reflection.testing.Request,
      io.grpc.reflection.testing.Reply> METHOD_METHOD =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.reflection.testing.ReflectableService", "Method"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.reflection.testing.Request.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.reflection.testing.Reply.getDefaultInstance()));

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static ReflectableServiceStub newStub(io.grpc.Channel channel) {
    return new ReflectableServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static ReflectableServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new ReflectableServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary and streaming output calls on the service
   */
  public static ReflectableServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new ReflectableServiceFutureStub(channel);
  }

  /**
   */
  public static abstract class ReflectableServiceImplBase implements io.grpc.BindableService {

    /**
     */
    public void method(io.grpc.reflection.testing.Request request,
        io.grpc.stub.StreamObserver<io.grpc.reflection.testing.Reply> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_METHOD, responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            METHOD_METHOD,
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.reflection.testing.Request,
                io.grpc.reflection.testing.Reply>(
                  this, METHODID_METHOD)))
          .build();
    }
  }

  /**
   */
  public static final class ReflectableServiceStub extends io.grpc.stub.AbstractStub<ReflectableServiceStub> {
    private ReflectableServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReflectableServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReflectableServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReflectableServiceStub(channel, callOptions);
    }

    /**
     */
    public void method(io.grpc.reflection.testing.Request request,
        io.grpc.stub.StreamObserver<io.grpc.reflection.testing.Reply> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_METHOD, getCallOptions()), request, responseObserver);
    }
  }

  /**
   */
  public static final class ReflectableServiceBlockingStub extends io.grpc.stub.AbstractStub<ReflectableServiceBlockingStub> {
    private ReflectableServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReflectableServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReflectableServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReflectableServiceBlockingStub(channel, callOptions);
    }

    /**
     */
    public io.grpc.reflection.testing.Reply method(io.grpc.reflection.testing.Request request) {
      return blockingUnaryCall(
          getChannel(), METHOD_METHOD, getCallOptions(), request);
    }
  }

  /**
   */
  public static final class ReflectableServiceFutureStub extends io.grpc.stub.AbstractStub<ReflectableServiceFutureStub> {
    private ReflectableServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ReflectableServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ReflectableServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ReflectableServiceFutureStub(channel, callOptions);
    }

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.reflection.testing.Reply> method(
        io.grpc.reflection.testing.Request request) {
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
    private final ReflectableServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(ReflectableServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_METHOD:
          serviceImpl.method((io.grpc.reflection.testing.Request) request,
              (io.grpc.stub.StreamObserver<io.grpc.reflection.testing.Reply>) responseObserver);
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

  private static final class ReflectableServiceDescriptorSupplier implements io.grpc.protobuf.ProtoFileDescriptorSupplier {
    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.reflection.testing.ReflectionTestProto.getDescriptor();
    }
  }

  private static volatile io.grpc.ServiceDescriptor serviceDescriptor;

  public static io.grpc.ServiceDescriptor getServiceDescriptor() {
    io.grpc.ServiceDescriptor result = serviceDescriptor;
    if (result == null) {
      synchronized (ReflectableServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new ReflectableServiceDescriptorSupplier())
              .addMethod(METHOD_METHOD)
              .build();
        }
      }
    }
    return result;
  }
}
