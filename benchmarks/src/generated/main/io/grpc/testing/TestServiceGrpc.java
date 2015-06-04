package io.grpc.testing;

import static io.grpc.stub.ClientCalls.createMethodDescriptor;
import static io.grpc.stub.ClientCalls.asyncUnaryCall;
import static io.grpc.stub.ClientCalls.asyncServerStreamingCall;
import static io.grpc.stub.ClientCalls.asyncClientStreamingCall;
import static io.grpc.stub.ClientCalls.duplexStreamingCall;
import static io.grpc.stub.ClientCalls.blockingUnaryCall;
import static io.grpc.stub.ClientCalls.blockingServerStreamingCall;
import static io.grpc.stub.ClientCalls.unaryFutureCall;
import static io.grpc.stub.ServerCalls.createMethodDefinition;
import static io.grpc.stub.ServerCalls.asyncUnaryRequestCall;
import static io.grpc.stub.ServerCalls.asyncStreamingRequestCall;

@javax.annotation.Generated("by gRPC proto compiler")
public class TestServiceGrpc {

  private static final io.grpc.stub.Method<io.grpc.testing.SimpleRequest,
      io.grpc.testing.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.UNARY, "UnaryCall",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.SimpleRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.SimpleResponse.PARSER));
  private static final io.grpc.stub.Method<io.grpc.testing.SimpleRequest,
      io.grpc.testing.SimpleResponse> METHOD_STREAMING_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "StreamingCall",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.SimpleRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.testing.SimpleResponse.PARSER));

  public static TestServiceStub newStub(io.grpc.Channel channel) {
    return new TestServiceStub(channel, CONFIG);
  }

  public static TestServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new TestServiceBlockingStub(channel, CONFIG);
  }

  public static TestServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new TestServiceFutureStub(channel, CONFIG);
  }

  public static final TestServiceServiceDescriptor CONFIG =
      new TestServiceServiceDescriptor();

  @javax.annotation.concurrent.Immutable
  public static class TestServiceServiceDescriptor extends
      io.grpc.stub.AbstractServiceDescriptor<TestServiceServiceDescriptor> {
    public final io.grpc.MethodDescriptor<io.grpc.testing.SimpleRequest,
        io.grpc.testing.SimpleResponse> unaryCall;
    public final io.grpc.MethodDescriptor<io.grpc.testing.SimpleRequest,
        io.grpc.testing.SimpleResponse> streamingCall;

    private TestServiceServiceDescriptor() {
      unaryCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_UNARY_CALL);
      streamingCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_STREAMING_CALL);
    }

    @SuppressWarnings("unchecked")
    private TestServiceServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      unaryCall = (io.grpc.MethodDescriptor<io.grpc.testing.SimpleRequest,
          io.grpc.testing.SimpleResponse>) methodMap.get(
          CONFIG.unaryCall.getName());
      streamingCall = (io.grpc.MethodDescriptor<io.grpc.testing.SimpleRequest,
          io.grpc.testing.SimpleResponse>) methodMap.get(
          CONFIG.streamingCall.getName());
    }

    @java.lang.Override
    protected TestServiceServiceDescriptor build(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      return new TestServiceServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public com.google.common.collect.ImmutableList<io.grpc.MethodDescriptor<?, ?>> methods() {
      return com.google.common.collect.ImmutableList.<io.grpc.MethodDescriptor<?, ?>>of(
          unaryCall,
          streamingCall);
    }
  }

  public static interface TestService {

    public void unaryCall(io.grpc.testing.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver);
  }

  public static interface TestServiceBlockingClient {

    public io.grpc.testing.SimpleResponse unaryCall(io.grpc.testing.SimpleRequest request);
  }

  public static interface TestServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.SimpleResponse> unaryCall(
        io.grpc.testing.SimpleRequest request);
  }

  public static class TestServiceStub extends
      io.grpc.stub.AbstractStub<TestServiceStub, TestServiceServiceDescriptor>
      implements TestService {
    private TestServiceStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceStub(channel, config);
    }

    @java.lang.Override
    public void unaryCall(io.grpc.testing.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.unaryCall), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.streamingCall), responseObserver);
    }
  }

  public static class TestServiceBlockingStub extends
      io.grpc.stub.AbstractStub<TestServiceBlockingStub, TestServiceServiceDescriptor>
      implements TestServiceBlockingClient {
    private TestServiceBlockingStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceBlockingStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceBlockingStub(channel, config);
    }

    @java.lang.Override
    public io.grpc.testing.SimpleResponse unaryCall(io.grpc.testing.SimpleRequest request) {
      return blockingUnaryCall(
          channel.newCall(config.unaryCall), request);
    }
  }

  public static class TestServiceFutureStub extends
      io.grpc.stub.AbstractStub<TestServiceFutureStub, TestServiceServiceDescriptor>
      implements TestServiceFutureClient {
    private TestServiceFutureStub(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceFutureStub build(io.grpc.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceFutureStub(channel, config);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.SimpleResponse> unaryCall(
        io.grpc.testing.SimpleRequest request) {
      return unaryFutureCall(
          channel.newCall(config.unaryCall), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder("grpc.testing.TestService")
      .addMethod(createMethodDefinition(
          METHOD_UNARY_CALL,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.testing.SimpleRequest,
                io.grpc.testing.SimpleResponse>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.testing.SimpleRequest request,
                  io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver) {
                serviceImpl.unaryCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_STREAMING_CALL,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                io.grpc.testing.SimpleRequest,
                io.grpc.testing.SimpleResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.testing.SimpleRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.testing.SimpleResponse> responseObserver) {
                return serviceImpl.streamingCall(responseObserver);
              }
            }))).build();
  }
}
