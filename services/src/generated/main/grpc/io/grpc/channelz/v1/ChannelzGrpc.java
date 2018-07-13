package io.grpc.channelz.v1;

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
 * Channelz is a service exposed by gRPC servers that provides detailed debug
 * information.
 * </pre>
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler",
    comments = "Source: grpc/channelz/v1/channelz.proto")
public final class ChannelzGrpc {

  private ChannelzGrpc() {}

  public static final String SERVICE_NAME = "grpc.channelz.v1.Channelz";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetTopChannelsRequest,
      io.grpc.channelz.v1.GetTopChannelsResponse> getGetTopChannelsMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetTopChannels",
      requestType = io.grpc.channelz.v1.GetTopChannelsRequest.class,
      responseType = io.grpc.channelz.v1.GetTopChannelsResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetTopChannelsRequest,
      io.grpc.channelz.v1.GetTopChannelsResponse> getGetTopChannelsMethod() {
    io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetTopChannelsRequest, io.grpc.channelz.v1.GetTopChannelsResponse> getGetTopChannelsMethod;
    if ((getGetTopChannelsMethod = ChannelzGrpc.getGetTopChannelsMethod) == null) {
      synchronized (ChannelzGrpc.class) {
        if ((getGetTopChannelsMethod = ChannelzGrpc.getGetTopChannelsMethod) == null) {
          ChannelzGrpc.getGetTopChannelsMethod = getGetTopChannelsMethod = 
              io.grpc.MethodDescriptor.<io.grpc.channelz.v1.GetTopChannelsRequest, io.grpc.channelz.v1.GetTopChannelsResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.channelz.v1.Channelz", "GetTopChannels"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetTopChannelsRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetTopChannelsResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ChannelzMethodDescriptorSupplier("GetTopChannels"))
                  .build();
          }
        }
     }
     return getGetTopChannelsMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServersRequest,
      io.grpc.channelz.v1.GetServersResponse> getGetServersMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetServers",
      requestType = io.grpc.channelz.v1.GetServersRequest.class,
      responseType = io.grpc.channelz.v1.GetServersResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServersRequest,
      io.grpc.channelz.v1.GetServersResponse> getGetServersMethod() {
    io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServersRequest, io.grpc.channelz.v1.GetServersResponse> getGetServersMethod;
    if ((getGetServersMethod = ChannelzGrpc.getGetServersMethod) == null) {
      synchronized (ChannelzGrpc.class) {
        if ((getGetServersMethod = ChannelzGrpc.getGetServersMethod) == null) {
          ChannelzGrpc.getGetServersMethod = getGetServersMethod = 
              io.grpc.MethodDescriptor.<io.grpc.channelz.v1.GetServersRequest, io.grpc.channelz.v1.GetServersResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.channelz.v1.Channelz", "GetServers"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetServersRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetServersResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ChannelzMethodDescriptorSupplier("GetServers"))
                  .build();
          }
        }
     }
     return getGetServersMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServerSocketsRequest,
      io.grpc.channelz.v1.GetServerSocketsResponse> getGetServerSocketsMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetServerSockets",
      requestType = io.grpc.channelz.v1.GetServerSocketsRequest.class,
      responseType = io.grpc.channelz.v1.GetServerSocketsResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServerSocketsRequest,
      io.grpc.channelz.v1.GetServerSocketsResponse> getGetServerSocketsMethod() {
    io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServerSocketsRequest, io.grpc.channelz.v1.GetServerSocketsResponse> getGetServerSocketsMethod;
    if ((getGetServerSocketsMethod = ChannelzGrpc.getGetServerSocketsMethod) == null) {
      synchronized (ChannelzGrpc.class) {
        if ((getGetServerSocketsMethod = ChannelzGrpc.getGetServerSocketsMethod) == null) {
          ChannelzGrpc.getGetServerSocketsMethod = getGetServerSocketsMethod = 
              io.grpc.MethodDescriptor.<io.grpc.channelz.v1.GetServerSocketsRequest, io.grpc.channelz.v1.GetServerSocketsResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.channelz.v1.Channelz", "GetServerSockets"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetServerSocketsRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetServerSocketsResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ChannelzMethodDescriptorSupplier("GetServerSockets"))
                  .build();
          }
        }
     }
     return getGetServerSocketsMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetChannelRequest,
      io.grpc.channelz.v1.GetChannelResponse> getGetChannelMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetChannel",
      requestType = io.grpc.channelz.v1.GetChannelRequest.class,
      responseType = io.grpc.channelz.v1.GetChannelResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetChannelRequest,
      io.grpc.channelz.v1.GetChannelResponse> getGetChannelMethod() {
    io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetChannelRequest, io.grpc.channelz.v1.GetChannelResponse> getGetChannelMethod;
    if ((getGetChannelMethod = ChannelzGrpc.getGetChannelMethod) == null) {
      synchronized (ChannelzGrpc.class) {
        if ((getGetChannelMethod = ChannelzGrpc.getGetChannelMethod) == null) {
          ChannelzGrpc.getGetChannelMethod = getGetChannelMethod = 
              io.grpc.MethodDescriptor.<io.grpc.channelz.v1.GetChannelRequest, io.grpc.channelz.v1.GetChannelResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.channelz.v1.Channelz", "GetChannel"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetChannelRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetChannelResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ChannelzMethodDescriptorSupplier("GetChannel"))
                  .build();
          }
        }
     }
     return getGetChannelMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSubchannelRequest,
      io.grpc.channelz.v1.GetSubchannelResponse> getGetSubchannelMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetSubchannel",
      requestType = io.grpc.channelz.v1.GetSubchannelRequest.class,
      responseType = io.grpc.channelz.v1.GetSubchannelResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSubchannelRequest,
      io.grpc.channelz.v1.GetSubchannelResponse> getGetSubchannelMethod() {
    io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSubchannelRequest, io.grpc.channelz.v1.GetSubchannelResponse> getGetSubchannelMethod;
    if ((getGetSubchannelMethod = ChannelzGrpc.getGetSubchannelMethod) == null) {
      synchronized (ChannelzGrpc.class) {
        if ((getGetSubchannelMethod = ChannelzGrpc.getGetSubchannelMethod) == null) {
          ChannelzGrpc.getGetSubchannelMethod = getGetSubchannelMethod = 
              io.grpc.MethodDescriptor.<io.grpc.channelz.v1.GetSubchannelRequest, io.grpc.channelz.v1.GetSubchannelResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.channelz.v1.Channelz", "GetSubchannel"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetSubchannelRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetSubchannelResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ChannelzMethodDescriptorSupplier("GetSubchannel"))
                  .build();
          }
        }
     }
     return getGetSubchannelMethod;
  }

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSocketRequest,
      io.grpc.channelz.v1.GetSocketResponse> getGetSocketMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetSocket",
      requestType = io.grpc.channelz.v1.GetSocketRequest.class,
      responseType = io.grpc.channelz.v1.GetSocketResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSocketRequest,
      io.grpc.channelz.v1.GetSocketResponse> getGetSocketMethod() {
    io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSocketRequest, io.grpc.channelz.v1.GetSocketResponse> getGetSocketMethod;
    if ((getGetSocketMethod = ChannelzGrpc.getGetSocketMethod) == null) {
      synchronized (ChannelzGrpc.class) {
        if ((getGetSocketMethod = ChannelzGrpc.getGetSocketMethod) == null) {
          ChannelzGrpc.getGetSocketMethod = getGetSocketMethod = 
              io.grpc.MethodDescriptor.<io.grpc.channelz.v1.GetSocketRequest, io.grpc.channelz.v1.GetSocketResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(
                  "grpc.channelz.v1.Channelz", "GetSocket"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetSocketRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  io.grpc.channelz.v1.GetSocketResponse.getDefaultInstance()))
                  .setSchemaDescriptor(new ChannelzMethodDescriptorSupplier("GetSocket"))
                  .build();
          }
        }
     }
     return getGetSocketMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static ChannelzStub newStub(io.grpc.Channel channel) {
    return new ChannelzStub(channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static ChannelzBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    return new ChannelzBlockingStub(channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static ChannelzFutureStub newFutureStub(
      io.grpc.Channel channel) {
    return new ChannelzFutureStub(channel);
  }

  /**
   * <pre>
   * Channelz is a service exposed by gRPC servers that provides detailed debug
   * information.
   * </pre>
   */
  public static abstract class ChannelzImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * Gets all root channels (i.e. channels the application has directly
     * created). This does not include subchannels nor non-top level channels.
     * </pre>
     */
    public void getTopChannels(io.grpc.channelz.v1.GetTopChannelsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetTopChannelsResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetTopChannelsMethod(), responseObserver);
    }

    /**
     * <pre>
     * Gets all servers that exist in the process.
     * </pre>
     */
    public void getServers(io.grpc.channelz.v1.GetServersRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServersResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetServersMethod(), responseObserver);
    }

    /**
     * <pre>
     * Gets all server sockets that exist in the process.
     * </pre>
     */
    public void getServerSockets(io.grpc.channelz.v1.GetServerSocketsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServerSocketsResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetServerSocketsMethod(), responseObserver);
    }

    /**
     * <pre>
     * Returns a single Channel, or else a NOT_FOUND code.
     * </pre>
     */
    public void getChannel(io.grpc.channelz.v1.GetChannelRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetChannelResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetChannelMethod(), responseObserver);
    }

    /**
     * <pre>
     * Returns a single Subchannel, or else a NOT_FOUND code.
     * </pre>
     */
    public void getSubchannel(io.grpc.channelz.v1.GetSubchannelRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSubchannelResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetSubchannelMethod(), responseObserver);
    }

    /**
     * <pre>
     * Returns a single Socket or else a NOT_FOUND code.
     * </pre>
     */
    public void getSocket(io.grpc.channelz.v1.GetSocketRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSocketResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetSocketMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getGetTopChannelsMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetTopChannelsRequest,
                io.grpc.channelz.v1.GetTopChannelsResponse>(
                  this, METHODID_GET_TOP_CHANNELS)))
          .addMethod(
            getGetServersMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetServersRequest,
                io.grpc.channelz.v1.GetServersResponse>(
                  this, METHODID_GET_SERVERS)))
          .addMethod(
            getGetServerSocketsMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetServerSocketsRequest,
                io.grpc.channelz.v1.GetServerSocketsResponse>(
                  this, METHODID_GET_SERVER_SOCKETS)))
          .addMethod(
            getGetChannelMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetChannelRequest,
                io.grpc.channelz.v1.GetChannelResponse>(
                  this, METHODID_GET_CHANNEL)))
          .addMethod(
            getGetSubchannelMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetSubchannelRequest,
                io.grpc.channelz.v1.GetSubchannelResponse>(
                  this, METHODID_GET_SUBCHANNEL)))
          .addMethod(
            getGetSocketMethod(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetSocketRequest,
                io.grpc.channelz.v1.GetSocketResponse>(
                  this, METHODID_GET_SOCKET)))
          .build();
    }
  }

  /**
   * <pre>
   * Channelz is a service exposed by gRPC servers that provides detailed debug
   * information.
   * </pre>
   */
  public static final class ChannelzStub extends io.grpc.stub.AbstractStub<ChannelzStub> {
    private ChannelzStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ChannelzStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ChannelzStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ChannelzStub(channel, callOptions);
    }

    /**
     * <pre>
     * Gets all root channels (i.e. channels the application has directly
     * created). This does not include subchannels nor non-top level channels.
     * </pre>
     */
    public void getTopChannels(io.grpc.channelz.v1.GetTopChannelsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetTopChannelsResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetTopChannelsMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Gets all servers that exist in the process.
     * </pre>
     */
    public void getServers(io.grpc.channelz.v1.GetServersRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServersResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetServersMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Gets all server sockets that exist in the process.
     * </pre>
     */
    public void getServerSockets(io.grpc.channelz.v1.GetServerSocketsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServerSocketsResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetServerSocketsMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Returns a single Channel, or else a NOT_FOUND code.
     * </pre>
     */
    public void getChannel(io.grpc.channelz.v1.GetChannelRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetChannelResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetChannelMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Returns a single Subchannel, or else a NOT_FOUND code.
     * </pre>
     */
    public void getSubchannel(io.grpc.channelz.v1.GetSubchannelRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSubchannelResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetSubchannelMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Returns a single Socket or else a NOT_FOUND code.
     * </pre>
     */
    public void getSocket(io.grpc.channelz.v1.GetSocketRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSocketResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetSocketMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   * <pre>
   * Channelz is a service exposed by gRPC servers that provides detailed debug
   * information.
   * </pre>
   */
  public static final class ChannelzBlockingStub extends io.grpc.stub.AbstractStub<ChannelzBlockingStub> {
    private ChannelzBlockingStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ChannelzBlockingStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ChannelzBlockingStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ChannelzBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * Gets all root channels (i.e. channels the application has directly
     * created). This does not include subchannels nor non-top level channels.
     * </pre>
     */
    public io.grpc.channelz.v1.GetTopChannelsResponse getTopChannels(io.grpc.channelz.v1.GetTopChannelsRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetTopChannelsMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Gets all servers that exist in the process.
     * </pre>
     */
    public io.grpc.channelz.v1.GetServersResponse getServers(io.grpc.channelz.v1.GetServersRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetServersMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Gets all server sockets that exist in the process.
     * </pre>
     */
    public io.grpc.channelz.v1.GetServerSocketsResponse getServerSockets(io.grpc.channelz.v1.GetServerSocketsRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetServerSocketsMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Returns a single Channel, or else a NOT_FOUND code.
     * </pre>
     */
    public io.grpc.channelz.v1.GetChannelResponse getChannel(io.grpc.channelz.v1.GetChannelRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetChannelMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Returns a single Subchannel, or else a NOT_FOUND code.
     * </pre>
     */
    public io.grpc.channelz.v1.GetSubchannelResponse getSubchannel(io.grpc.channelz.v1.GetSubchannelRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetSubchannelMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Returns a single Socket or else a NOT_FOUND code.
     * </pre>
     */
    public io.grpc.channelz.v1.GetSocketResponse getSocket(io.grpc.channelz.v1.GetSocketRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetSocketMethod(), getCallOptions(), request);
    }
  }

  /**
   * <pre>
   * Channelz is a service exposed by gRPC servers that provides detailed debug
   * information.
   * </pre>
   */
  public static final class ChannelzFutureStub extends io.grpc.stub.AbstractStub<ChannelzFutureStub> {
    private ChannelzFutureStub(io.grpc.Channel channel) {
      super(channel);
    }

    private ChannelzFutureStub(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected ChannelzFutureStub build(io.grpc.Channel channel,
        io.grpc.CallOptions callOptions) {
      return new ChannelzFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * Gets all root channels (i.e. channels the application has directly
     * created). This does not include subchannels nor non-top level channels.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetTopChannelsResponse> getTopChannels(
        io.grpc.channelz.v1.GetTopChannelsRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetTopChannelsMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Gets all servers that exist in the process.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetServersResponse> getServers(
        io.grpc.channelz.v1.GetServersRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetServersMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Gets all server sockets that exist in the process.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetServerSocketsResponse> getServerSockets(
        io.grpc.channelz.v1.GetServerSocketsRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetServerSocketsMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Returns a single Channel, or else a NOT_FOUND code.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetChannelResponse> getChannel(
        io.grpc.channelz.v1.GetChannelRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetChannelMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Returns a single Subchannel, or else a NOT_FOUND code.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetSubchannelResponse> getSubchannel(
        io.grpc.channelz.v1.GetSubchannelRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetSubchannelMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Returns a single Socket or else a NOT_FOUND code.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetSocketResponse> getSocket(
        io.grpc.channelz.v1.GetSocketRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetSocketMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_GET_TOP_CHANNELS = 0;
  private static final int METHODID_GET_SERVERS = 1;
  private static final int METHODID_GET_SERVER_SOCKETS = 2;
  private static final int METHODID_GET_CHANNEL = 3;
  private static final int METHODID_GET_SUBCHANNEL = 4;
  private static final int METHODID_GET_SOCKET = 5;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final ChannelzImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(ChannelzImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_GET_TOP_CHANNELS:
          serviceImpl.getTopChannels((io.grpc.channelz.v1.GetTopChannelsRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetTopChannelsResponse>) responseObserver);
          break;
        case METHODID_GET_SERVERS:
          serviceImpl.getServers((io.grpc.channelz.v1.GetServersRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServersResponse>) responseObserver);
          break;
        case METHODID_GET_SERVER_SOCKETS:
          serviceImpl.getServerSockets((io.grpc.channelz.v1.GetServerSocketsRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServerSocketsResponse>) responseObserver);
          break;
        case METHODID_GET_CHANNEL:
          serviceImpl.getChannel((io.grpc.channelz.v1.GetChannelRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetChannelResponse>) responseObserver);
          break;
        case METHODID_GET_SUBCHANNEL:
          serviceImpl.getSubchannel((io.grpc.channelz.v1.GetSubchannelRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSubchannelResponse>) responseObserver);
          break;
        case METHODID_GET_SOCKET:
          serviceImpl.getSocket((io.grpc.channelz.v1.GetSocketRequest) request,
              (io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSocketResponse>) responseObserver);
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
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class ChannelzBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    ChannelzBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return io.grpc.channelz.v1.ChannelzProto.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("Channelz");
    }
  }

  private static final class ChannelzFileDescriptorSupplier
      extends ChannelzBaseDescriptorSupplier {
    ChannelzFileDescriptorSupplier() {}
  }

  private static final class ChannelzMethodDescriptorSupplier
      extends ChannelzBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    ChannelzMethodDescriptorSupplier(String methodName) {
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
      synchronized (ChannelzGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new ChannelzFileDescriptorSupplier())
              .addMethod(getGetTopChannelsMethod())
              .addMethod(getGetServersMethod())
              .addMethod(getGetServerSocketsMethod())
              .addMethod(getGetChannelMethod())
              .addMethod(getGetSubchannelMethod())
              .addMethod(getGetSocketMethod())
              .build();
        }
      }
    }
    return result;
  }
}
