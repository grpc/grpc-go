package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_HEADER;
import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_PROTORPC;
import static com.google.net.stubby.newtransport.netty.Utils.HTTP_METHOD;
import static com.google.net.stubby.newtransport.netty.Utils.STATUS_OK;

import com.google.common.base.Preconditions;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.ServerStreamListener;
import com.google.net.stubby.newtransport.ServerTransportListener;
import com.google.net.stubby.newtransport.TransportFrameUtil;

import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionDecoder;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.Http2CodecUtil;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionHandler;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2FrameAdapter;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2OutboundFlowController;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.handler.codec.http2.Http2StreamException;
import io.netty.util.ReferenceCountUtil;

import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Server-side Netty handler for GRPC processing. All event handlers are executed entirely within
 * the context of the Netty Channel thread.
 */
class NettyServerHandler extends Http2ConnectionHandler {

  private static Logger logger = Logger.getLogger(NettyServerHandler.class.getName());

  private static final Status GOAWAY_STATUS = Status.UNAVAILABLE;

  private final ServerTransportListener transportListener;
  private final DefaultHttp2InboundFlowController inboundFlow;

  NettyServerHandler(ServerTransportListener transportListener, Http2Connection connection,
      Http2FrameReader frameReader,
      Http2FrameWriter frameWriter,
      DefaultHttp2InboundFlowController inboundFlow,
      Http2OutboundFlowController outboundFlow) {
    super(connection, frameReader, frameWriter, inboundFlow, outboundFlow, new LazyFrameListener());
    this.transportListener = Preconditions.checkNotNull(transportListener, "transportListener");
    this.inboundFlow = Preconditions.checkNotNull(inboundFlow, "inboundFlow");
    initListener();
    connection.local().allowPushTo(false);
  }

  private void initListener() {
    ((LazyFrameListener) ((DefaultHttp2ConnectionDecoder) this.decoder()).listener()).setHandler(
        this);
  }

  private void onHeadersRead(ChannelHandlerContext ctx, int streamId, Http2Headers headers)
      throws Http2Exception {
    try {
      NettyServerStream stream = new NettyServerStream(ctx.channel(), streamId, inboundFlow);
      // The Http2Stream object was put by AbstractHttp2ConnectionHandler before calling this
      // method.
      Http2Stream http2Stream = connection().requireStream(streamId);
      http2Stream.data(stream);
      String method = determineMethod(streamId, headers);
      ServerStreamListener listener =
          transportListener.streamCreated(stream, method, Utils.convertHeaders(headers));
      stream.setListener(listener);
    } catch (Http2Exception e) {
      throw e;
    } catch (Throwable e) {
      logger.log(Level.WARNING, "Exception in onHeadersRead()", e);
      throw new Http2StreamException(streamId, Http2Error.INTERNAL_ERROR, e.toString());
    }
  }

  private void onDataRead(int streamId, ByteBuf data, boolean endOfStream) throws Http2Exception {
    try {
      NettyServerStream stream = serverStream(connection().requireStream(streamId));
      stream.inboundDataReceived(data, endOfStream);
    } catch (Http2Exception e) {
      throw e;
    } catch (Throwable e) {
      logger.log(Level.WARNING, "Exception in onDataRead()", e);
      throw new Http2StreamException(streamId, Http2Error.INTERNAL_ERROR, e.toString());
    }
  }

  private void onRstStreamRead(int streamId) throws Http2Exception {
    try {
      NettyServerStream stream = serverStream(connection().requireStream(streamId));
      stream.abortStream(Status.CANCELLED, false);
    } catch (Http2Exception e) {
      throw e;
    } catch (Throwable e) {
      logger.log(Level.WARNING, "Exception in onRstStreamRead()", e);
      throw new Http2StreamException(streamId, Http2Error.INTERNAL_ERROR, e.toString());
    }
  }

  @Override
  public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
    // Force the conversion of any exceptions into HTTP/2 exceptions.
    Http2Exception e = Http2CodecUtil.toHttp2Exception(cause);
    if (e instanceof Http2StreamException) {
      // Aborts the stream with a status that contains the cause.
      Http2Stream stream = connection().stream(((Http2StreamException)cause).streamId());
      if (stream != null) {
        // Send the error message to the client to help debugging.
        serverStream(stream).abortStream(Status.fromThrowable(cause), true);
        // We've already handled it, don't call the base class.
        return;
      }
    }

