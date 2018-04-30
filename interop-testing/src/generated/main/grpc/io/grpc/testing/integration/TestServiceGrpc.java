package io.grpc.testing.integration;

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
 * A simple service to test the various types of RPCs and experiment with
 * performance with various types of payload.
 * </pre>
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: grpc/testing/test.proto")
public final class TestServiceGrpc {

  private TestServiceGrpc() {}

  public static final String SERVICE_NAME = "grpc.testing.TestService";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getEmptyCallMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty,
      io.grpc.testing.integration.EmptyProtos.Empty> METHOD_EMPTY_CALL = getEmptyCallMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty,
      io.grpc.testing.integration.EmptyProtos.Empty> getEmptyCallMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty,
      io.grpc.testing.integration.EmptyProtos.Empty> getEmptyCallMethod() {
    return getEmptyCallMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty,
      io.grpc.testing.integration.EmptyProtos.Empty> getEmptyCallMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty, io.grpc.testing.integration.EmptyProtos.Empty> getEmptyCallMethod;
    if ((getEmptyCallMethod = TestServiceGrpc.getEmptyCallMethod) == null) {
      synchronized (TestServiceGrpc.class) {
        if ((getEmptyCallMethod = TestServiceGrpc.getEmptyCallMethod) == null) {
          TestServiceGrpc.getEmptyCallMethod = getEmptyCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.EmptyProtos.Empty, io.grpc.testing.integration.EmptyProtos.Empty>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.TestService", "EmptyCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.EmptyProtos.Empty.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.EmptyProtos.Empty.getDefaultInstance()))
                  .setSchemaDescriptor(new TestServiceMethodDescriptorSupplier("EmptyCall"))
                  .build();
          }
        }
     }
     return getEmptyCallMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getUnaryCallMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> METHOD_UNARY_CALL = getUnaryCallMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> getUnaryCallMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> getUnaryCallMethod() {
    return getUnaryCallMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> getUnaryCallMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest, io.grpc.testing.integration.Messages.SimpleResponse> getUnaryCallMethod;
    if ((getUnaryCallMethod = TestServiceGrpc.getUnaryCallMethod) == null) {
      synchronized (TestServiceGrpc.class) {
        if ((getUnaryCallMethod = TestServiceGrpc.getUnaryCallMethod) == null) {
          TestServiceGrpc.getUnaryCallMethod = getUnaryCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.Messages.SimpleRequest, io.grpc.testing.integration.Messages.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.TestService", "UnaryCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new TestServiceMethodDescriptorSupplier("UnaryCall"))
                  .build();
          }
        }
     }
     return getUnaryCallMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getCacheableUnaryCallMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> METHOD_CACHEABLE_UNARY_CALL = getCacheableUnaryCallMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> getCacheableUnaryCallMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> getCacheableUnaryCallMethod() {
    return getCacheableUnaryCallMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest,
      io.grpc.testing.integration.Messages.SimpleResponse> getCacheableUnaryCallMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.SimpleRequest, io.grpc.testing.integration.Messages.SimpleResponse> getCacheableUnaryCallMethod;
    if ((getCacheableUnaryCallMethod = TestServiceGrpc.getCacheableUnaryCallMethod) == null) {
      synchronized (TestServiceGrpc.class) {
        if ((getCacheableUnaryCallMethod = TestServiceGrpc.getCacheableUnaryCallMethod) == null) {
          TestServiceGrpc.getCacheableUnaryCallMethod = getCacheableUnaryCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.Messages.SimpleRequest, io.grpc.testing.integration.Messages.SimpleResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.TestService", "CacheableUnaryCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.SimpleRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.SimpleResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new TestServiceMethodDescriptorSupplier("CacheableUnaryCall"))
                  .build();
          }
        }
     }
     return getCacheableUnaryCallMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getStreamingOutputCallMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_STREAMING_OUTPUT_CALL = getStreamingOutputCallMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getStreamingOutputCallMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getStreamingOutputCallMethod() {
    return getStreamingOutputCallMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getStreamingOutputCallMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest, io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getStreamingOutputCallMethod;
    if ((getStreamingOutputCallMethod = TestServiceGrpc.getStreamingOutputCallMethod) == null) {
      synchronized (TestServiceGrpc.class) {
        if ((getStreamingOutputCallMethod = TestServiceGrpc.getStreamingOutputCallMethod) == null) {
          TestServiceGrpc.getStreamingOutputCallMethod = getStreamingOutputCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.Messages.StreamingOutputCallRequest, io.grpc.testing.integration.Messages.StreamingOutputCallResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.TestService", "StreamingOutputCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.StreamingOutputCallRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.StreamingOutputCallResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new TestServiceMethodDescriptorSupplier("StreamingOutputCall"))
                  .build();
          }
        }
     }
     return getStreamingOutputCallMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getStreamingInputCallMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest,
      io.grpc.testing.integration.Messages.StreamingInputCallResponse> METHOD_STREAMING_INPUT_CALL = getStreamingInputCallMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest,
      io.grpc.testing.integration.Messages.StreamingInputCallResponse> getStreamingInputCallMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest,
      io.grpc.testing.integration.Messages.StreamingInputCallResponse> getStreamingInputCallMethod() {
    return getStreamingInputCallMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest,
      io.grpc.testing.integration.Messages.StreamingInputCallResponse> getStreamingInputCallMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingInputCallRequest, io.grpc.testing.integration.Messages.StreamingInputCallResponse> getStreamingInputCallMethod;
    if ((getStreamingInputCallMethod = TestServiceGrpc.getStreamingInputCallMethod) == null) {
      synchronized (TestServiceGrpc.class) {
        if ((getStreamingInputCallMethod = TestServiceGrpc.getStreamingInputCallMethod) == null) {
          TestServiceGrpc.getStreamingInputCallMethod = getStreamingInputCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.Messages.StreamingInputCallRequest, io.grpc.testing.integration.Messages.StreamingInputCallResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.TestService", "StreamingInputCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.StreamingInputCallRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.StreamingInputCallResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new TestServiceMethodDescriptorSupplier("StreamingInputCall"))
                  .build();
          }
        }
     }
     return getStreamingInputCallMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getFullDuplexCallMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_FULL_DUPLEX_CALL = getFullDuplexCallMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getFullDuplexCallMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getFullDuplexCallMethod() {
    return getFullDuplexCallMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getFullDuplexCallMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest, io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getFullDuplexCallMethod;
    if ((getFullDuplexCallMethod = TestServiceGrpc.getFullDuplexCallMethod) == null) {
      synchronized (TestServiceGrpc.class) {
        if ((getFullDuplexCallMethod = TestServiceGrpc.getFullDuplexCallMethod) == null) {
          TestServiceGrpc.getFullDuplexCallMethod = getFullDuplexCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.Messages.StreamingOutputCallRequest, io.grpc.testing.integration.Messages.StreamingOutputCallResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.TestService", "FullDuplexCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.StreamingOutputCallRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.StreamingOutputCallResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new TestServiceMethodDescriptorSupplier("FullDuplexCall"))
                  .build();
          }
        }
     }
     return getFullDuplexCallMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getHalfDuplexCallMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> METHOD_HALF_DUPLEX_CALL = getHalfDuplexCallMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getHalfDuplexCallMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getHalfDuplexCallMethod() {
    return getHalfDuplexCallMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
      io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getHalfDuplexCallMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.Messages.StreamingOutputCallRequest, io.grpc.testing.integration.Messages.StreamingOutputCallResponse> getHalfDuplexCallMethod;
    if ((getHalfDuplexCallMethod = TestServiceGrpc.getHalfDuplexCallMethod) == null) {
      synchronized (TestServiceGrpc.class) {
        if ((getHalfDuplexCallMethod = TestServiceGrpc.getHalfDuplexCallMethod) == null) {
          TestServiceGrpc.getHalfDuplexCallMethod = getHalfDuplexCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.Messages.StreamingOutputCallRequest, io.grpc.testing.integration.Messages.StreamingOutputCallResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.TestService", "HalfDuplexCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.StreamingOutputCallRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.Messages.StreamingOutputCallResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new TestServiceMethodDescriptorSupplier("HalfDuplexCall"))
                  .build();
          }
        }
     }
     return getHalfDuplexCallMethod;
  }
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getUnimplementedCallMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty,
      io.grpc.testing.integration.EmptyProtos.Empty> METHOD_UNIMPLEMENTED_CALL = getUnimplementedCallMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty,
      io.grpc.testing.integration.EmptyProtos.Empty> getUnimplementedCallMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty,
      io.grpc.testing.integration.EmptyProtos.Empty> getUnimplementedCallMethod() {
    return getUnimplementedCallMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty,
      io.grpc.testing.integration.EmptyProtos.Empty> getUnimplementedCallMethodHelper() {
    io.grpc.MethodDescriptor<io.grpc.testing.integration.EmptyProtos.Empty, io.grpc.testing.integration.EmptyProtos.Empty> getUnimplementedCallMethod;
    if ((getUnimplementedCallMethod = TestServiceGrpc.getUnimplementedCallMethod) == null) {
      synchronized (TestServiceGrpc.class) {
        if ((getUnimplementedCallMethod = TestServiceGrpc.getUnimplementedCallMethod) == null) {
          TestServiceGrpc.getUnimplementedCallMethod = getUnimplementedCallMethod = 
              io.grpc.MethodDescriptor.<io.grpc.testing.integration.EmptyProtos.Empty, io.grpc.testing.integration.EmptyProtos.Empty>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.testing.TestService", "UnimplementedCall"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.EmptyProtos.Empty.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.testing.integration.EmptyProtos.Empty.getDefaultInstance()))
                  .setSchemaDescriptor(new TestServiceMethodDescriptorSupplier("UnimplementedCall"))
                  .build();
          }
        }
     }
     return getUnimplementedCallMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static TestServiceStub newStub(io.grpc.Channel channel) {
    return new TestServiceStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static TestServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new TestServiceBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static TestServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new TestServiceFutureStub(channel);
  }

  /**
   * <pre>
   * A simple service to test the various types of RPCs and experiment with
   * performance with various types of payload.
   * </pre>
   */
  public static abstract class TestServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * One empty request followed by one empty response.
     * </pre>
     */
    public void emptyCall(io.grpc.testing.integration.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.EmptyProtos.Empty> responseObserver) {
      asyncUnimplementedUnaryCall(getEmptyCallMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * One request followed by one response.
     * </pre>
     */
    public void unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getUnaryCallMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * One request followed by one response. Response has cache control
     * headers set such that a caching HTTP proxy (such as GFE) can
     * satisfy subsequent requests.
     * </pre>
     */
    public void cacheableUnaryCall(io.grpc.testing.integration.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getCacheableUnaryCallMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * One request followed by a sequence of responses (streamed download).
     * The server returns the payload with client desired type and sizes.
     * </pre>
     */
    public void streamingOutputCall(io.grpc.testing.integration.Messages.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getStreamingOutputCallMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * A sequence of requests followed by one response (streamed upload).
     * The server returns the aggregated size of client payload as the result.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getStreamingInputCallMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * A sequence of requests with each request served by the server immediately.
     * As one request could lead to multiple responses, this interface
     * demonstrates the idea of full duplexing.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getFullDuplexCallMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * A sequence of requests followed by a sequence of responses.
     * The server buffers all the client requests and then serves them in order. A
     * stream of responses are returned to the client when the server starts with
     * first request.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return asyncUnimplementedStreamingCall(getHalfDuplexCallMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * The test server will not implement this method. It will be used
     * to test the behavior when clients call unimplemented methods.
     * </pre>
     */
    public void unimplementedCall(io.grpc.testing.integration.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.EmptyProtos.Empty> responseObserver) {
      asyncUnimplementedUnaryCall(getUnimplementedCallMethodHelper(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getEmptyCallMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.testing.integration.EmptyProtos.Empty,
                io.grpc.testing.integration.EmptyProtos.Empty>(
                  this, METHODID_EMPTY_CALL)))
          .addMethod(
            getUnaryCallMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.testing.integration.Messages.SimpleRequest,
                io.grpc.testing.integration.Messages.SimpleResponse>(
                  this, METHODID_UNARY_CALL)))
          .addMethod(
            getCacheableUnaryCallMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.testing.integration.Messages.SimpleRequest,
                io.grpc.testing.integration.Messages.SimpleResponse>(
                  this, METHODID_CACHEABLE_UNARY_CALL)))
          .addMethod(
            getStreamingOutputCallMethodHelper(),
            asyncServerStreamingCall(
              new MethodHandlers<
                io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
                io.grpc.testing.integration.Messages.StreamingOutputCallResponse>(
                  this, METHODID_STREAMING_OUTPUT_CALL)))
          .addMethod(
            getStreamingInputCallMethodHelper(),
            asyncClientStreamingCall(
              new MethodHandlers<
                io.grpc.testing.integration.Messages.StreamingInputCallRequest,
                io.grpc.testing.integration.Messages.StreamingInputCallResponse>(
                  this, METHODID_STREAMING_INPUT_CALL)))
          .addMethod(
            getFullDuplexCallMethodHelper(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
                io.grpc.testing.integration.Messages.StreamingOutputCallResponse>(
                  this, METHODID_FULL_DUPLEX_CALL)))
          .addMethod(
            getHalfDuplexCallMethodHelper(),
            asyncBidiStreamingCall(
              new MethodHandlers<
                io.grpc.testing.integration.Messages.StreamingOutputCallRequest,
                io.grpc.testing.integration.Messages.StreamingOutputCallResponse>(
                  this, METHODID_HALF_DUPLEX_CALL)))
          .addMethod(
            getUnimplementedCallMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.testing.integration.EmptyProtos.Empty,
                io.grpc.testing.integration.EmptyProtos.Empty>(
                  this, METHODID_UNIMPLEMENTED_CALL)))
          .build();
    }
  }

  /**
   * <pre>
   * A simple service to test the various types of RPCs and experiment with
   * performance with various types of payload.
   * </pre>
   */
  public static final class TestServiceStub extends io.grpc.stub.AbstractStub<TestServiceStub> {
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

    /**
     * <pre>
     * One empty request followed by one empty response.
     * </pre>
     */
    public void emptyCall(io.grpc.testing.integration.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getEmptyCallMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * One request followed by one response.
     * </pre>
     */
    public void unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getUnaryCallMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * One request followed by one response. Response has cache control
     * headers set such that a caching HTTP proxy (such as GFE) can
     * satisfy subsequent requests.
     * </pre>
     */
    public void cacheableUnaryCall(io.grpc.testing.integration.Messages.SimpleRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getCacheableUnaryCallMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * One request followed by a sequence of responses (streamed download).
     * The server returns the payload with client desired type and sizes.
     * </pre>
     */
    public void streamingOutputCall(io.grpc.testing.integration.Messages.StreamingOutputCallRequest request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(getStreamingOutputCallMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * A sequence of requests followed by one response (streamed upload).
     * The server returns the aggregated size of client payload as the result.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallRequest> streamingInputCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse> responseObserver) {
      return asyncClientStreamingCall(
          getChannel().newCall(getStreamingInputCallMethodHelper(), getCallOptions()), responseObserver);
    }

    /**
     * <pre>
     * A sequence of requests with each request served by the server immediately.
     * As one request could lead to multiple responses, this interface
     * demonstrates the idea of full duplexing.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> fullDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getFullDuplexCallMethodHelper(), getCallOptions()), responseObserver);
    }

    /**
     * <pre>
     * A sequence of requests followed by a sequence of responses.
     * The server buffers all the client requests and then serves them in order. A
     * stream of responses are returned to the client when the server starts with
     * first request.
     * </pre>
     */
    public io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallRequest> halfDuplexCall(
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(getHalfDuplexCallMethodHelper(), getCallOptions()), responseObserver);
    }

    /**
     * <pre>
     * The test server will not implement this method. It will be used
     * to test the behavior when clients call unimplemented methods.
     * </pre>
     */
    public void unimplementedCall(io.grpc.testing.integration.EmptyProtos.Empty request,
        io.grpc.stub.StreamObserver<io.grpc.testing.integration.EmptyProtos.Empty> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getUnimplementedCallMethodHelper(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   * <pre>
   * A simple service to test the various types of RPCs and experiment with
   * performance with various types of payload.
   * </pre>
   */
  public static final class TestServiceBlockingStub extends io.grpc.stub.AbstractStub<TestServiceBlockingStub> {
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

    /**
     * <pre>
     * One empty request followed by one empty response.
     * </pre>
     */
    public io.grpc.testing.integration.EmptyProtos.Empty emptyCall(io.grpc.testing.integration.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          getChannel(), getEmptyCallMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * One request followed by one response.
     * </pre>
     */
    public io.grpc.testing.integration.Messages.SimpleResponse unaryCall(io.grpc.testing.integration.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel(), getUnaryCallMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * One request followed by one response. Response has cache control
     * headers set such that a caching HTTP proxy (such as GFE) can
     * satisfy subsequent requests.
     * </pre>
     */
    public io.grpc.testing.integration.Messages.SimpleResponse cacheableUnaryCall(io.grpc.testing.integration.Messages.SimpleRequest request) {
      return blockingUnaryCall(
          getChannel(), getCacheableUnaryCallMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * One request followed by a sequence of responses (streamed download).
     * The server returns the payload with client desired type and sizes.
     * </pre>
     */
    public java.util.Iterator<io.grpc.testing.integration.Messages.StreamingOutputCallResponse> streamingOutputCall(
        io.grpc.testing.integration.Messages.StreamingOutputCallRequest request) {
      return blockingServerStreamingCall(
          getChannel(), getStreamingOutputCallMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * The test server will not implement this method. It will be used
     * to test the behavior when clients call unimplemented methods.
     * </pre>
     */
    public io.grpc.testing.integration.EmptyProtos.Empty unimplementedCall(io.grpc.testing.integration.EmptyProtos.Empty request) {
      return blockingUnaryCall(
          getChannel(), getUnimplementedCallMethodHelper(), getCallOptions(), request);
    }
  }

  /**
   * <pre>
   * A simple service to test the various types of RPCs and experiment with
   * performance with various types of payload.
   * </pre>
   */
  public static final class TestServiceFutureStub extends io.grpc.stub.AbstractStub<TestServiceFutureStub> {
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

    /**
     * <pre>
     * One empty request followed by one empty response.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.EmptyProtos.Empty> emptyCall(
        io.grpc.testing.integration.EmptyProtos.Empty request) {
      return futureUnaryCall(
          getChannel().newCall(getEmptyCallMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * One request followed by one response.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Messages.SimpleResponse> unaryCall(
        io.grpc.testing.integration.Messages.SimpleRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getUnaryCallMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * One request followed by one response. Response has cache control
     * headers set such that a caching HTTP proxy (such as GFE) can
     * satisfy subsequent requests.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.Messages.SimpleResponse> cacheableUnaryCall(
        io.grpc.testing.integration.Messages.SimpleRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getCacheableUnaryCallMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * The test server will not implement this method. It will be used
     * to test the behavior when clients call unimplemented methods.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.testing.integration.EmptyProtos.Empty> unimplementedCall(
        io.grpc.testing.integration.EmptyProtos.Empty request) {
      return futureUnaryCall(
          getChannel().newCall(getUnimplementedCallMethodHelper(), getCallOptions()), request);
    }
  }

  private static final int METHODID_EMPTY_CALL = 0;
  private static final int METHODID_UNARY_CALL = 1;
  private static final int METHODID_CACHEABLE_UNARY_CALL = 2;
  private static final int METHODID_STREAMING_OUTPUT_CALL = 3;
  private static final int METHODID_UNIMPLEMENTED_CALL = 4;
  private static final int METHODID_STREAMING_INPUT_CALL = 5;
  private static final int METHODID_FULL_DUPLEX_CALL = 6;
  private static final int METHODID_HALF_DUPLEX_CALL = 7;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final TestServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(TestServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_EMPTY_CALL:
          serviceImpl.emptyCall((io.grpc.testing.integration.EmptyProtos.Empty) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.EmptyProtos.Empty>) responseObserver);
          break;
        case METHODID_UNARY_CALL:
          serviceImpl.unaryCall((io.grpc.testing.integration.Messages.SimpleRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse>) responseObserver);
          break;
        case METHODID_CACHEABLE_UNARY_CALL:
          serviceImpl.cacheableUnaryCall((io.grpc.testing.integration.Messages.SimpleRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.SimpleResponse>) responseObserver);
          break;
        case METHODID_STREAMING_OUTPUT_CALL:
          serviceImpl.streamingOutputCall((io.grpc.testing.integration.Messages.StreamingOutputCallRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) responseObserver);
          break;
        case METHODID_UNIMPLEMENTED_CALL:
          serviceImpl.unimplementedCall((io.grpc.testing.integration.EmptyProtos.Empty) request,
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.EmptyProtos.Empty>) responseObserver);
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
        case METHODID_STREAMING_INPUT_CALL:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.streamingInputCall(
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingInputCallResponse>) responseObserver);
        case METHODID_FULL_DUPLEX_CALL:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.fullDuplexCall(
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) responseObserver);
        case METHODID_HALF_DUPLEX_CALL:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.halfDuplexCall(
              (io.grpc.stub.StreamObserver<io.grpc.testing.integration.Messages.StreamingOutputCallResponse>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class TestServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    TestServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.testing.integration.Test.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("TestService");
    }
  }

  private static final class TestServiceFileDescriptorSupplier
      extends TestServiceBaseDescriptorSupplier {
    TestServiceFileDescriptorSupplier() {}
  }

  private static final class TestServiceMethodDescriptorSupplier
      extends TestServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    TestServiceMethodDescriptorSupplier(String methodName) {
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
      synchronized (TestServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new TestServiceFileDescriptorSupplier())
              .addMethod(getEmptyCallMethodHelper())
              .addMethod(getUnaryCallMethodHelper())
              .addMethod(getCacheableUnaryCallMethodHelper())
              .addMethod(getStreamingOutputCallMethodHelper())
              .addMethod(getStreamingInputCallMethodHelper())
              .addMethod(getFullDuplexCallMethodHelper())
              .addMethod(getHalfDuplexCallMethodHelper())
              .addMethod(getUnimplementedCallMethodHelper())
              .build();
        }
      }
    }
    return result;
  }
}
