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
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetTopChannelsMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetTopChannelsRequest,
      io.grpc.channelz.v1.GetTopChannelsResponse> METHOD_GET_TOP_CHANNELS = getGetTopChannelsMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetTopChannelsRequest,
      io.grpc.channelz.v1.GetTopChannelsResponse> getGetTopChannelsMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetTopChannelsRequest,
      io.grpc.channelz.v1.GetTopChannelsResponse> getGetTopChannelsMethod() {
    return getGetTopChannelsMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetTopChannelsRequest,
      io.grpc.channelz.v1.GetTopChannelsResponse> getGetTopChannelsMethodHelper() {
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
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetServersMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServersRequest,
      io.grpc.channelz.v1.GetServersResponse> METHOD_GET_SERVERS = getGetServersMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServersRequest,
      io.grpc.channelz.v1.GetServersResponse> getGetServersMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServersRequest,
      io.grpc.channelz.v1.GetServersResponse> getGetServersMethod() {
    return getGetServersMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServersRequest,
      io.grpc.channelz.v1.GetServersResponse> getGetServersMethodHelper() {
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
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetServerSocketsMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServerSocketsRequest,
      io.grpc.channelz.v1.GetServerSocketsResponse> METHOD_GET_SERVER_SOCKETS = getGetServerSocketsMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServerSocketsRequest,
      io.grpc.channelz.v1.GetServerSocketsResponse> getGetServerSocketsMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServerSocketsRequest,
      io.grpc.channelz.v1.GetServerSocketsResponse> getGetServerSocketsMethod() {
    return getGetServerSocketsMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetServerSocketsRequest,
      io.grpc.channelz.v1.GetServerSocketsResponse> getGetServerSocketsMethodHelper() {
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
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetChannelMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetChannelRequest,
      io.grpc.channelz.v1.GetChannelResponse> METHOD_GET_CHANNEL = getGetChannelMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetChannelRequest,
      io.grpc.channelz.v1.GetChannelResponse> getGetChannelMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetChannelRequest,
      io.grpc.channelz.v1.GetChannelResponse> getGetChannelMethod() {
    return getGetChannelMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetChannelRequest,
      io.grpc.channelz.v1.GetChannelResponse> getGetChannelMethodHelper() {
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
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetSubchannelMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSubchannelRequest,
      io.grpc.channelz.v1.GetSubchannelResponse> METHOD_GET_SUBCHANNEL = getGetSubchannelMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSubchannelRequest,
      io.grpc.channelz.v1.GetSubchannelResponse> getGetSubchannelMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSubchannelRequest,
      io.grpc.channelz.v1.GetSubchannelResponse> getGetSubchannelMethod() {
    return getGetSubchannelMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSubchannelRequest,
      io.grpc.channelz.v1.GetSubchannelResponse> getGetSubchannelMethodHelper() {
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
  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  @java.lang.Deprecated // Use {@link #getGetSocketMethod()} instead. 
  public static final io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSocketRequest,
      io.grpc.channelz.v1.GetSocketResponse> METHOD_GET_SOCKET = getGetSocketMethodHelper();

  private static volatile io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSocketRequest,
      io.grpc.channelz.v1.GetSocketResponse> getGetSocketMethod;

  @io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/1901")
  public static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSocketRequest,
      io.grpc.channelz.v1.GetSocketResponse> getGetSocketMethod() {
    return getGetSocketMethodHelper();
  }

  private static io.grpc.MethodDescriptor<io.grpc.channelz.v1.GetSocketRequest,
      io.grpc.channelz.v1.GetSocketResponse> getGetSocketMethodHelper() {
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
      asyncUnimplementedUnaryCall(getGetTopChannelsMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Gets all servers that exist in the process.
     * </pre>
     */
    public void getServers(io.grpc.channelz.v1.GetServersRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServersResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetServersMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Gets all server sockets that exist in the process.
     * </pre>
     */
    public void getServerSockets(io.grpc.channelz.v1.GetServerSocketsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServerSocketsResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetServerSocketsMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Returns a single Channel, or else a NOT_FOUND code.
     * </pre>
     */
    public void getChannel(io.grpc.channelz.v1.GetChannelRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetChannelResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetChannelMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Returns a single Subchannel, or else a NOT_FOUND code.
     * </pre>
     */
    public void getSubchannel(io.grpc.channelz.v1.GetSubchannelRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSubchannelResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetSubchannelMethodHelper(), responseObserver);
    }

    /**
     * <pre>
     * Returns a single Socket or else a NOT_FOUND code.
     * </pre>
     */
    public void getSocket(io.grpc.channelz.v1.GetSocketRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSocketResponse> responseObserver) {
      asyncUnimplementedUnaryCall(getGetSocketMethodHelper(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getGetTopChannelsMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetTopChannelsRequest,
                io.grpc.channelz.v1.GetTopChannelsResponse>(
                  this, METHODID_GET_TOP_CHANNELS)))
          .addMethod(
            getGetServersMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetServersRequest,
                io.grpc.channelz.v1.GetServersResponse>(
                  this, METHODID_GET_SERVERS)))
          .addMethod(
            getGetServerSocketsMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetServerSocketsRequest,
                io.grpc.channelz.v1.GetServerSocketsResponse>(
                  this, METHODID_GET_SERVER_SOCKETS)))
          .addMethod(
            getGetChannelMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetChannelRequest,
                io.grpc.channelz.v1.GetChannelResponse>(
                  this, METHODID_GET_CHANNEL)))
          .addMethod(
            getGetSubchannelMethodHelper(),
            asyncUnaryCall(
              new MethodHandlers<
                io.grpc.channelz.v1.GetSubchannelRequest,
                io.grpc.channelz.v1.GetSubchannelResponse>(
                  this, METHODID_GET_SUBCHANNEL)))
          .addMethod(
            getGetSocketMethodHelper(),
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
          getChannel().newCall(getGetTopChannelsMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Gets all servers that exist in the process.
     * </pre>
     */
    public void getServers(io.grpc.channelz.v1.GetServersRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServersResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetServersMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Gets all server sockets that exist in the process.
     * </pre>
     */
    public void getServerSockets(io.grpc.channelz.v1.GetServerSocketsRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetServerSocketsResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetServerSocketsMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Returns a single Channel, or else a NOT_FOUND code.
     * </pre>
     */
    public void getChannel(io.grpc.channelz.v1.GetChannelRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetChannelResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetChannelMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Returns a single Subchannel, or else a NOT_FOUND code.
     * </pre>
     */
    public void getSubchannel(io.grpc.channelz.v1.GetSubchannelRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSubchannelResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetSubchannelMethodHelper(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * Returns a single Socket or else a NOT_FOUND code.
     * </pre>
     */
    public void getSocket(io.grpc.channelz.v1.GetSocketRequest request,
        io.grpc.stub.StreamObserver<io.grpc.channelz.v1.GetSocketResponse> responseObserver) {
      asyncUnaryCall(
          getChannel().newCall(getGetSocketMethodHelper(), getCallOptions()), request, responseObserver);
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
          getChannel(), getGetTopChannelsMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Gets all servers that exist in the process.
     * </pre>
     */
    public io.grpc.channelz.v1.GetServersResponse getServers(io.grpc.channelz.v1.GetServersRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetServersMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Gets all server sockets that exist in the process.
     * </pre>
     */
    public io.grpc.channelz.v1.GetServerSocketsResponse getServerSockets(io.grpc.channelz.v1.GetServerSocketsRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetServerSocketsMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Returns a single Channel, or else a NOT_FOUND code.
     * </pre>
     */
    public io.grpc.channelz.v1.GetChannelResponse getChannel(io.grpc.channelz.v1.GetChannelRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetChannelMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Returns a single Subchannel, or else a NOT_FOUND code.
     * </pre>
     */
    public io.grpc.channelz.v1.GetSubchannelResponse getSubchannel(io.grpc.channelz.v1.GetSubchannelRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetSubchannelMethodHelper(), getCallOptions(), request);
    }

    /**
     * <pre>
     * Returns a single Socket or else a NOT_FOUND code.
     * </pre>
     */
    public io.grpc.channelz.v1.GetSocketResponse getSocket(io.grpc.channelz.v1.GetSocketRequest request) {
      return blockingUnaryCall(
          getChannel(), getGetSocketMethodHelper(), getCallOptions(), request);
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
          getChannel().newCall(getGetTopChannelsMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Gets all servers that exist in the process.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetServersResponse> getServers(
        io.grpc.channelz.v1.GetServersRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetServersMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Gets all server sockets that exist in the process.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetServerSocketsResponse> getServerSockets(
        io.grpc.channelz.v1.GetServerSocketsRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetServerSocketsMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Returns a single Channel, or else a NOT_FOUND code.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetChannelResponse> getChannel(
        io.grpc.channelz.v1.GetChannelRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetChannelMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Returns a single Subchannel, or else a NOT_FOUND code.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetSubchannelResponse> getSubchannel(
        io.grpc.channelz.v1.GetSubchannelRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetSubchannelMethodHelper(), getCallOptions()), request);
    }

    /**
     * <pre>
     * Returns a single Socket or else a NOT_FOUND code.
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<io.grpc.channelz.v1.GetSocketResponse> getSocket(
        io.grpc.channelz.v1.GetSocketRequest request) {
      return futureUnaryCall(
          getChannel().newCall(getGetSocketMethodHelper(), getCallOptions()), request);
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
              .addMethod(getGetTopChannelsMethodHelper())
              .addMethod(getGetServersMethodHelper())
              .addMethod(getGetServerSocketsMethodHelper())
              .addMethod(getGetChannelMethodHelper())
              .addMethod(getGetSubchannelMethodHelper())
              .addMethod(getGetSocketMethodHelper())
              .build();
        }
      }
    }
    return result;
  }
}
