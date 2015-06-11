package io.grpc.android.integrationtest;

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

import java.io.IOException;

//@javax.annotation.Generated("by gRPC proto compiler")
public class TestServiceGrpc {

  private static final io.grpc.stub.Method<io.grpc.android.integrationtest.EmptyProtos.Empty,
      io.grpc.android.integrationtest.EmptyProtos.Empty> METHOD_EMPTY_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.UNARY, "EmptyCall",
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.EmptyProtos.Empty>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.EmptyProtos.Empty>() {
                  @Override
                  public io.grpc.android.integrationtest.EmptyProtos.Empty parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.EmptyProtos.Empty.parseFrom(input);
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.EmptyProtos.Empty>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.EmptyProtos.Empty>() {
                  @Override
                  public io.grpc.android.integrationtest.EmptyProtos.Empty parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.EmptyProtos.Empty.parseFrom(input);
                  }
          }));
  private static final io.grpc.stub.Method<io.grpc.android.integrationtest.Messages.SimpleRequest,
      io.grpc.android.integrationtest.Messages.SimpleResponse> METHOD_UNARY_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.UNARY, "UnaryCall",
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.SimpleRequest>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.SimpleRequest>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.SimpleRequest parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.SimpleRequest.parseFrom(input);
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.SimpleResponse>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.SimpleResponse>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.SimpleResponse parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.SimpleResponse.parseFrom(input);
                  }
          }));
  private static final io.grpc.stub.Method<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
      io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> METHOD_STREAMING_OUTPUT_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.SERVER_STREAMING, "StreamingOutputCall",
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest.parseFrom(input);
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse.parseFrom(input);
                  }
          }));
  private static final io.grpc.stub.Method<io.grpc.android.integrationtest.Messages.StreamingInputCallRequest,
      io.grpc.android.integrationtest.Messages.StreamingInputCallResponse> METHOD_STREAMING_INPUT_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.CLIENT_STREAMING, "StreamingInputCall",
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.StreamingInputCallRequest>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.StreamingInputCallRequest>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.StreamingInputCallRequest parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.StreamingInputCallRequest.parseFrom(input);
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.StreamingInputCallResponse>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.StreamingInputCallResponse>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.StreamingInputCallResponse parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.StreamingInputCallResponse.parseFrom(input);
                  }
          }));
  private static final io.grpc.stub.Method<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
      io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> METHOD_FULL_DUPLEX_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "FullDuplexCall",
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest.parseFrom(input);
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse.parseFrom(input);
                  }
          }));
  private static final io.grpc.stub.Method<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
      io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> METHOD_HALF_DUPLEX_CALL =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "HalfDuplexCall",
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest.parseFrom(input);
                  }
          }),
          io.grpc.protobuf.nano.NanoUtils.<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>marshaller(
              new io.grpc.protobuf.nano.Parser<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>() {
                  @Override
                  public io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse parse(com.google.protobuf.nano.CodedInputByteBufferNano input) throws IOException {
                      return io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse.parseFrom(input);
                  }
          }));

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
    public final io.grpc.MethodDescriptor<io.grpc.android.integrationtest.EmptyProtos.Empty,
        io.grpc.android.integrationtest.EmptyProtos.Empty> emptyCall;
    public final io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.SimpleRequest,
        io.grpc.android.integrationtest.Messages.SimpleResponse> unaryCall;
    public final io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
        io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> streamingOutputCall;
    public final io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.StreamingInputCallRequest,
        io.grpc.android.integrationtest.Messages.StreamingInputCallResponse> streamingInputCall;
    public final io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
        io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> fullDuplexCall;
    public final io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
        io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> halfDuplexCall;

    private TestServiceServiceDescriptor() {
      emptyCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_EMPTY_CALL);
      unaryCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_UNARY_CALL);
      streamingOutputCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_STREAMING_OUTPUT_CALL);
      streamingInputCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_STREAMING_INPUT_CALL);
      fullDuplexCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_FULL_DUPLEX_CALL);
      halfDuplexCall = createMethodDescriptor(
          "grpc.testing.TestService", METHOD_HALF_DUPLEX_CALL);
    }

    @SuppressWarnings("unchecked")
    private TestServiceServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      emptyCall = (io.grpc.MethodDescriptor<io.grpc.android.integrationtest.EmptyProtos.Empty,
          io.grpc.android.integrationtest.EmptyProtos.Empty>) methodMap.get(
          CONFIG.emptyCall.getName());
      unaryCall = (io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.SimpleRequest,
          io.grpc.android.integrationtest.Messages.SimpleResponse>) methodMap.get(
          CONFIG.unaryCall.getName());
      streamingOutputCall = (io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
          io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.streamingOutputCall.getName());
      streamingInputCall = (io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.StreamingInputCallRequest,
          io.grpc.android.integrationtest.Messages.StreamingInputCallResponse>) methodMap.get(
          CONFIG.streamingInputCall.getName());
      fullDuplexCall = (io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
          io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.fullDuplexCall.getName());
      halfDuplexCall = (io.grpc.MethodDescriptor<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
          io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.halfDuplexCall.getName());
    }

    @java.lang.Override
    protected TestServiceServiceDescriptor build(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      return new TestServiceServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public com.google.common.collect.ImmutableList<io.grpc.MethodDescriptor<?, ?>> methods() {
      return com.google.common.collect.ImmutableList.<io.grpc.MethodDescriptor<?, ?>>of(
          emptyCall,
          unaryCall,
          streamingOutputCall,
          streamingInputCall,
          fullDuplexCall,
          halfDuplexCall);
    }
  }

  public static interface TestService {

    public void emptyCall(io.grpc.android.integrationtest.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.EmptyProtos.Empty> responseObserver);

    public void unaryCall(io.grpc.android.integrationtest.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.SimpleResponse> responseObserver);

    public void streamingOutputCall(io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingInputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver);
  }

  public static interface TestServiceBlockingClient {

    public io.grpc.android.integrationtest.EmptyProtos.Empty emptyCall(io.grpc.android.integrationtest.EmptyProtos.Empty request);

    public io.grpc.android.integrationtest.Messages.SimpleResponse unaryCall(io.grpc.android.integrationtest.Messages.SimpleRequest request);

    public java.util.Iterator<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest request);
  }

  public static interface TestServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.android.integrationtest.EmptyProtos.Empty> emptyCall(
        io.grpc.android.integrationtest.EmptyProtos.Empty request);

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.android.integrationtest.Messages.SimpleResponse> unaryCall(
        io.grpc.android.integrationtest.Messages.SimpleRequest request);
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
    public void emptyCall(io.grpc.android.integrationtest.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.emptyCall), request, responseObserver);
    }

    @java.lang.Override
    public void unaryCall(io.grpc.android.integrationtest.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.unaryCall), request, responseObserver);
    }

    @java.lang.Override
    public void streamingOutputCall(io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver) {
      asyncServerStreamingCall(
          channel.newCall(config.streamingOutputCall), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingInputCallResponse> responseObserver) {
      return asyncClientStreamingCall(
          channel.newCall(config.streamingInputCall), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.fullDuplexCall), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.halfDuplexCall), responseObserver);
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
    public io.grpc.android.integrationtest.EmptyProtos.Empty emptyCall(io.grpc.android.integrationtest.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          channel.newCall(config.emptyCall), request);
    }

    @java.lang.Override
    public io.grpc.android.integrationtest.Messages.SimpleResponse unaryCall(io.grpc.android.integrationtest.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          channel.newCall(config.unaryCall), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest request) {
      return blockingServerStreamingCall(
          channel.newCall(config.streamingOutputCall), request);
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
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.android.integrationtest.EmptyProtos.Empty> emptyCall(
        io.grpc.android.integrationtest.EmptyProtos.Empty request) {
      return unaryFutureCall(
          channel.newCall(config.emptyCall), request);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.android.integrationtest.Messages.SimpleResponse> unaryCall(
        io.grpc.android.integrationtest.Messages.SimpleRequest request) {
      return unaryFutureCall(
          channel.newCall(config.unaryCall), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder("grpc.testing.TestService")
      .addMethod(createMethodDefinition(
          METHOD_EMPTY_CALL,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.android.integrationtest.EmptyProtos.Empty,
                io.grpc.android.integrationtest.EmptyProtos.Empty>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.android.integrationtest.EmptyProtos.Empty request,
                  io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.EmptyProtos.Empty> responseObserver) {
                serviceImpl.emptyCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_UNARY_CALL,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.android.integrationtest.Messages.SimpleRequest,
                io.grpc.android.integrationtest.Messages.SimpleResponse>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.android.integrationtest.Messages.SimpleRequest request,
                  io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.SimpleResponse> responseObserver) {
                serviceImpl.unaryCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_STREAMING_OUTPUT_CALL,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
                io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest request,
                  io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver) {
                serviceImpl.streamingOutputCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_STREAMING_INPUT_CALL,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                io.grpc.android.integrationtest.Messages.StreamingInputCallRequest,
                io.grpc.android.integrationtest.Messages.StreamingInputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingInputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingInputCallResponse> responseObserver) {
                return serviceImpl.streamingInputCall(responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_FULL_DUPLEX_CALL,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
                io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver) {
                return serviceImpl.fullDuplexCall(responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_HALF_DUPLEX_CALL,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest,
                io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallRequest> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.android.integrationtest.Messages.StreamingOutputCallResponse> responseObserver) {
                return serviceImpl.halfDuplexCall(responseObserver);
              }
            }))).build();
  }
}
