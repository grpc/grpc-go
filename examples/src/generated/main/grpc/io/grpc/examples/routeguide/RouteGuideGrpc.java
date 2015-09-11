package io.grpc.examples.routeguide;

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

@javax.annotation.Generated("by gRPC proto compiler")
public class RouteGuideGrpc {

  private RouteGuideGrpc() {}

  public static final String SERVICE_NAME = "routeguide.RouteGuide";

  // Static method descriptors that strictly reflect the proto.
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
      io.grpc.examples.routeguide.Feature> METHOD_GET_FEATURE =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          generateFullMethodName(
              "routeguide.RouteGuide", "GetFeature"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Point.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Feature.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Rectangle,
      io.grpc.examples.routeguide.Feature> METHOD_LIST_FEATURES =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING,
          generateFullMethodName(
              "routeguide.RouteGuide", "ListFeatures"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Rectangle.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Feature.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
      io.grpc.examples.routeguide.RouteSummary> METHOD_RECORD_ROUTE =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING,
          generateFullMethodName(
              "routeguide.RouteGuide", "RecordRoute"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Point.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteSummary.getDefaultInstance()));
  @io.grpc.ExperimentalApi
  public static final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.RouteNote,
      io.grpc.examples.routeguide.RouteNote> METHOD_ROUTE_CHAT =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING,
          generateFullMethodName(
              "routeguide.RouteGuide", "RouteChat"),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteNote.getDefaultInstance()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteNote.getDefaultInstance()));

  public static RouteGuideStub newStub(io.grpc.Channel channel) {
    return new RouteGuideStub(channel);
  }

  public static RouteGuideBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new RouteGuideBlockingStub(channel);
  }

  public static RouteGuideFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new RouteGuideFutureStub(channel);
  }

  public static interface RouteGuide {

    public void getFeature(io.grpc.examples.routeguide.Point request,
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver);

    public void listFeatures(io.grpc.examples.routeguide.Rectangle request,
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Point> recordRoute(
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteSummary> responseObserver);

    public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> routeChat(
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> responseObserver);
  }

  public static interface RouteGuideBlockingClient {

    public io.grpc.examples.routeguide.Feature getFeature(io.grpc.examples.routeguide.Point request);

    public java.util.Iterator<io.grpc.examples.routeguide.Feature> listFeatures(
        io.grpc.examples.routeguide.Rectangle request);
  }

  public static interface RouteGuideFutureClient {

    public com.google.common.util.concurrent.ListenableFuture<io.grpc.examples.routeguide.Feature> getFeature(
        io.grpc.examples.routeguide.Point request);
  }

  public static class RouteGuideStub extends io.grpc.stub.AbstractStub<RouteGuideStub>
      implements RouteGuide {
    private RouteGuideStub(io.grpc.Channel channel) {
      super(channel);
    }

    private RouteGuideStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected RouteGuideStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new RouteGuideStub(channel, callOptions);
    }

    @java.lang.Override
    public void getFeature(io.grpc.examples.routeguide.Point request,
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(METHOD_GET_FEATURE, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public void listFeatures(io.grpc.examples.routeguide.Rectangle request,
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
      asyncServerStreamingCall(
          getChannel().newCall(METHOD_LIST_FEATURES, getCallOptions()), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Point> recordRoute(
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteSummary> responseObserver) {
      return asyncClientStreamingCall(
          getChannel().newCall(METHOD_RECORD_ROUTE, getCallOptions()), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> routeChat(
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> responseObserver) {
      return asyncBidiStreamingCall(
          getChannel().newCall(METHOD_ROUTE_CHAT, getCallOptions()), responseObserver);
    }
  }

  public static class RouteGuideBlockingStub extends io.grpc.stub.AbstractStub<RouteGuideBlockingStub>
      implements RouteGuideBlockingClient {
    private RouteGuideBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private RouteGuideBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected RouteGuideBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new RouteGuideBlockingStub(channel, callOptions);
    }

    @java.lang.Override
    public io.grpc.examples.routeguide.Feature getFeature(io.grpc.examples.routeguide.Point request) {
      return blockingUnaryCall(
          getChannel().newCall(METHOD_GET_FEATURE, getCallOptions()), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.examples.routeguide.Feature> listFeatures(
        io.grpc.examples.routeguide.Rectangle request) {
      return blockingServerStreamingCall(
          getChannel().newCall(METHOD_LIST_FEATURES, getCallOptions()), request);
    }
  }

  public static class RouteGuideFutureStub extends io.grpc.stub.AbstractStub<RouteGuideFutureStub>
      implements RouteGuideFutureClient {
    private RouteGuideFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private RouteGuideFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected RouteGuideFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new RouteGuideFutureStub(channel, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.examples.routeguide.Feature> getFeature(
        io.grpc.examples.routeguide.Point request) {
      return futureUnaryCall(
          getChannel().newCall(METHOD_GET_FEATURE, getCallOptions()), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final RouteGuide serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder(SERVICE_NAME)
      .addMethod(
        METHOD_GET_FEATURE,
        asyncUnaryCall(
          new io.grpc.stub.ServerCalls.UnaryMethod<
              io.grpc.examples.routeguide.Point,
              io.grpc.examples.routeguide.Feature>() {
            @java.lang.Override
            public void invoke(
                io.grpc.examples.routeguide.Point request,
                io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
              serviceImpl.getFeature(request, responseObserver);
            }
          }))
      .addMethod(
        METHOD_LIST_FEATURES,
        asyncServerStreamingCall(
          new io.grpc.stub.ServerCalls.ServerStreamingMethod<
              io.grpc.examples.routeguide.Rectangle,
              io.grpc.examples.routeguide.Feature>() {
            @java.lang.Override
            public void invoke(
                io.grpc.examples.routeguide.Rectangle request,
                io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
              serviceImpl.listFeatures(request, responseObserver);
            }
          }))
      .addMethod(
        METHOD_RECORD_ROUTE,
        asyncClientStreamingCall(
          new io.grpc.stub.ServerCalls.ClientStreamingMethod<
              io.grpc.examples.routeguide.Point,
              io.grpc.examples.routeguide.RouteSummary>() {
            @java.lang.Override
            public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Point> invoke(
                io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteSummary> responseObserver) {
              return serviceImpl.recordRoute(responseObserver);
            }
          }))
      .addMethod(
        METHOD_ROUTE_CHAT,
        asyncBidiStreamingCall(
          new io.grpc.stub.ServerCalls.BidiStreamingMethod<
              io.grpc.examples.routeguide.RouteNote,
              io.grpc.examples.routeguide.RouteNote>() {
            @java.lang.Override
            public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> invoke(
                io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> responseObserver) {
              return serviceImpl.routeChat(responseObserver);
            }
          })).build();
  }
}
