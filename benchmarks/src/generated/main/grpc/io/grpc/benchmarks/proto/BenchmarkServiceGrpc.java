package io.grpc.benchmarks.proto;

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
    value = "by gRPC proto compiler",
    comments = "Source: services.proto")
public final class BenchmarkServiceGrpc {

  private BenchmarkServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.BenchmarkService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
          .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
          .setFullMethodName(generateFullMethodName(
              "grpc.testing.BenchmarkService", "UnaryCall"))
          .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
          .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
          .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("UnaryCall"))
          .build();
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> METHOD_STREAMING_CALL =
      io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
          .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
          .setFullMethodName(generateFullMethodName(
              "grpc.testing.BenchmarkService", "StreamingCall"))
          .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
          .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
          .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("StreamingCall"))
          .build();
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> METHOD_STREAMING_FROM_CLIENT =
      io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
          .setType(io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING)
          .setFullMethodName(generateFullMethodName(
              "grpc.testing.BenchmarkService", "StreamingFromClient"))
          .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
          .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
          .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("StreamingFromClient"))
          .build();
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> METHOD_STREAMING_FROM_SERVER =
      io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
          .setType(io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
          .setFullMethodName(generateFullMethodName(
              "grpc.testing.BenchmarkService", "StreamingFromServer"))
          .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
          .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
          .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("StreamingFromServer"))
          .build();
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> METHOD_STREAMING_BOTH_WAYS =
      io.grpc.MethodDescriptor.<io.grpc.benchmarks.proto.Messages.SimpleRequest, io.grpc.benchmarks.proto.Messages.SimpleResponse>newBuilder()
          .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
          .setFullMethodName(generateFullMethodName(
              "grpc.testing.BenchmarkService", "StreamingBothWays"))
          .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()))
          .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
              io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()))
          .setSchemaDescriptor(new BenchmarkServiceMethodDescriptorSupplier("StreamingBothWays"))
          .build();

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
      asyncUnimplementedUnaryCall(METHOD_UNARY_CALL, responseObserver);
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
      return asyncUnimplementedStreamingCall(METHOD_STREAMING_CALL, responseObserver);
    }

    /**
     * <pre>
     * Single-sided unbounded streaming from client to server
     * The server returns the client payload as-is once the client does WritesDone
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingFromClient(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_STREAMING_FROM_CLIENT, responseObserver);
    }

    /**
     * <pre>
     * Single-sided unbounded streaming from server to client
     * The server repeatedly returns the client payload as-is
     * </pre>
     */
    public void streamingFromServer(io.grpc.benchmarks.proto.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_STREAMING_FROM_SERVER, responseObserver);
    }

    /**
     * <pre>
     * Two-sided unbounded streaming between server to client
     * Both sides send the content of their own choice to the other
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingBothWays(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_STREAMING_BOTH_WAYS, responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            METHOD_UNARY_CALL,
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_UNARY_CALL)))
          .addMethod(
            METHOD_STREAMING_CALL,
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_STREAMING_CALL)))
          .addMethod(
            METHOD_STREAMING_FROM_CLIENT,
            asyncClientStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_STREAMING_FROM_CLIENT)))
          .addMethod(
            METHOD_STREAMING_FROM_SERVER,
            asyncServerStreamingCall(
              new MethodHandlers<
                io.grpc.benchmarks.proto.Messages.SimpleRequest,
                io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                  this, METHODID_STREAMING_FROM_SERVER)))
          .addMethod(
            METHOD_STREAMING_BOTH_WAYS,
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
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request, responseObserver);
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
          getChannel().newCall(METHOD_STREAMING_CALL, getCallOptions()), responseObserver);
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
          getChannel().newCall(METHOD_STREAMING_FROM_CLIENT, getCallOptions()), responseObserver);
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
          getChannel().newCall(METHOD_STREAMING_FROM_SERVER, getCallOptions()), request, responseObserver);
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
          getChannel().newCall(METHOD_STREAMING_BOTH_WAYS, getCallOptions()), responseObserver);
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
          getChannel(), METHOD_UNARY_CALL, getCallOptions(), request);
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
          getChannel(), METHOD_STREAMING_FROM_SERVER, getCallOptions(), request);
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
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request);
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
              .addMethod(METHOD_UNARY_CALL)
              .addMethod(METHOD_STREAMING_CALL)
              .addMethod(METHOD_STREAMING_FROM_CLIENT)
              .addMethod(METHOD_STREAMING_FROM_SERVER)
              .addMethod(METHOD_STREAMING_BOTH_WAYS)
              .build();
        }
      }
    }
    return result;
  }
}
