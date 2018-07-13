package io.grpc.alts.internal;

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
    comments = "Source: grpc/gcp/handshaker.proto")
public final class HandshakerServiceGrpc {

  private HandshakerServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.gcp.HandshakerService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.alts.internal.Handshaker.HandshakerReq,
      io.grpc.alts.internal.Handshaker.HandshakerResp> getDoHandshakeMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "DoHandshake",
      requestType = io.grpc.alts.internal.Handshaker.HandshakerReq.class,
      responseType = io.grpc.alts.internal.Handshaker.HandshakerResp.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.alts.internal.Handshaker.HandshakerReq,
      io.grpc.alts.internal.Handshaker.HandshakerResp> getDoHandshakeMethod() {
    io.grpc.MethodDescriptor<io.grpc.alts.internal.Handshaker.HandshakerReq, io.grpc.alts.internal.Handshaker.HandshakerResp> getDoHandshakeMethod;
    if ((getDoHandshakeMethod = HandshakerServiceGrpc.getDoHandshakeMethod) == null) {
      synchronized (HandshakerServiceGrpc.class) {
        if ((getDoHandshakeMethod = HandshakerServiceGrpc.getDoHandshakeMethod) == null) {
          HandshakerServiceGrpc.getDoHandshakeMethod = getDoHandshakeMethod = 
              io.grpc.MethodDescriptor.<io.grpc.alts.internal.Handshaker.HandshakerReq, io.grpc.alts.internal.Handshaker.HandshakerResp>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.gcp.HandshakerService", "DoHandshake"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.alts.internal.Handshaker.HandshakerReq.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.alts.internal.Handshaker.HandshakerResp.getDefaultInstance()))
                  .setSchemaDescriptor(new HandshakerServiceMethodDescriptorSupplier("DoHandshake"))
                  .build();
          }
        }
     }
     return getDoHandshakeMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static HandshakerServiceStub newStub(io.grpc.Channel channel) {
    return new HandshakerServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static HandshakerServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new HandshakerServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static HandshakerServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new HandshakerServiceFutureStub(channel);
  }

  /**
   */
  public static abstract class HandshakerServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * Handshaker service accepts a stream of handshaker request, returning a
     * stream of handshaker response. Client is expected to send exactly one
     * message with either client_start or server_start followed by one or more
     * messages with next. Each time client sends a request, the handshaker
     * service expects to respond. Client does not have to wait for service's
     * response before sending next request.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.alts.internal.Handshaker.HandshakerReq> doHandshake(
        io.grpc.stub.StreamObserver<io.grpc.alts.internal.Handshaker.HandshakerResp> responseObserver) {
      return asyncUnimplementedStreamingCall(getDoHandshakeMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getDoHandshakeMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.alts.internal.Handshaker.HandshakerReq,
                io.grpc.alts.internal.Handshaker.HandshakerResp>(
                  this, METHODID_DO_HANDSHAKE)))
          .build();
    }
  }

  /**
   */
  public static final class HandshakerServiceStub extends io.grpc.stub.AbstractStub<HandshakerServiceStub> {
    private HandshakerServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private HandshakerServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HandshakerServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new HandshakerServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * Handshaker service accepts a stream of handshaker request, returning a
     * stream of handshaker response. Client is expected to send exactly one
     * message with either client_start or server_start followed by one or more
     * messages with next. Each time client sends a request, the handshaker
     * service expects to respond. Client does not have to wait for service's
     * response before sending next request.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.alts.internal.Handshaker.HandshakerReq> doHandshake(
        io.grpc.stub.StreamObserver<io.grpc.alts.internal.Handshaker.HandshakerResp> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getDoHandshakeMethod(), getCallOptions()), responseObserver);
    }
  }

  /**
   */
  public static final class HandshakerServiceBlockingStub extends io.grpc.stub.AbstractStub<HandshakerServiceBlockingStub> {
    private HandshakerServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private HandshakerServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HandshakerServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new HandshakerServiceBlockingStub(channel, callOptions);
    }
  }

  /**
   */
  public static final class HandshakerServiceFutureStub extends io.grpc.stub.AbstractStub<HandshakerServiceFutureStub> {
    private HandshakerServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private HandshakerServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HandshakerServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new HandshakerServiceFutureStub(channel, callOptions);
    }
  }

  private static final int METHODID_DO_HANDSHAKE = 0;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final HandshakerServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(HandshakerServiceImplBase serviceImpl, int methodId) {
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
        case METHODID_DO_HANDSHAKE:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.doHandshake(
              (io.grpc.stub.StreamObserver<io.grpc.alts.internal.Handshaker.HandshakerResp>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class HandshakerServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    HandshakerServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.alts.internal.Handshaker.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("HandshakerService");
    }
  }

  private static final class HandshakerServiceFileDescriptorSupplier
      extends HandshakerServiceBaseDescriptorSupplier {
    HandshakerServiceFileDescriptorSupplier() {}
  }

  private static final class HandshakerServiceMethodDescriptorSupplier
      extends HandshakerServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    HandshakerServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (HandshakerServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new HandshakerServiceFileDescriptorSupplier())
              .addMethod(getDoHandshakeMethod())
              .build();
        }
      }
    }
    return result;
  }
}
