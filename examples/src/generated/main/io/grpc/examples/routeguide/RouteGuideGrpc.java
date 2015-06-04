package io.grpc.examples.routeguide;

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
public class RouteGuideGrpc {

  private static final io.grpc.stub.Method<io.grpc.examples.routeguide.Point,
      io.grpc.examples.routeguide.Feature> METHOD_GET_FEATURE =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.UNARY, "GetFeature",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Point.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Feature.PARSER));
  private static final io.grpc.stub.Method<io.grpc.examples.routeguide.Rectangle,
      io.grpc.examples.routeguide.Feature> METHOD_LIST_FEATURES =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.SERVER_STREAMING, "ListFeatures",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Rectangle.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Feature.PARSER));
  private static final io.grpc.stub.Method<io.grpc.examples.routeguide.Point,
      io.grpc.examples.routeguide.RouteSummary> METHOD_RECORD_ROUTE =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.CLIENT_STREAMING, "RecordRoute",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.Point.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteSummary.PARSER));
  private static final io.grpc.stub.Method<io.grpc.examples.routeguide.RouteNote,
      io.grpc.examples.routeguide.RouteNote> METHOD_ROUTE_CHAT =
      io.grpc.stub.Method.create(
          io.grpc.MethodType.DUPLEX_STREAMING, "RouteChat",
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteNote.PARSER),
          io.grpc.protobuf.ProtoUtils.marshaller(io.grpc.examples.routeguide.RouteNote.PARSER));

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

  public static final RouteGuideServiceDescriptor CONFIG =
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
      getFeature = createMethodDescriptor(
          "routeguide.RouteGuide", METHOD_GET_FEATURE);
      listFeatures = createMethodDescriptor(
          "routeguide.RouteGuide", METHOD_LIST_FEATURES);
      recordRoute = createMethodDescriptor(
          "routeguide.RouteGuide", METHOD_RECORD_ROUTE);
      routeChat = createMethodDescriptor(
          "routeguide.RouteGuide", METHOD_ROUTE_CHAT);
    }

    @SuppressWarnings("unchecked")
    private RouteGuideServiceDescriptor(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      getFeature = (io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
          io.grpc.examples.routeguide.Feature>) methodMap.get(
          CONFIG.getFeature.getName());
      listFeatures = (io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Rectangle,
          io.grpc.examples.routeguide.Feature>) methodMap.get(
          CONFIG.listFeatures.getName());
      recordRoute = (io.grpc.MethodDescriptor<io.grpc.examples.routeguide.Point,
          io.grpc.examples.routeguide.RouteSummary>) methodMap.get(
          CONFIG.recordRoute.getName());
      routeChat = (io.grpc.MethodDescriptor<io.grpc.examples.routeguide.RouteNote,
          io.grpc.examples.routeguide.RouteNote>) methodMap.get(
          CONFIG.routeChat.getName());
    }

    @java.lang.Override
    protected RouteGuideServiceDescriptor build(
        java.util.Map<java.lang.String, io.grpc.MethodDescriptor<?, ?>> methodMap) {
      return new RouteGuideServiceDescriptor(methodMap);
    }

    @java.lang.Override
    public com.google.common.collect.ImmutableList<io.grpc.MethodDescriptor<?, ?>> methods() {
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

    @java.lang.Override
    protected RouteGuideStub build(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config) {
      return new RouteGuideStub(channel, config);
    }

    @java.lang.Override
    public void getFeature(io.grpc.examples.routeguide.Point request,
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
      asyncUnaryCall(
          channel.newCall(config.getFeature), request, responseObserver);
    }

    @java.lang.Override
    public void listFeatures(io.grpc.examples.routeguide.Rectangle request,
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
      asyncServerStreamingCall(
          channel.newCall(config.listFeatures), request, responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Point> recordRoute(
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteSummary> responseObserver) {
      return asyncClientStreamingCall(
          channel.newCall(config.recordRoute), responseObserver);
    }

    @java.lang.Override
    public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> routeChat(
        io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteNote> responseObserver) {
      return duplexStreamingCall(
          channel.newCall(config.routeChat), responseObserver);
    }
  }

  public static class RouteGuideBlockingStub extends
      io.grpc.stub.AbstractStub<RouteGuideBlockingStub, RouteGuideServiceDescriptor>
      implements RouteGuideBlockingClient {
    private RouteGuideBlockingStub(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected RouteGuideBlockingStub build(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config) {
      return new RouteGuideBlockingStub(channel, config);
    }

    @java.lang.Override
    public io.grpc.examples.routeguide.Feature getFeature(io.grpc.examples.routeguide.Point request) {
      return blockingUnaryCall(
          channel.newCall(config.getFeature), request);
    }

    @java.lang.Override
    public java.util.Iterator<io.grpc.examples.routeguide.Feature> listFeatures(
        io.grpc.examples.routeguide.Rectangle request) {
      return blockingServerStreamingCall(
          channel.newCall(config.listFeatures), request);
    }
  }

  public static class RouteGuideFutureStub extends
      io.grpc.stub.AbstractStub<RouteGuideFutureStub, RouteGuideServiceDescriptor>
      implements RouteGuideFutureClient {
    private RouteGuideFutureStub(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config) {
      super(channel, config);
    }

    @java.lang.Override
    protected RouteGuideFutureStub build(io.grpc.Channel channel,
        RouteGuideServiceDescriptor config) {
      return new RouteGuideFutureStub(channel, config);
    }

    @java.lang.Override
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.examples.routeguide.Feature> getFeature(
        io.grpc.examples.routeguide.Point request) {
      return unaryFutureCall(
          channel.newCall(config.getFeature), request);
    }
  }

  public static io.grpc.ServerServiceDefinition bindService(
      final RouteGuide serviceImpl) {
    return io.grpc.ServerServiceDefinition.builder("routeguide.RouteGuide")
      .addMethod(createMethodDefinition(
          METHOD_GET_FEATURE,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.examples.routeguide.Point,
                io.grpc.examples.routeguide.Feature>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.examples.routeguide.Point request,
                  io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
                serviceImpl.getFeature(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_LIST_FEATURES,
          asyncUnaryRequestCall(
            new io.grpc.stub.ServerCalls.UnaryRequestMethod<
                io.grpc.examples.routeguide.Rectangle,
                io.grpc.examples.routeguide.Feature>() {
              @java.lang.Override
              public void invoke(
                  io.grpc.examples.routeguide.Rectangle request,
                  io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Feature> responseObserver) {
                serviceImpl.listFeatures(request, responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_RECORD_ROUTE,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
                io.grpc.examples.routeguide.Point,
                io.grpc.examples.routeguide.RouteSummary>() {
              @java.lang.Override
              public io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.Point> invoke(
                  io.grpc.stub.StreamObserver<io.grpc.examples.routeguide.RouteSummary> responseObserver) {
                return serviceImpl.recordRoute(responseObserver);
              }
            })))
      .addMethod(createMethodDefinition(
          METHOD_ROUTE_CHAT,
          asyncStreamingRequestCall(
            new io.grpc.stub.ServerCalls.StreamingRequestMethod<
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
