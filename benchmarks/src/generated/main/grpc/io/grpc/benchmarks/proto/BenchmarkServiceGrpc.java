package io.grpc.benchmarks.proto;

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
    comments = "Source: grpc/testing/services.proto")
public final class BenchmarkServiceGrpc {

  private BenchmarkServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.BenchmarkService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getUnaryCallMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "UnaryCall",
      requestType = io.grpc.benchmarks.proto.Messages.SimpleRequest.class,
      responseType = io.grpc.benchmarks.proto.Messages.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getUnaryCallMethod() {
    io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse> getUnaryCallMethod;
    if ((getUnaryCallMethod = BenchmarkServiceGrpc.getUnaryCallMethod) == null) {
      synchronized (BenchmarkServiceGrpc.class) {
        if ((getUnaryCallMethod = BenchmarkServiceGrpc.getUnaryCallMethod) == null) {
          BenchmarkServiceGrpc.getUnaryCallMethod = getUnaryCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.BenchmarkService", "UnaryCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("UnaryCall"))
                  .build();
          }
        }
     }
     return getUnaryCallMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingCallMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "StreamingCall",
      requestType = io.grpc.benchmarks.proto.Messages.SimpleRequest.class,
      responseType = io.grpc.benchmarks.proto.Messages.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingCallMethod() {
    io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingCallMethod;
    if ((getStreamingCallMethod = BenchmarkServiceGrpc.getStreamingCallMethod) == null) {
      synchronized (BenchmarkServiceGrpc.class) {
        if ((getStreamingCallMethod = BenchmarkServiceGrpc.getStreamingCallMethod) == null) {
          BenchmarkServiceGrpc.getStreamingCallMethod = getStreamingCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.BenchmarkService", "StreamingCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("StreamingCall"))
                  .build();
          }
        }
     }
     return getStreamingCallMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingFromClientMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "StreamingFromClient",
      requestType = io.grpc.benchmarks.proto.Messages.SimpleRequest.class,
      responseType = io.grpc.benchmarks.proto.Messages.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingFromClientMethod() {
    io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingFromClientMethod;
    if ((getStreamingFromClientMethod = BenchmarkServiceGrpc.getStreamingFromClientMethod) == null) {
      synchronized (BenchmarkServiceGrpc.class) {
        if ((getStreamingFromClientMethod = BenchmarkServiceGrpc.getStreamingFromClientMethod) == null) {
          BenchmarkServiceGrpc.getStreamingFromClientMethod = getStreamingFromClientMethod = 
              io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.BenchmarkService", "StreamingFromClient"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("StreamingFromClient"))
                  .build();
          }
        }
     }
     return getStreamingFromClientMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingFromServerMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "StreamingFromServer",
      requestType = io.grpc.benchmarks.proto.Messages.SimpleRequest.class,
      responseType = io.grpc.benchmarks.proto.Messages.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingFromServerMethod() {
    io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingFromServerMethod;
    if ((getStreamingFromServerMethod = BenchmarkServiceGrpc.getStreamingFromServerMethod) == null) {
      synchronized (BenchmarkServiceGrpc.class) {
        if ((getStreamingFromServerMethod = BenchmarkServiceGrpc.getStreamingFromServerMethod) == null) {
          BenchmarkServiceGrpc.getStreamingFromServerMethod = getStreamingFromServerMethod = 
              io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.BenchmarkService", "StreamingFromServer"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("StreamingFromServer"))
                  .build();
          }
        }
     }
     return getStreamingFromServerMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingBothWaysMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "StreamingBothWays",
      requestType = io.grpc.benchmarks.proto.Messages.SimpleRequest.class,
      responseType = io.grpc.benchmarks.proto.Messages.SimpleResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingBothWaysMethod() {
    io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse> getStreamingBothWaysMethod;
    if ((getStreamingBothWaysMethod = BenchmarkServiceGrpc.getStreamingBothWaysMethod) == null) {
      synchronized (BenchmarkServiceGrpc.class) {
        if ((getStreamingBothWaysMethod = BenchmarkServiceGrpc.getStreamingBothWaysMethod) == null) {
          BenchmarkServiceGrpc.getStreamingBothWaysMethod = getStreamingBothWaysMethod = 
              io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.BenchmarkService", "StreamingBothWays"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("StreamingBothWays"))
                  .build();
          }
        }
     }
     return getStreamingBothWaysMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static BenchmarkServiceStub newStub(io.grpc.Channel channel) {
    return new BenchmarkServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static BenchmarkServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new BenchmarkServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static BenchmarkServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new BenchmarkServiceFutureStub(channel);
  }

  /**
   */
  public static abstract class BenchmarkServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * One request followed by one response.
     * The server returns the client payload as-is.
     * </pre>
     */
    public void unaryCall(io.grpc.benchmarks.proto.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getUnaryCallMethod(), responseObserver);
    }

    /**
     * <pre>
     * Repeated sequence of one request followed by one response.
     * Should be called streaming ping-pong
     * The server returns the client payload as-is on each response
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getStreamingCallMethod(), responseObserver);
    }

    /**
     * <pre>
     * Single-sided unbounded streaming from client to server
     * The server returns the client payload as-is once the client does WritesDone
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingFromClient(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getStreamingFromClientMethod(), responseObserver);
    }

    /**
     * <pre>
     * Single-sided unbounded streaming from server to client
     * The server repeatedly returns the client payload as-is
     * </pre>
     */
    public void streamingFromServer(io.grpc.benchmarks.proto.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getStreamingFromServerMethod(), responseObserver);
    }

    /**
     * <pre>
     * Two-sided unbounded streaming between server to client
     * Both sides send the content of their own choice to the other
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingBothWays(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getStreamingBothWaysMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getUnaryCallMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_UNARY_CALL)))
          .addMethod(
            getStreamingCallMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_STREAMING_CALL)))
          .addMethod(
            getStreamingFromClientMethod(),
            asyncClientStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_STREAMING_FROM_CLIENT)))
          .addMethod(
            getStreamingFromServerMethod(),
            asyncServerStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_STREAMING_FROM_SERVER)))
          .addMethod(
            getStreamingBothWaysMethod(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_STREAMING_BOTH_WAYS)))
          .build();
    }
  }

  /**
   */
  public static final class BenchmarkServiceStub extends io.grpc.stub.AbstractStub<BenchmarkServiceStub> {
    private BenchmarkServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private BenchmarkServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected BenchmarkServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new BenchmarkServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * One request followed by one response.
     * The server returns the client payload as-is.
     * </pre>
     */
    public void unaryCall(io.grpc.benchmarks.proto.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getUnaryCallMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Repeated sequence of one request followed by one response.
     * Should be called streaming ping-pong
     * The server returns the client payload as-is on each response
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getStreamingCallMethod(), getCallOptions()), responseObserver);
    }

