package io.grpc.testing.protobuf;

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
 * A simple service for test.
 * </pre>
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: io/grpc/testing/protobuf/simpleservice.proto")
public final class SimpleServiceGrpc {

  private SimpleServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.SimpleService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest,
      io.grpc.testing.protobuf.SimpleResponse> getUnaryRpcMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "UnaryRpc",
      requestType = io.grpc.testing.protobuf.SimpleRequest.class,
      responseType = io.grpc.testing.protobuf.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest,
      io.grpc.testing.protobuf.SimpleResponse> getUnaryRpcMethod() {
    io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest, io.grpc.testing.protobuf.SimpleResponse> getUnaryRpcMethod;
    if ((getUnaryRpcMethod = SimpleServiceGrpc.getUnaryRpcMethod) == null) {
      synchronized (SimpleServiceGrpc.class) {
        if ((getUnaryRpcMethod = SimpleServiceGrpc.getUnaryRpcMethod) == null) {
          SimpleServiceGrpc.getUnaryRpcMethod = getUnaryRpcMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.protobuf.SimpleRequest, io.grpc.testing.protobuf.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.SimpleService", "UnaryRpc"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.protobuf.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.protobuf.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new SimpleServiceMethodDescriptorSupplier("UnaryRpc"))
                  .build();
          }
        }
     }
     return getUnaryRpcMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest,
      io.grpc.testing.protobuf.SimpleResponse> getClientStreamingRpcMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "ClientStreamingRpc",
      requestType = io.grpc.testing.protobuf.SimpleRequest.class,
      responseType = io.grpc.testing.protobuf.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest,
      io.grpc.testing.protobuf.SimpleResponse> getClientStreamingRpcMethod() {
    io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest, io.grpc.testing.protobuf.SimpleResponse> getClientStreamingRpcMethod;
    if ((getClientStreamingRpcMethod = SimpleServiceGrpc.getClientStreamingRpcMethod) == null) {
      synchronized (SimpleServiceGrpc.class) {
        if ((getClientStreamingRpcMethod = SimpleServiceGrpc.getClientStreamingRpcMethod) == null) {
          SimpleServiceGrpc.getClientStreamingRpcMethod = getClientStreamingRpcMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.protobuf.SimpleRequest, io.grpc.testing.protobuf.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.SimpleService", "ClientStreamingRpc"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.protobuf.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.protobuf.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new SimpleServiceMethodDescriptorSupplier("ClientStreamingRpc"))
                  .build();
          }
        }
     }
     return getClientStreamingRpcMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest,
      io.grpc.testing.protobuf.SimpleResponse> getServerStreamingRpcMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "ServerStreamingRpc",
      requestType = io.grpc.testing.protobuf.SimpleRequest.class,
      responseType = io.grpc.testing.protobuf.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest,
      io.grpc.testing.protobuf.SimpleResponse> getServerStreamingRpcMethod() {
    io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest, io.grpc.testing.protobuf.SimpleResponse> getServerStreamingRpcMethod;
    if ((getServerStreamingRpcMethod = SimpleServiceGrpc.getServerStreamingRpcMethod) == null) {
      synchronized (SimpleServiceGrpc.class) {
        if ((getServerStreamingRpcMethod = SimpleServiceGrpc.getServerStreamingRpcMethod) == null) {
          SimpleServiceGrpc.getServerStreamingRpcMethod = getServerStreamingRpcMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.protobuf.SimpleRequest, io.grpc.testing.protobuf.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.SimpleService", "ServerStreamingRpc"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.protobuf.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.protobuf.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new SimpleServiceMethodDescriptorSupplier("ServerStreamingRpc"))
                  .build();
          }
        }
     }
     return getServerStreamingRpcMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest,
      io.grpc.testing.protobuf.SimpleResponse> getBidiStreamingRpcMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "BidiStreamingRpc",
      requestType = io.grpc.testing.protobuf.SimpleRequest.class,
      responseType = io.grpc.testing.protobuf.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest,
      io.grpc.testing.protobuf.SimpleResponse> getBidiStreamingRpcMethod() {
    io.grpc.MethodDescriptor<io.grpc.testing.protobuf.SimpleRequest, io.grpc.testing.protobuf.SimpleResponse> getBidiStreamingRpcMethod;
    if ((getBidiStreamingRpcMethod = SimpleServiceGrpc.getBidiStreamingRpcMethod) == null) {
      synchronized (SimpleServiceGrpc.class) {
        if ((getBidiStreamingRpcMethod = SimpleServiceGrpc.getBidiStreamingRpcMethod) == null) {
          SimpleServiceGrpc.getBidiStreamingRpcMethod = getBidiStreamingRpcMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.protobuf.SimpleRequest, io.grpc.testing.protobuf.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.SimpleService", "BidiStreamingRpc"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.protobuf.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.protobuf.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new SimpleServiceMethodDescriptorSupplier("BidiStreamingRpc"))
                  .build();
          }
        }
     }
     return getBidiStreamingRpcMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static SimpleServiceStub newStub(io.grpc.Channel channel) {
    return new SimpleServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static SimpleServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new SimpleServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static SimpleServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new SimpleServiceFutureStub(channel);
  }

  /**
   * <pre>
   * A simple service for test.
   * </pre>
   */
  public static abstract class SimpleServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * Simple unary RPC.
     * </pre>
     */
    public void unaryRpc(io.grpc.testing.protobuf.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getUnaryRpcMethod(), responseObserver);
    }

    /**
     * <pre>
     * Simple client-to-server streaming RPC.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleRequest> clientStreamingRpc(
        io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getClientStreamingRpcMethod(), responseObserver);
    }

    /**
     * <pre>
     * Simple server-to-client streaming RPC.
     * </pre>
     */
    public void serverStreamingRpc(io.grpc.testing.protobuf.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getServerStreamingRpcMethod(), responseObserver);
    }

    /**
     * <pre>
     * Simple bidirectional streaming RPC.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleRequest> bidiStreamingRpc(
        io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getBidiStreamingRpcMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getUnaryRpcMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.testing.protobuf.SimpleRequest,
                io.grpc.testing.protobuf.SimpleResponse>(
                  this, METHODID_UNARY_RPC)))
          .addMethod(
            getClientStreamingRpcMethod(),
            asyncClientStreamingCall(
              new MethodHandlers<
                io.grpc.testing.protobuf.SimpleRequest,
                io.grpc.testing.protobuf.SimpleResponse>(
                  this, METHODID_CLIENT_STREAMING_RPC)))
          .addMethod(
            getServerStreamingRpcMethod(),
            asyncServerStreamingCall(
              new MethodHandlers<
                io.grpc.testing.protobuf.SimpleRequest,
                io.grpc.testing.protobuf.SimpleResponse>(
                  this, METHODID_SERVER_STREAMING_RPC)))
          .addMethod(
            getBidiStreamingRpcMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.testing.protobuf.SimpleRequest,
                io.grpc.testing.protobuf.SimpleResponse>(
                  this, METHODID_BIDI_STREAMING_RPC)))
          .build();
    }
  }

  /**
   * <pre>
   * A simple service for test.
   * </pre>
   */
  public static final class SimpleServiceStub extends io.grpc.stub.AbstractStub<SimpleServiceStub> {
    private SimpleServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private SimpleServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected SimpleServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new SimpleServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * Simple unary RPC.
     * </pre>
     */
    public void unaryRpc(io.grpc.testing.protobuf.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getUnaryRpcMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Simple client-to-server streaming RPC.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleRequest> clientStreamingRpc(
        io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse> responseObserver) {
      return asyncClientStreamingCall(
          getChannel().newCall(getClientStreamingRpcMethod(), getCallOptions()), responseObserver);
    }

    /**
     * <pre>
     * Simple server-to-client streaming RPC.
     * </pre>
     */
    public void serverStreamingRpc(io.grpc.testing.protobuf.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(getServerStreamingRpcMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Simple bidirectional streaming RPC.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleRequest> bidiStreamingRpc(
        io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getBidiStreamingRpcMethod(), getCallOptions()), responseObserver);
    }
  }

  /**
   * <pre>
   * A simple service for test.
   * </pre>
   */
  public static final class SimpleServiceBlockingStub extends io.grpc.stub.AbstractStub<SimpleServiceBlockingStub> {
    private SimpleServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private SimpleServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected SimpleServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new SimpleServiceBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * Simple unary RPC.
     * </pre>
     */
    public io.grpc.testing.protobuf.SimpleResponse unaryRpc(io.grpc.testing.protobuf.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel(), getUnaryRpcMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Simple server-to-client streaming RPC.
     * </pre>
     */
    public java.util.Iterator<io.grpc.testing.protobuf.SimpleResponse> serverStreamingRpc(
        io.grpc.testing.protobuf.SimpleRequest request) {
      return blockingServerStreamingCall(
          getChannel(), getServerStreamingRpcMethod(), getCallOptions(), request);
    }
  }

  /**
   * <pre>
   * A simple service for test.
   * </pre>
   */
  public static final class SimpleServiceFutureStub extends io.grpc.stub.AbstractStub<SimpleServiceFutureStub> {
    private SimpleServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private SimpleServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected SimpleServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new SimpleServiceFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * Simple unary RPC.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.protobuf.SimpleResponse> unaryRpc(
        io.grpc.testing.protobuf.SimpleRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getUnaryRpcMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_UNARY_RPC = 0;
  private static final int METHODID_SERVER_STREAMING_RPC = 1;
  private static final int METHODID_CLIENT_STREAMING_RPC = 2;
  private static final int METHODID_BIDI_STREAMING_RPC = 3;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final SimpleServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(SimpleServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_UNARY_RPC:
          serviceImpl.unaryRpc((io.grpc.testing.protobuf.SimpleRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse>) responseObserver);
          break;
        case METHODID_SERVER_STREAMING_RPC:
          serviceImpl.serverStreamingRpc((io.grpc.testing.protobuf.SimpleRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse>) responseObserver);
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
        case METHODID_CLIENT_STREAMING_RPC:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.clientStreamingRpc(
              (io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse>) responseObserver);
        case METHODID_BIDI_STREAMING_RPC:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.bidiStreamingRpc(
              (io.grpc.stub.StreamObserver<io.grpc.testing.protobuf.SimpleResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class SimpleServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    SimpleServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.testing.protobuf.SimpleServiceProto.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("SimpleService");
    }
  }

  private static final class SimpleServiceFileDescriptorSupplier
      extends SimpleServiceBaseDescriptorSupplier {
    SimpleServiceFileDescriptorSupplier() {}
  }

  private static final class SimpleServiceMethodDescriptorSupplier
      extends SimpleServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    SimpleServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (SimpleServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new SimpleServiceFileDescriptorSupplier())
              .addMethod(getUnaryRpcMethod())
              .addMethod(getClientStreamingRpcMethod())
              .addMethod(getServerStreamingRpcMethod())
              .addMethod(getBidiStreamingRpcMethod())
              .build();
        }
      }
    }
    return result;
  }
}