    // Delegate to the super class for proper handling of the Http2Exception.
    super.exceptionCaught(ctx, e);
  }

  /**
   * Handler for the Channel shutting down
   */
  @Override
  public void channelInactive(ChannelHandlerContext ctx) throws Exception {
    super.channelInactive(ctx);
    // Any streams that are still active must be closed
    for (Http2Stream stream : connection().activeStreams()) {
      serverStream(stream).abortStream(GOAWAY_STATUS, false);
    }
  }

  /**
   * Handler for commands sent from the stream.
   */
  @Override
  public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise)
      throws Http2Exception {
    if (msg instanceof SendGrpcFrameCommand) {
      SendGrpcFrameCommand cmd = (SendGrpcFrameCommand) msg;
      if (cmd.endStream()) {
        final NettyServerStream stream = serverStream(connection().requireStream(cmd.streamId()));
        promise.addListener(new ChannelFutureListener() {
          @Override
          public void operationComplete(ChannelFuture future) {
            stream.complete();
          }
        });
      }
      // Call the base class to write the HTTP/2 DATA frame.
      encoder().writeData(ctx, cmd.streamId(), cmd.content(), 0, cmd.endStream(), promise);
      ctx.flush();
    } else if (msg instanceof SendResponseHeadersCommand) {
      SendResponseHeadersCommand cmd = (SendResponseHeadersCommand) msg;
      encoder().writeHeaders(ctx,
          cmd.streamId(),
          new DefaultHttp2Headers()
            .status(STATUS_OK)
            .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_PROTORPC),
          0,
          false,
          promise);
      ctx.flush();
    } else {
      AssertionError e = new AssertionError("Write called for unexpected type: "
          + msg.getClass().getName());
      ReferenceCountUtil.release(msg);
      promise.setFailure(e);
      throw e;
    }
  }

  private String determineMethod(int streamId, Http2Headers headers)
      throws Http2StreamException {
    if (!HTTP_METHOD.equals(headers.method())) {
      throw new Http2StreamException(streamId, Http2Error.REFUSED_STREAM,
          String.format("Method '%s' is not supported", headers.method()));
    }
    if (!CONTENT_TYPE_PROTORPC.equals(headers.get(CONTENT_TYPE_HEADER))) {
      throw new Http2StreamException(streamId, Http2Error.REFUSED_STREAM,
          String.format("Header '%s'='%s', while '%s' is expected", CONTENT_TYPE_HEADER,
          headers.get(CONTENT_TYPE_HEADER), CONTENT_TYPE_PROTORPC));
    }
    String methodName = TransportFrameUtil.getFullMethodNameFromPath(headers.path().toString());
    if (methodName == null) {
      throw new Http2StreamException(streamId, Http2Error.REFUSED_STREAM,
          String.format("Malformatted path: %s", headers.path()));
    }
    return methodName;
  }

  /**
   * Returns the server stream associated to the given HTTP/2 stream object
   */
  private NettyServerStream serverStream(Http2Stream stream) {
    return stream.<NettyServerStream>data();
  }

  private static class LazyFrameListener extends Http2FrameAdapter {
    private NettyServerHandler handler;

    void setHandler(NettyServerHandler handler) {
      this.handler = handler;
    }

    @Override
    public void onDataRead(ChannelHandlerContext ctx, int streamId, ByteBuf data, int padding,
        boolean endOfStream) throws Http2Exception {
      handler.onDataRead(streamId, data, endOfStream);
    }

    @Override
    public void onHeadersRead(ChannelHandlerContext ctx,
        int streamId,
        Http2Headers headers,
        int streamDependency,
        short weight,
        boolean exclusive,
        int padding,
        boolean endStream) throws Http2Exception {
      handler.onHeadersRead(ctx, streamId, headers);
    }

    @Override
    public void onRstStreamRead(ChannelHandlerContext ctx, int streamId, long errorCode)
        throws Http2Exception {
      handler.onRstStreamRead(streamId);
    }
  }
}
