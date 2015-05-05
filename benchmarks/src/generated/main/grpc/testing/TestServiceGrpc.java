package grpc.testing;

import static io.grpc.stub.Calls.createMethodDescriptor;
import static io.grpc.stub.Calls.asyncUnaryCall;
import static io.grpc.stub.Calls.asyncServerStreamingCall;
import static io.grpc.stub.Calls.asyncClientStreamingCall;
import static io.grpc.stub.Calls.duplexStreamingCall;
import static io.grpc.stub.Calls.blockingUnaryCall;
import static io.grpc.stub.Calls.blockingServerStreamingCall;
import static io.grpc.stub.Calls.unaryFutureCall;
import static io.grpc.stub.ServerCalls.createMethodDefinition;
import static io.grpc.stub.ServerCalls.asyncUnaryRequestCall;
import static io.grpc.stub.ServerCalls.asyncStreamingRequestCall;

@javax.annotation.Generated("by gRPC proto compiler")
public class TestServiceGrpc {

  private static final io.grpc.stub.Method<grpc.testing.Qpstest.SimpleRequest,
      grpc.testing.Qpstest.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.UNARY, "UnaryCall",
          io.grpc.protobuf.ProtoUtils.marshaller(grpc.testing.Qpstest.SimpleRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(grpc.testing.Qpstest.SimpleResponse.PARSER));
  private static final io.grpc.stub.Method<grpc.testing.Qpstest.SimpleRequest,
      grpc.testing.Qpstest.SimpleResponse> METHOD_STREAMING_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "StreamingCall",
          io.grpc.protobuf.ProtoUtils.marshaller(grpc.testing.Qpstest.SimpleRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(grpc.testing.Qpstest.SimpleResponse.PARSER));

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
    public final io.grpc.MethodDescriptor<grpc.testing.Qpstest.SimpleRequest,
        grpc.testing.Qpstest.SimpleResponse> unaryCall;
    public final io.grpc.MethodDescriptor<grpc.testing.Qpstest.SimpleRequest,
        grpc.testing.Qpstest.SimpleResponse> streamingCall;

    private TestServiceServiceDescriptor() {
      unaryCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_UNARY_CALL);
      streamingCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_STREAMING_CALL);
    }

    @SuppressWarnings("unchecked")
    private TestServiceServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      unaryCall = (io.grpc.MethodDescriptor<grpc.testing.Qpstest.SimpleRequest,
          grpc.testing.Qpstest.SimpleResponse>) methodMap.get(
          CONFIG.unaryCall.getName());
      streamingCall = (io.grpc.MethodDescriptor<grpc.testing.Qpstest.SimpleRequest,
          grpc.testing.Qpstest.SimpleResponse>) methodMap.get(
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

    public void unaryCall(grpc.testing.Qpstest.SimpleRequest request,
        io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleResponse> responseObserver);

    public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleResponse> responseObserver);
  }

  public static interface TestServiceBlockingClient {

    public grpc.testing.Qpstest.SimpleResponse unaryCall(grpc.testing.Qpstest.SimpleRequest request);
  }

  public static interface TestServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<grpc.testing.Qpstest.SimpleResponse> unaryCall(
        grpc.testing.Qpstest.SimpleRequest request);
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
    public void unaryCall(grpc.testing.Qpstest.SimpleRequest request,
        io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.unaryCall), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleRequest> streamingCall(
        io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleResponse> responseObserver) {
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
    public grpc.testing.Qpstest.SimpleResponse unaryCall(grpc.testing.Qpstest.SimpleRequest request) {
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
    public com.google.common.util.concurrent.ListenableFuture<grpc.testing.Qpstest.SimpleResponse> unaryCall(
        grpc.testing.Qpstest.SimpleRequest request) {
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
                grpc.testing.Qpstest.SimpleRequest,
                grpc.testing.Qpstest.SimpleResponse>() {
              @java.lang.Override
              public void invoke(
                  grpc.testing.Qpstest.SimpleRequest request,
                  io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleResponse> responseObserver) {
                serviceImpl.unaryCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_STREAMING_CALL,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                grpc.testing.Qpstest.SimpleRequest,
                grpc.testing.Qpstest.SimpleResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleRequest> invoke(
                  io.grpc.stub.StreamObserver<grpc.testing.Qpstest.SimpleResponse> responseObserver) {
                return serviceImpl.streamingCall(responseObserver);
              }
            }))).build();
  }
}
