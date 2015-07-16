package io.grpc.examples.routeguide;

import static io.grpc.stub.ClientCalls.asyncUnaryCall;
import static io.grpc.stub.ClientCalls.asyncServerStreamingCall;
import static io.grpc.stub.ClientCalls.asyncClientStreamingCall;
import static io.grpc.stub.ClientCalls.asyncDuplexStreamingCall;
import static io.grpc.stub.ClientCalls.blockingUnaryCall;
import static io.grpc.stub.ClientCalls.blockingServerStreamingCall;
import static io.grpc.stub.ClientCalls.unaryFutureCall;
import static io.grpc.stub.ServerCalls.asyncUnaryCall;
import static io.grpc.stub.ServerCalls.asyncServerStreamingCall;
import static io.grpc.stub.ServerCalls.asyncClientStreamingCall;
import static io.grpc.stub.ServerCalls.asyncDuplexStreamingCall;

@javax.annotation.Generated("by gRPC proto compiler")
public class RouteGuideGrpc {

  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
      io.grpc.examples.routeguide.Feature> METHOD_GET_FEATURE =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.UNARY,
          "routeguide.RouteGuide", "GetFeature",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Point.parser()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Feature.parser()));
  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Rectangle,
      io.grpc.examples.routeguide.Feature> METHOD_LIST_FEATURES =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING,
          "routeguide.RouteGuide", "ListFeatures",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Rectangle.parser()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Feature.parser()));
  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
      io.grpc.examples.routeguide.RouteSummary> METHOD_RECORD_ROUTE =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING,
          "routeguide.RouteGuide", "RecordRoute",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Point.parser()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteSummary.parser()));
  // Static method descriptors that strictly reflect the proto.
  public static final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.RouteNote,
      io.grpc.examples.routeguide.RouteNote> METHOD_ROUTE_CHAT =
      io.grpc.MethodDescriptor.create(
          io.grpc.MethodDescriptor.MethodType.DUPLEX_STREAMING,
          "routeguide.RouteGuide", "RouteChat",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteNote.parser()),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteNote.parser()));

  public static RouteGuideStub newStub(io.grpc.Channel channel) {
    return new RouteGuideStub(channel, CONFIG);
  }

  public static RouteGuideBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new RouteGuideBlockingStub(channel, CONFIG);
  }

  public static RouteGuideFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new RouteGuideFutureStub(channel, CONFIG);
  }

  // The default service descriptor
  private static final RouteGuideServiceDescriptor CONFIG =
      new RouteGuideServiceDescriptor();

  @javax.annotation.concurrent.Immutable
  public static class RouteGuideServiceDescriptor extends
      io.grpc.stub.AbstractServiceDescriptor<RouteGuideServiceDescriptor> {
    public final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
        io.grpc.examples.routeguide.Feature> getFeature;
    public final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Rectangle,
        io.grpc.examples.routeguide.Feature> listFeatures;
    public final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
        io.grpc.examples.routeguide.RouteSummary> recordRoute;
    public final io.grpc.MethodDescriptor<io.grpc.examples.routeguide.RouteNote,
        io.grpc.examples.routeguide.RouteNote> routeChat;

    private RouteGuideServiceDescriptor() {
      getFeature = METHOD_GET_FEATURE;
      listFeatures = METHOD_LIST_FEATURES;
      recordRoute = METHOD_RECORD_ROUTE;
      routeChat = METHOD_ROUTE_CHAT;
    }

    @SuppressWarnings("unchecked")
    private RouteGuideServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      getFeature = (io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
          io.grpc.examples.routeguide.Feature>) methodMap.get(
          CONFIG.getFeature.getFullMethodName());
      listFeatures = (io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Rectangle,
          io.grpc.examples.routeguide.Feature>) methodMap.get(
          CONFIG.listFeatures.getFullMethodName());
      recordRoute = (io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
          io.grpc.examples.routeguide.RouteSummary>) methodMap.get(
          CONFIG.recordRoute.getFullMethodName());
      routeChat = (io.grpc.MethodDescriptor<io.grpc.examples.routeguide.RouteNote,
          io.grpc.examples.routeguide.RouteNote>) methodMap.get(
          CONFIG.routeChat.getFullMethodName());
    }

    @java.lang.Override
    protected RouteGuideServiceDescriptor build(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      return new RouteGuideServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public java.util.Collection<io.grpc.MethodDescriptor<?, ?>> methods() {
      return com.google.common.collect.ImmutableList.<io.grpc.MethodDescriptor<?, ?>>of(
          getFeature,
          listFeatures,
          recordRoute,
          routeChat);
    }
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

  public static class RouteGuideStub extends
      io.grpc.stub.AbstractStub<RouteGuideStub, RouteGuideServiceDescriptor>
      implements RouteGuide {
    private RouteGuideStub(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config) {
      super(channel, config);
    }

    private RouteGuideStub(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      super(channel, config, callOptions);
    }

    @java.lang.Override
    protected RouteGuideStub build(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      return new RouteGuideStub(channel, config, callOptions);
    }

    @java.lang.Override
    public void getFeature(io.grpc.examples.routeguide.Point request,
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.getFeature, callOptions), request, responseObserver);
    }

    @java.lang.Override
    public void listFeatures(io.grpc.examples.routeguide.Rectangle request,
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
      asyncServerStreamingCall(
          channel.newCall(config.listFeatures, callOptions), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Point> recordRoute(
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteSummary> responseObserver) {
      return asyncClientStreamingCall(
          channel.newCall(config.recordRoute, callOptions), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> routeChat(
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> responseObserver) {
      return asyncDuplexStreamingCall(
          channel.newCall(config.routeChat, callOptions), responseObserver);
    }
  }

  public static class RouteGuideBlockingStub extends
      io.grpc.stub.AbstractStub<RouteGuideBlockingStub, RouteGuideServiceDescriptor>
      implements RouteGuideBlockingClient {
    private RouteGuideBlockingStub(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config) {
      super(channel, config);
    }

    private RouteGuideBlockingStub(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      super(channel, config, callOptions);
    }

    @java.lang.Override
    protected RouteGuideBlockingStub build(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      return new RouteGuideBlockingStub(channel, config, callOptions);
    }

    @java.lang.Override
    public io.grpc.examples.routeguide.Feature getFeature(io.grpc.examples.routeguide.Point request) {
      return blockingUnaryCall(
          channel.newCall(config.getFeature, callOptions), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.examples.routeguide.Feature> listFeatures(
        io.grpc.examples.routeguide.Rectangle request) {
      return blockingServerStreamingCall(
          channel.newCall(config.listFeatures, callOptions), request);
    }
  }

  public static class RouteGuideFutureStub extends
      io.grpc.stub.AbstractStub<RouteGuideFutureStub, RouteGuideServiceDescriptor>
      implements RouteGuideFutureClient {
    private RouteGuideFutureStub(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config) {
      super(channel, config);
    }

    private RouteGuideFutureStub(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      super(channel, config, callOptions);
    }

    @java.lang.Override
    protected RouteGuideFutureStub build(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config,
        io.grpc.CallOptions callOptions) {
      return new RouteGuideFutureStub(channel, config, callOptions);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.examples.routeguide.Feature> getFeature(
        io.grpc.examples.routeguide.Point request) {
      return unaryFutureCall(
          channel.newCall(config.getFeature, callOptions), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final RouteGuide serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder("routeguide.RouteGuide")
      .addMethod(io.grpc.ServerMethodDefinition.create(
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
            })))
      .addMethod(io.grpc.ServerMethodDefinition.create(
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
            })))
      .addMethod(io.grpc.ServerMethodDefinition.create(
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
            })))
      .addMethod(io.grpc.ServerMethodDefinition.create(
          METHOD_ROUTE_CHAT,
          asyncDuplexStreamingCall(
            new io.grpc.stub.ServerCalls.DuplexStreamingMethod<
                io.grpc.examples.routeguide.RouteNote,
                io.grpc.examples.routeguide.RouteNote>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> responseObserver) {
                return serviceImpl.routeChat(responseObserver);
              }
            }))).build();
  }
}