    /**
     * <pre>
     * Single-sided unbounded streaming from client to server
     * The server returns the client payload as-is once the client does WritesDone
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingFromClient(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncClientStreamingCall(
          getChannel().newCall(getStreamingFromClientMethod(), getCallOptions()), responseObserver);
    }

    /**
     * <pre>
     * Single-sided unbounded streaming from server to client
     * The server repeatedly returns the client payload as-is
     * </pre>
     */
    public void streamingFromServer(io.grpc.benchmarks.proto.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(getStreamingFromServerMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Two-sided unbounded streaming between server to client
     * Both sides send the content of their own choice to the other
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingBothWays(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getStreamingBothWaysMethod(), getCallOptions()), responseObserver);
    }
  }

  /**
   */
  public static final class BenchmarkServiceBlockingStub extends io.grpc.stub.AbstractStub<BenchmarkServiceBlockingStub> {
    private BenchmarkServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private BenchmarkServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected BenchmarkServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new BenchmarkServiceBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * One request followed by one response.
     * The server returns the client payload as-is.
     * </pre>
     */
    public io.grpc.benchmarks.proto.Messages.SimpleResponse unaryCall(io.grpc.benchmarks.proto.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel(), getUnaryCallMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Single-sided unbounded streaming from server to client
     * The server repeatedly returns the client payload as-is
     * </pre>
     */
    public java.util.Iterator<io.grpc.benchmarks.proto.Messages.SimpleResponse> streamingFromServer(
        io.grpc.benchmarks.proto.Messages.SimpleRequest request) {
      return blockingServerStreamingCall(
          getChannel(), getStreamingFromServerMethod(), getCallOptions(), request);
    }
  }

  /**
   */
  public static final class BenchmarkServiceFutureStub extends io.grpc.stub.AbstractStub<BenchmarkServiceFutureStub> {
    private BenchmarkServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private BenchmarkServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected BenchmarkServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new BenchmarkServiceFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * One request followed by one response.
     * The server returns the client payload as-is.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Messages.SimpleResponse> unaryCall(
        io.grpc.benchmarks.proto.Messages.SimpleRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getUnaryCallMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_UNARY_CALL = 0;
  private static final int METHODID_STREAMING_FROM_SERVER = 1;
  private static final int METHODID_STREAMING_CALL = 2;
  private static final int METHODID_STREAMING_FROM_CLIENT = 3;
  private static final int METHODID_STREAMING_BOTH_WAYS = 4;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final BenchmarkServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(BenchmarkServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_UNARY_CALL:
          serviceImpl.unaryCall((io.grpc.benchmarks.proto.Messages.SimpleRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse>) responseObserver);
          break;
        case METHODID_STREAMING_FROM_SERVER:
          serviceImpl.streamingFromServer((io.grpc.benchmarks.proto.Messages.SimpleRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse>) responseObserver);
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
        case METHODID_STREAMING_CALL:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.streamingCall(
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse>) responseObserver);
        case METHODID_STREAMING_FROM_CLIENT:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.streamingFromClient(
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse>) responseObserver);
        case METHODID_STREAMING_BOTH_WAYS:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.streamingBothWays(
              (io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class BenchmarkServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    BenchmarkServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.benchmarks.proto.Services.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("BenchmarkService");
    }
  }

  private static final class BenchmarkServiceFileDescriptorSupplier
      extends BenchmarkServiceBaseDescriptorSupplier {
    BenchmarkServiceFileDescriptorSupplier() {}
  }

  private static final class BenchmarkServiceMethodDescriptorSupplier
      extends BenchmarkServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    BenchmarkServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (BenchmarkServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new BenchmarkServiceFileDescriptorSupplier())
              .addMethod(getUnaryCallMethod())
              .addMethod(getStreamingCallMethod())
              .addMethod(getStreamingFromClientMethod())
              .addMethod(getStreamingFromServerMethod())
              .addMethod(getStreamingBothWaysMethod())
              .build();
        }
      }
    }
    return result;
  }
}
