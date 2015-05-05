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
public class WorkerGrpc {

  private static final io.grpc.stub.Method<grpc.testing.Qpstest.ClientArgs,
      grpc.testing.Qpstest.ClientStatus> METHOD_RUN_TEST =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "RunTest",
          io.grpc.protobuf.ProtoUtils.marshaller(grpc.testing.Qpstest.ClientArgs.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(grpc.testing.Qpstest.ClientStatus.PARSER));
  private static final io.grpc.stub.Method<grpc.testing.Qpstest.ServerArgs,
      grpc.testing.Qpstest.ServerStatus> METHOD_RUN_SERVER =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "RunServer",
          io.grpc.protobuf.ProtoUtils.marshaller(grpc.testing.Qpstest.ServerArgs.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(grpc.testing.Qpstest.ServerStatus.PARSER));

  public static WorkerStub newStub(io.grpc.Channel channel) {
    return new WorkerStub(channel, CONFIG);
  }

  public static WorkerBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new WorkerBlockingStub(channel, CONFIG);
  }

  public static WorkerFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new WorkerFutureStub(channel, CONFIG);
  }

  public static final WorkerServiceDescriptor CONFIG =
      new WorkerServiceDescriptor();

  @javax.annotation.concurrent.Immutable
  public static class WorkerServiceDescriptor extends
      io.grpc.stub.AbstractServiceDescriptor<WorkerServiceDescriptor> {
    public final io.grpc.MethodDescriptor<grpc.testing.Qpstest.ClientArgs,
        grpc.testing.Qpstest.ClientStatus> runTest;
    public final io.grpc.MethodDescriptor<grpc.testing.Qpstest.ServerArgs,
        grpc.testing.Qpstest.ServerStatus> runServer;

    private WorkerServiceDescriptor() {
      runTest = createMethodDescriptor(
          "grpc.testing.Worker", METHOD_RUN_TEST);
      runServer = createMethodDescriptor(
          "grpc.testing.Worker", METHOD_RUN_SERVER);
    }

    @SuppressWarnings("unchecked")
    private WorkerServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      runTest = (io.grpc.MethodDescriptor<grpc.testing.Qpstest.ClientArgs,
          grpc.testing.Qpstest.ClientStatus>) methodMap.get(
          CONFIG.runTest.getName());
      runServer = (io.grpc.MethodDescriptor<grpc.testing.Qpstest.ServerArgs,
          grpc.testing.Qpstest.ServerStatus>) methodMap.get(
          CONFIG.runServer.getName());
    }

    @java.lang.Override
    protected WorkerServiceDescriptor build(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      return new WorkerServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public com.google.common.collect.ImmutableList<io.grpc.MethodDescriptor<?, ?>> methods() {
      return com.google.common.collect.ImmutableList.<io.grpc.MethodDescriptor<?, ?>>of(
          runTest,
          runServer);
    }
  }

  public static interface Worker {

    public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ClientArgs> runTest(
        io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ClientStatus> responseObserver);

    public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ServerArgs> runServer(
        io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ServerStatus> responseObserver);
  }

  public static interface WorkerBlockingClient {
  }

  public static interface WorkerFutureClient {
  }

  public static class WorkerStub extends
      io.grpc.stub.AbstractStub<WorkerStub, WorkerServiceDescriptor>
      implements Worker {
    private WorkerStub(io.grpc.Channel channel,
        WorkerServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected WorkerStub build(io.grpc.Channel channel,
        WorkerServiceDescriptor config) {
      return new WorkerStub(channel, config);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ClientArgs> runTest(
        io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ClientStatus> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.runTest), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ServerArgs> runServer(
        io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ServerStatus> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.runServer), responseObserver);
    }
  }

  public static class WorkerBlockingStub extends
      io.grpc.stub.AbstractStub<WorkerBlockingStub, WorkerServiceDescriptor>
      implements WorkerBlockingClient {
    private WorkerBlockingStub(io.grpc.Channel channel,
        WorkerServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected WorkerBlockingStub build(io.grpc.Channel channel,
        WorkerServiceDescriptor config) {
      return new WorkerBlockingStub(channel, config);
    }
  }

  public static class WorkerFutureStub extends
      io.grpc.stub.AbstractStub<WorkerFutureStub, WorkerServiceDescriptor>
      implements WorkerFutureClient {
    private WorkerFutureStub(io.grpc.Channel channel,
        WorkerServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected WorkerFutureStub build(io.grpc.Channel channel,
        WorkerServiceDescriptor config) {
      return new WorkerFutureStub(channel, config);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final Worker serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder("grpc.testing.Worker")
      .addMethod(createMethodDefinition(
          METHOD_RUN_TEST,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                grpc.testing.Qpstest.ClientArgs,
                grpc.testing.Qpstest.ClientStatus>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ClientArgs> invoke(
                  io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ClientStatus> responseObserver) {
                return serviceImpl.runTest(responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_RUN_SERVER,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                grpc.testing.Qpstest.ServerArgs,
                grpc.testing.Qpstest.ServerStatus>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ServerArgs> invoke(
                  io.grpc.stub.StreamObserver<grpc.testing.Qpstest.ServerStatus> responseObserver) {
                return serviceImpl.runServer(responseObserver);
              }
            }))).build();
  }
}
