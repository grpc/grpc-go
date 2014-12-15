package com.google.net.stubby.testing.integration;

import static com.google.net.stubby.stub.Calls.createMethodDescriptor;
import static com.google.net.stubby.stub.Calls.asyncUnaryCall;
import static com.google.net.stubby.stub.Calls.asyncServerStreamingCall;
import static com.google.net.stubby.stub.Calls.asyncClientStreamingCall;
import static com.google.net.stubby.stub.Calls.duplexStreamingCall;
import static com.google.net.stubby.stub.Calls.blockingUnaryCall;
import static com.google.net.stubby.stub.Calls.blockingServerStreamingCall;
import static com.google.net.stubby.stub.Calls.unaryFutureCall;
import static com.google.net.stubby.stub.ServerCalls.createMethodDefinition;
import static com.google.net.stubby.stub.ServerCalls.asyncUnaryRequestCall;
import static com.google.net.stubby.stub.ServerCalls.asyncStreamingRequestCall;

@javax.annotation.Generated("by gRPC proto compiler")
public class TestServiceGrpc {

  private static final com.google.net.stubby.stub.Method<com.google.protobuf.EmptyProtos.Empty,
      com.google.protobuf.EmptyProtos.Empty> METHOD_EMPTY_CALL =
      com.google.net.stubby.stub.Method.create(
          com.google.net.stubby.MethodType.UNARY, "EmptyCall",
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.PARSER),
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.protobuf.EmptyProtos.Empty.PARSER));
  private static final com.google.net.stubby.stub.Method<com.google.net.stubby.testing.integration.Messages.SimpleRequest,
      com.google.net.stubby.testing.integration.Messages.SimpleResponse> METHOD_UNARY_CALL =
      com.google.net.stubby.stub.Method.create(
          com.google.net.stubby.MethodType.UNARY, "UnaryCall",
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.SimpleRequest.PARSER),
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.SimpleResponse.PARSER));
  private static final com.google.net.stubby.stub.Method<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
      com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> METHOD_STREAMING_OUTPUT_CALL =
      com.google.net.stubby.stub.Method.create(
          com.google.net.stubby.MethodType.SERVER_STREAMING, "StreamingOutputCall",
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest.PARSER),
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse.PARSER));
  private static final com.google.net.stubby.stub.Method<com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest,
      com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse> METHOD_STREAMING_INPUT_CALL =
      com.google.net.stubby.stub.Method.create(
          com.google.net.stubby.MethodType.CLIENT_STREAMING, "StreamingInputCall",
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest.PARSER),
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse.PARSER));
  private static final com.google.net.stubby.stub.Method<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
      com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> METHOD_FULL_DUPLEX_CALL =
      com.google.net.stubby.stub.Method.create(
          com.google.net.stubby.MethodType.DUPLEX_STREAMING, "FullDuplexCall",
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest.PARSER),
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse.PARSER));
  private static final com.google.net.stubby.stub.Method<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
      com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> METHOD_HALF_DUPLEX_CALL =
      com.google.net.stubby.stub.Method.create(
          com.google.net.stubby.MethodType.DUPLEX_STREAMING, "HalfDuplexCall",
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest.PARSER),
          com.google.net.stubby.proto.ProtoUtils.marshaller(com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse.PARSER));

  public static TestServiceStub newStub(com.google.net.stubby.Channel channel) {
    return new TestServiceStub(channel, CONFIG);
  }

  public static TestServiceBlockingStub newBlockingStub(
      com.google.net.stubby.Channel channel) {
    return new TestServiceBlockingStub(channel, CONFIG);
  }

  public static TestServiceFutureStub newFutureStub(
      com.google.net.stubby.Channel channel) {
    return new TestServiceFutureStub(channel, CONFIG);
  }

  public static final TestServiceServiceDescriptor CONFIG =
      new TestServiceServiceDescriptor();

  @javax.annotation.concurrent.Immutable
  public static class TestServiceServiceDescriptor extends
      com.google.net.stubby.stub.AbstractServiceDescriptor<TestServiceServiceDescriptor> {
    public final com.google.net.stubby.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
        com.google.protobuf.EmptyProtos.Empty> emptyCall;
    public final com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.SimpleRequest,
        com.google.net.stubby.testing.integration.Messages.SimpleResponse> unaryCall;
    public final com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
        com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall;
    public final com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest,
        com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse> streamingInputCall;
    public final com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
        com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> fullDuplexCall;
    public final com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
        com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> halfDuplexCall;

    private TestServiceServiceDescriptor() {
      emptyCall = createMethodDescriptor(
          "TestService", METHOD_EMPTY_CALL);
      unaryCall = createMethodDescriptor(
          "TestService", METHOD_UNARY_CALL);
      streamingOutputCall = createMethodDescriptor(
          "TestService", METHOD_STREAMING_OUTPUT_CALL);
      streamingInputCall = createMethodDescriptor(
          "TestService", METHOD_STREAMING_INPUT_CALL);
      fullDuplexCall = createMethodDescriptor(
          "TestService", METHOD_FULL_DUPLEX_CALL);
      halfDuplexCall = createMethodDescriptor(
          "TestService", METHOD_HALF_DUPLEX_CALL);
    }

    private TestServiceServiceDescriptor(
        java.util.Map<java.lang.String, com.google.net.stubby.MethodDescriptor<?, ?>> methodMap) {
      emptyCall = (com.google.net.stubby.MethodDescriptor<com.google.protobuf.EmptyProtos.Empty,
          com.google.protobuf.EmptyProtos.Empty>) methodMap.get(
          CONFIG.emptyCall.getName());
      unaryCall = (com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.SimpleRequest,
          com.google.net.stubby.testing.integration.Messages.SimpleResponse>) methodMap.get(
          CONFIG.unaryCall.getName());
      streamingOutputCall = (com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
          com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.streamingOutputCall.getName());
      streamingInputCall = (com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest,
          com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse>) methodMap.get(
          CONFIG.streamingInputCall.getName());
      fullDuplexCall = (com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
          com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.fullDuplexCall.getName());
      halfDuplexCall = (com.google.net.stubby.MethodDescriptor<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
          com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse>) methodMap.get(
          CONFIG.halfDuplexCall.getName());
    }

    @java.lang.Override
    protected TestServiceServiceDescriptor build(
        java.util.Map<java.lang.String, com.google.net.stubby.MethodDescriptor<?, ?>> methodMap) {
      return new TestServiceServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public com.google.common.collect.ImmutableList<com.google.net.stubby.MethodDescriptor<?, ?>> methods() {
      return com.google.common.collect.ImmutableList.<com.google.net.stubby.MethodDescriptor<?, ?>>of(
          emptyCall,
          unaryCall,
          streamingOutputCall,
          streamingInputCall,
          fullDuplexCall,
          halfDuplexCall);
    }
  }

  public static interface TestService {

    public void emptyCall(com.google.protobuf.EmptyProtos.Empty request,
        com.google.net.stubby.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver);

    public void unaryCall(com.google.net.stubby.testing.integration.Messages.SimpleRequest request,
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.SimpleResponse> responseObserver);

    public void streamingOutputCall(com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest request,
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver);

    public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest> streamingInputCall(
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse> responseObserver);

    public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest> fullDuplexCall(
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver);

    public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest> halfDuplexCall(
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver);
  }

  public static interface TestServiceBlockingClient {

    public com.google.protobuf.EmptyProtos.Empty emptyCall(com.google.protobuf.EmptyProtos.Empty request);

    public com.google.net.stubby.testing.integration.Messages.SimpleResponse unaryCall(com.google.net.stubby.testing.integration.Messages.SimpleRequest request);

    public java.util.Iterator<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall(
        com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest request);
  }

  public static interface TestServiceFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> emptyCall(
        com.google.protobuf.EmptyProtos.Empty request);

    public com.google.common.util.concurrent.ListenableFuture<com.google.net.stubby.testing.integration.Messages.SimpleResponse> unaryCall(
        com.google.net.stubby.testing.integration.Messages.SimpleRequest request);
  }

  public static class TestServiceStub extends
      com.google.net.stubby.stub.AbstractStub<TestServiceStub, TestServiceServiceDescriptor>
      implements TestService {
    private TestServiceStub(com.google.net.stubby.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceStub build(com.google.net.stubby.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceStub(channel, config);
    }

    @java.lang.Override
    public void emptyCall(com.google.protobuf.EmptyProtos.Empty request,
        com.google.net.stubby.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.emptyCall), request, responseObserver);
    }

    @java.lang.Override
    public void unaryCall(com.google.net.stubby.testing.integration.Messages.SimpleRequest request,
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.unaryCall), request, responseObserver);
    }

    @java.lang.Override
    public void streamingOutputCall(com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest request,
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      asyncServerStreamingCall(
          channel.newCall(config.streamingOutputCall), request, responseObserver);
    }

    @java.lang.Override
    public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest> streamingInputCall(
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse> responseObserver) {
      return asyncClientStreamingCall(
          channel.newCall(config.streamingInputCall), responseObserver);
    }

    @java.lang.Override
    public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest> fullDuplexCall(
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.fullDuplexCall), responseObserver);
    }

    @java.lang.Override
    public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest> halfDuplexCall(
        com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.halfDuplexCall), responseObserver);
    }
  }

  public static class TestServiceBlockingStub extends
      com.google.net.stubby.stub.AbstractStub<TestServiceBlockingStub, TestServiceServiceDescriptor>
      implements TestServiceBlockingClient {
    private TestServiceBlockingStub(com.google.net.stubby.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceBlockingStub build(com.google.net.stubby.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceBlockingStub(channel, config);
    }

    @java.lang.Override
    public com.google.protobuf.EmptyProtos.Empty emptyCall(com.google.protobuf.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          channel.newCall(config.emptyCall), request);
    }

    @java.lang.Override
    public com.google.net.stubby.testing.integration.Messages.SimpleResponse unaryCall(com.google.net.stubby.testing.integration.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          channel.newCall(config.unaryCall), request);
    }

    @java.lang.Override
    public java.util.Iterator<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall(
        com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest request) {
      return blockingServerStreamingCall(
          channel.newCall(config.streamingOutputCall), request);
    }
  }

  public static class TestServiceFutureStub extends
      com.google.net.stubby.stub.AbstractStub<TestServiceFutureStub, TestServiceServiceDescriptor>
      implements TestServiceFutureClient {
    private TestServiceFutureStub(com.google.net.stubby.Channel channel,
        TestServiceServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected TestServiceFutureStub build(com.google.net.stubby.Channel channel,
        TestServiceServiceDescriptor config) {
      return new TestServiceFutureStub(channel, config);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<com.google.protobuf.EmptyProtos.Empty> emptyCall(
        com.google.protobuf.EmptyProtos.Empty request) {
      return unaryFutureCall(
          channel.newCall(config.emptyCall), request);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<com.google.net.stubby.testing.integration.Messages.SimpleResponse> unaryCall(
        com.google.net.stubby.testing.integration.Messages.SimpleRequest request) {
      return unaryFutureCall(
          channel.newCall(config.unaryCall), request);
    }
  }

  public static com.google.net.stubby.ServerServiceDefinition bindService(
      final TestService serviceImpl) {
    return com.google.net.stubby.ServerServiceDefinition.builder("TestService")
      .addMethod(createMethodDefinition(
          METHOD_EMPTY_CALL,
          asyncUnaryRequestCall(
            new com.google.net.stubby.stub.ServerCalls.UnaryRequestMethod<
                com.google.protobuf.EmptyProtos.Empty,
                com.google.protobuf.EmptyProtos.Empty>() {
              @java.lang.Override
              public void invoke(
                  com.google.protobuf.EmptyProtos.Empty request,
                  com.google.net.stubby.stub.StreamObserver<com.google.protobuf.EmptyProtos.Empty> responseObserver) {
                serviceImpl.emptyCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_UNARY_CALL,
          asyncUnaryRequestCall(
            new com.google.net.stubby.stub.ServerCalls.UnaryRequestMethod<
                com.google.net.stubby.testing.integration.Messages.SimpleRequest,
                com.google.net.stubby.testing.integration.Messages.SimpleResponse>() {
              @java.lang.Override
              public void invoke(
                  com.google.net.stubby.testing.integration.Messages.SimpleRequest request,
                  com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.SimpleResponse> responseObserver) {
                serviceImpl.unaryCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_STREAMING_OUTPUT_CALL,
          asyncUnaryRequestCall(
            new com.google.net.stubby.stub.ServerCalls.UnaryRequestMethod<
                com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
                com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public void invoke(
                  com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest request,
                  com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
                serviceImpl.streamingOutputCall(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_STREAMING_INPUT_CALL,
          asyncStreamingRequestCall(
            new com.google.net.stubby.stub.ServerCalls.StreamingRequestMethod<
                com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest,
                com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse>() {
              @java.lang.Override
              public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest> invoke(
                  com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse> responseObserver) {
                return serviceImpl.streamingInputCall(responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_FULL_DUPLEX_CALL,
          asyncStreamingRequestCall(
            new com.google.net.stubby.stub.ServerCalls.StreamingRequestMethod<
                com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
                com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest> invoke(
                  com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
                return serviceImpl.fullDuplexCall(responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_HALF_DUPLEX_CALL,
          asyncStreamingRequestCall(
            new com.google.net.stubby.stub.ServerCalls.StreamingRequestMethod<
                com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest,
                com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse>() {
              @java.lang.Override
              public com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest> invoke(
                  com.google.net.stubby.stub.StreamObserver<com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
                return serviceImpl.halfDuplexCall(responseObserver);
              }
            }))).build();
  }
}
