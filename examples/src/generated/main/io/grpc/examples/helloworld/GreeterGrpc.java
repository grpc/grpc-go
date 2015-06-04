package io.grpc.examples.helloworld;

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
public class GreeterGrpc {

  private static final io.grpc.stub.Method<io.grpc.examples.helloworld.HelloRequest,
      io.grpc.examples.helloworld.HelloResponse> METHOD_SAY_HELLO =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.UNARY, "SayHello",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.helloworld.HelloRequest.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.helloworld.HelloResponse.PARSER));

  public static GreeterStub newStub(io.grpc.Channel channel) {
    return new GreeterStub(channel, CONFIG);
  }

  public static GreeterBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new GreeterBlockingStub(channel, CONFIG);
  }

  public static GreeterFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new GreeterFutureStub(channel, CONFIG);
  }

  public static final GreeterServiceDescriptor CONFIG =
      new GreeterServiceDescriptor();

  @javax.annotation.concurrent.Immutable
  public static class GreeterServiceDescriptor extends
      io.grpc.stub.AbstractServiceDescriptor<GreeterServiceDescriptor> {
    public final io.grpc.MethodDescriptor<io.grpc.examples.helloworld.HelloRequest,
        io.grpc.examples.helloworld.HelloResponse> sayHello;

    private GreeterServiceDescriptor() {
      sayHello = createMethodDescriptor(
          "helloworld.Greeter", METHOD_SAY_HELLO);
    }

    @SuppressWarnings("unchecked")
    private GreeterServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      sayHello = (io.grpc.MethodDescriptor<io.grpc.examples.helloworld.HelloRequest,
          io.grpc.examples.helloworld.HelloResponse>) methodMap.get(
          CONFIG.sayHello.getName());
    }

    @java.lang.Override
    protected GreeterServiceDescriptor build(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      return new GreeterServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public com.google.common.collect.ImmutableList<io.grpc.MethodDescriptor<?, ?>> methods() {
      return com.google.common.collect.ImmutableList.<io.grpc.MethodDescriptor<?, ?>>of(
          sayHello);
    }
  }

  public static interface Greeter {

    public void sayHello(io.grpc.examples.helloworld.HelloRequest request,
        io.grpc.stub.StreamObserver<io.grpc.examples.helloworld.HelloResponse> responseObserver);
  }

  public static interface GreeterBlockingClient {

    public io.grpc.examples.helloworld.HelloResponse sayHello(io.grpc.examples.helloworld.HelloRequest request);
  }

  public static interface GreeterFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.examples.helloworld.HelloResponse> sayHello(
        io.grpc.examples.helloworld.HelloRequest request);
  }

  public static class GreeterStub extends
      io.grpc.stub.AbstractStub<GreeterStub, GreeterServiceDescriptor>
      implements Greeter {
    private GreeterStub(io.grpc.Channel channel,
        GreeterServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected GreeterStub build(io.grpc.Channel channel,
        GreeterServiceDescriptor config) {
      return new GreeterStub(channel, config);
    }

    @java.lang.Override
    public void sayHello(io.grpc.examples.helloworld.HelloRequest request,
        io.grpc.stub.StreamObserver<io.grpc.examples.helloworld.HelloResponse> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.sayHello), request, responseObserver);
    }
  }

  public static class GreeterBlockingStub extends
      io.grpc.stub.AbstractStub<GreeterBlockingStub, GreeterServiceDescriptor>
      implements GreeterBlockingClient {
    private GreeterBlockingStub(io.grpc.Channel channel,
        GreeterServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected GreeterBlockingStub build(io.grpc.Channel channel,
        GreeterServiceDescriptor config) {
      return new GreeterBlockingStub(channel, config);
    }

    @java.lang.Override
    public io.grpc.examples.helloworld.HelloResponse sayHello(io.grpc.examples.helloworld.HelloRequest request) {
      return blockingUnaryCall(
          channel.newCall(config.sayHello), request);
    }
  }

  public static class GreeterFutureStub extends
      io.grpc.stub.AbstractStub<GreeterFutureStub, GreeterServiceDescriptor>
      implements GreeterFutureClient {
    private GreeterFutureStub(io.grpc.Channel channel,
        GreeterServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected GreeterFutureStub build(io.grpc.Channel channel,
        GreeterServiceDescriptor config) {
      return new GreeterFutureStub(channel, config);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.examples.helloworld.HelloResponse> sayHello(
        io.grpc.examples.helloworld.HelloRequest request) {
      return unaryFutureCall(
          channel.newCall(config.sayHello), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final Greeter serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder("helloworld.Greeter")
      .addMethod(createMethodDefinition(
          METHOD_SAY_HELLO,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.examples.helloworld.HelloRequest,
                io.grpc.examples.helloworld.HelloResponse>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.examples.helloworld.HelloRequest request,
                  io.grpc.stub.StreamObserver<io.grpc.examples.helloworld.HelloResponse> responseObserver) {
                serviceImpl.sayHello(request, responseObserver);
              }
            }))).build();
  }
}
