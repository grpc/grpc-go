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
    value = "by gRPC proto compiler (version 0.15.0-SNAPSHOT)",
    comments = "Source: services.proto")
public class BenchmarkServiceGrpc {

  private BenchmarkServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.BenchmarkService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.BenchmarkService", "UnaryCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()));
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static final io.grpc.MethodDescriptor<io.grpc.benchmarks.proto.Messages.SimpleRequest,
      io.grpc.benchmarks.proto.Messages.SimpleResponse> METHOD_STREAMING_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.BenchmarkService", "StreamingCall"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Messages.SimpleRequest.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.benchmarks.proto.Messages.SimpleResponse.getDefaultInstance()));

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
   * Creates a new ListenableFuture-style stub that supports unary and streaming output calls on the service
   */
  public static BenchmarkServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new BenchmarkServiceFutureStub(channel);
  }

  /**
   */
  public static interface BenchmarkService {

    /**
     * <pre>
     * One request followed by one response.
     * The server returns the client payload as-is.
     * </pre>
     */
    public void unaryCall(io.grpc.benchmarks.proto.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver);

    /**
     * <pre>
     * One request followed by one response.
     * The server returns the client payload as-is.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver);
  }

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1469")
  public static abstract class AbstractBenchmarkService implements BenchmarkService, io.grpc.BindableService {

    @java.lang.Override
    public void unaryCall(io.grpc.benchmarks.proto.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(METHOD_UNARY_CALL, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(METHOD_STREAMING_CALL, responseObserver);
    }

    @java.lang.Override public io.grpc.ServerServiceDefinition bindService() {
      return BenchmarkServiceGrpc.bindService(this);
    }
  }

  /**
   */
  public static interface BenchmarkServiceBlockingClient {

    /**
     * <pre>
     * One request followed by one response.
     * The server returns the client payload as-is.
     * </pre>
     */
    public io.grpc.benchmarks.proto.Messages.SimpleResponse unaryCall(io.grpc.benchmarks.proto.Messages.SimpleRequest request);
  }

  /**
   */
  public static interface BenchmarkServiceFutureClient {

    /**
     * <pre>
     * One request followed by one response.
     * The server returns the client payload as-is.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Messages.SimpleResponse> unaryCall(
        io.grpc.benchmarks.proto.Messages.SimpleRequest request);
  }

  public static class BenchmarkServiceStub extends io.grpc.stub.AbstractStub<BenchmarkServiceStub>
      implements BenchmarkService {
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

    @java.lang.Override
    public void unaryCall(io.grpc.benchmarks.proto.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.benchmarks.proto.Messages.SimpleResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_STREAMING_CALL, getCallOptions()), responseObserver);
    }
  }

  public static class BenchmarkServiceBlockingStub extends io.grpc.stub.AbstractStub<BenchmarkServiceBlockingStub>
      implements BenchmarkServiceBlockingClient {
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

    @java.lang.Override
    public io.grpc.benchmarks.proto.Messages.SimpleResponse unaryCall(io.grpc.benchmarks.proto.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel(), METHOD_UNARY_CALL, getCallOptions(), request);
    }
  }

  public static class BenchmarkServiceFutureStub extends io.grpc.stub.AbstractStub<BenchmarkServiceFutureStub>
      implements BenchmarkServiceFutureClient {
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

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.benchmarks.proto.Messages.SimpleResponse> unaryCall(
        io.grpc.benchmarks.proto.Messages.SimpleRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request);
    }
  }

  private static final int METHODID_UNARY_CALL = 0;
  private static final int METHODID_STREAMING_CALL = 1;

  private static class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final BenchmarkService serviceImpl;
    private final int methodId;

    public MethodHandlers(BenchmarkService serviceImpl, int methodId) {
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
        default:
          throw new AssertionError();
      }
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final BenchmarkService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
        .addMethod(
          METHOD_UNARY_CALL,
          asyncUnaryCall(
            new MethodHandlers<
              io.grpc.benchmarks.proto.Messages.SimpleRequest,
              io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                serviceImpl, METHODID_UNARY_CALL)))
        .addMethod(
          METHOD_STREAMING_CALL,
          asyncBidiStreamingCall(
            new MethodHandlers<
              io.grpc.benchmarks.proto.Messages.SimpleRequest,
              io.grpc.benchmarks.proto.Messages.SimpleResponse>(
                serviceImpl, METHODID_STREAMING_CALL)))
        .build();
  }
}
