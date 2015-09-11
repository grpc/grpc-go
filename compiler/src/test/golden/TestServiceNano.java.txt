package io.grpc.testing.integration;

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

import java.io.IOException;

@javax.annotation.Generated("by gRPC proto compiler")
public class TestServiceGrpc {

  private TestServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.TestService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.nano.Test.SimpleRequest,
      io.grpc.testing.integration.nano.Test.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "grpc.testing.TestService", "UnaryCall"),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.SimpleRequest>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.SimpleRequest>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.SimpleRequest newInstance() {
                      return new io.grpc.testing.integration.nano.Test.SimpleRequest();
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.SimpleResponse>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.SimpleResponse>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.SimpleResponse newInstance() {
                      return new io.grpc.testing.integration.nano.Test.SimpleResponse();
                  }
          }));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest,
      io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> METHOD_STREAMING_OUTPUT_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "StreamingOutputCall"),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest newInstance() {
                      return new io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest();
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse newInstance() {
                      return new io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse();
                  }
          }));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.nano.Test.StreamingInputCallRequest,
      io.grpc.testing.integration.nano.Test.StreamingInputCallResponse> METHOD_STREAMING_INPUT_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "StreamingInputCall"),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.StreamingInputCallRequest>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.StreamingInputCallRequest>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.StreamingInputCallRequest newInstance() {
                      return new io.grpc.testing.integration.nano.Test.StreamingInputCallRequest();
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.StreamingInputCallResponse>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.StreamingInputCallResponse>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.StreamingInputCallResponse newInstance() {
                      return new io.grpc.testing.integration.nano.Test.StreamingInputCallResponse();
                  }
          }));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest,
      io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> METHOD_FULL_BIDI_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "FullBidiCall"),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest newInstance() {
                      return new io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest();
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse newInstance() {
                      return new io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse();
                  }
          }));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest,
      io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> METHOD_HALF_BIDI_CALL =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "grpc.testing.TestService", "HalfBidiCall"),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest newInstance() {
                      return new io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest();
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>marshaller(
              new io.grpc.protobuf.nano.MessageNanoFactory<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>() {
                  @Override
                  public io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse newInstance() {
                      return new io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse();
                  }
          }));

  public static TestServiceStub newStub(io.grpc.Channel channel) {
    return new TestServiceStub(channel);
  }

  public static TestServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new TestServiceBlockingStub(channel);
  }

  public static TestServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new TestServiceFutureStub(channel);
  }

  public static interface TestService {

    public void unaryCall(io.grpc.testing.integration.nano.Test.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.SimpleResponse> responseObserver);

    public void streamingOutputCall(io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingInputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest> fullBidiCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest> halfBidiCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver);
  }

  public static interface TestServiceBlockingClient {

    public io.grpc.testing.integration.nano.Test.SimpleResponse unaryCall(io.grpc.testing.integration.nano.Test.SimpleRequest request);

    public java.util.Iterator<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest request);
  }

  public static interface TestServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.nano.Test.SimpleResponse> unaryCall(
        io.grpc.testing.integration.nano.Test.SimpleRequest request);
  }

  public static class TestServiceStub extends io.grpc.stub.AbstractStub<TestServiceStub>
      implements TestService {
    private TestServiceStub(io.grpc.Channel channel) {
      super(channel);
    }

    private TestServiceStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected TestServiceStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new TestServiceStub(channel, callOptions);
    }

    @java.lang.Override
    public void unaryCall(io.grpc.testing.integration.nano.Test.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public void streamingOutputCall(io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(METHOD_STREAMING_OUTPUT_CALL, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingInputCallResponse> responseObserver) {
      return asyncClientStreamingCall(
          getChannel().newCall(METHOD_STREAMING_INPUT_CALL, getCallOptions()), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest> fullBidiCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_FULL_BIDI_CALL, getCallOptions()), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest> halfBidiCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_HALF_BIDI_CALL, getCallOptions()), responseObserver);
    }
  }

  public static class TestServiceBlockingStub extends io.grpc.stub.AbstractStub<TestServiceBlockingStub>
      implements TestServiceBlockingClient {
    private TestServiceBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private TestServiceBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected TestServiceBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new TestServiceBlockingStub(channel, callOptions);
    }

    @java.lang.Override
    public io.grpc.testing.integration.nano.Test.SimpleResponse unaryCall(io.grpc.testing.integration.nano.Test.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest request) {
      return blockingServerStreamingCall(
          getChannel().newCall(METHOD_STREAMING_OUTPUT_CALL, getCallOptions()), request);
    }
  }

  public static class TestServiceFutureStub extends io.grpc.stub.AbstractStub<TestServiceFutureStub>
      implements TestServiceFutureClient {
    private TestServiceFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private TestServiceFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected TestServiceFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new TestServiceFutureStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.nano.Test.SimpleResponse> unaryCall(
        io.grpc.testing.integration.nano.Test.SimpleRequest request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_UNARY_CALL, getCallOptions()), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
      .addMethod(
        METHOD_UNARY_CALL,
        asyncUnaryCall(
          new io.grpc.stub.ServerCalls.UnaryMethod<
              io.grpc.testing.integration.nano.Test.SimpleRequest,
              io.grpc.testing.integration.nano.Test.SimpleResponse>() {
            @java.lang.Override
            public void invoke(
                io.grpc.testing.integration.nano.Test.SimpleRequest request,
                io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.SimpleResponse> responseObserver) {
              serviceImpl.unaryCall(request, responseObserver);
            }
          }))
      .addMethod(
        METHOD_STREAMING_OUTPUT_CALL,
        asyncServerStreamingCall(
          new io.grpc.stub.ServerCalls.ServerStreamingMethod<
              io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest,
              io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>() {
            @java.lang.Override
            public void invoke(
                io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest request,
                io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver) {
              serviceImpl.streamingOutputCall(request, responseObserver);
            }
          }))
      .addMethod(
        METHOD_STREAMING_INPUT_CALL,
        asyncClientStreamingCall(
          new io.grpc.stub.ServerCalls.ClientStreamingMethod<
              io.grpc.testing.integration.nano.Test.StreamingInputCallRequest,
              io.grpc.testing.integration.nano.Test.StreamingInputCallResponse>() {
            @java.lang.Override
            public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingInputCallRequest> invoke(
                io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingInputCallResponse> responseObserver) {
              return serviceImpl.streamingInputCall(responseObserver);
            }
          }))
      .addMethod(
        METHOD_FULL_BIDI_CALL,
        asyncBidiStreamingCall(
          new io.grpc.stub.ServerCalls.BidiStreamingMethod<
              io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest,
              io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>() {
            @java.lang.Override
            public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest> invoke(
                io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver) {
              return serviceImpl.fullBidiCall(responseObserver);
            }
          }))
      .addMethod(
        METHOD_HALF_BIDI_CALL,
        asyncBidiStreamingCall(
          new io.grpc.stub.ServerCalls.BidiStreamingMethod<
              io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest,
              io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse>() {
            @java.lang.Override
            public io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallRequest> invoke(
                io.grpc.stub.StreamObserver<io.grpc.testing.integration.nano.Test.StreamingOutputCallResponse> responseObserver) {
              return serviceImpl.halfBidiCall(responseObserver);
            }
          })).build();
  }
}
