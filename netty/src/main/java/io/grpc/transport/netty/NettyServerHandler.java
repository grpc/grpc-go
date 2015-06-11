/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.transport.netty;

import static io.grpc.transport.netty.Utils.CONTENT_TYPE_GRPC;
import static io.grpc.transport.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.transport.netty.Utils.HTTP_METHOD;
import static io.grpc.transport.netty.Utils.TE_HEADER;
import static io.grpc.transport.netty.Utils.TE_TRAILERS;

import com.google.common.base.Preconditions;

import io.grpc.Status;
import io.grpc.transport.ServerStreamListener;
import io.grpc.transport.ServerTransportListener;
import io.grpc.transport.TransportFrameUtil;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionHandler;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2Exception.StreamException;
import io.netty.handler.codec.http2.Http2FrameAdapter;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Settings;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.handler.codec.http2.Http2StreamVisitor;
import io.netty.util.ByteString;
import io.netty.util.ReferenceCountUtil;

import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;

/**
 * Server-side Netty handler for GRPC processing. All event handlers are executed entirely within
 * the context of the Netty Channel thread.
 */
class NettyServerHandler extends Http2ConnectionHandler {

  private static Logger logger = Logger.getLogger(NettyServerHandler.class.getName());

  private static final Status GOAWAY_STATUS = Status.UNAVAILABLE;

  private final Http2Connection.PropertyKey streamKey;
  private final ServerTransportListener transportListener;
  private Throwable connectionError;
  private ChannelHandlerContext ctx;
  private boolean teWarningLogged;
  private int connectionWindowSize;
  private Http2Settings initialSettings = new Http2Settings();
  private WriteQueue serverWriteQueue;

  NettyServerHandler(ServerTransportListener transportListener,
      Http2Connection connection,
      Http2FrameReader frameReader,
      Http2FrameWriter frameWriter,
      int maxStreams,
      int connectionWindowSize,
      int streamWindowSize) {
    super(connection, frameReader, frameWriter, new LazyFrameListener());
    Preconditions.checkArgument(connectionWindowSize > 0, "connectionWindowSize must be positive");
    this.connectionWindowSize = connectionWindowSize;

    streamKey = connection.newKey();
    this.transportListener = Preconditions.checkNotNull(transportListener, "transportListener");
    initListener();

    // TODO(nmittler): this is a temporary hack as we currently have to send a 2nd SETTINGS
    // frame. Once we upgrade to Netty 4.1.Beta6 we'll be able to pass in the initial SETTINGS
    // to the super class constructor.
    initialSettings.initialWindowSize(streamWindowSize);
    initialSettings.maxConcurrentStreams(maxStreams);
  }

  @Nullable
  Throwable connectionError() {
    return connectionError;
  }

  private void initListener() {
    ((LazyFrameListener) decoder().listener()).setHandler(this);
  }

  @Override
  public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
    this.ctx = ctx;
    serverWriteQueue = new WriteQueue(ctx.channel());
    super.handlerAdded(ctx);
    sendInitialSettings();
  }

  @Override
  public void channelActive(ChannelHandlerContext ctx) throws Exception {
    // Sends connection preface if we haven't already.
    super.channelActive(ctx);
    sendInitialSettings();
  }

  private void onHeadersRead(ChannelHandlerContext ctx, int streamId, Http2Headers headers)
      throws Http2Exception {
    if (!teWarningLogged && !TE_TRAILERS.equals(headers.get(TE_HEADER))) {
      logger.warning(String.format("Expected header TE: %s, but %s is received. This means "
            + "some intermediate proxy may not support trailers",
            TE_TRAILERS, headers.get(TE_HEADER)));
      teWarningLogged = true;
    }

    try {
      // The Http2Stream object was put by AbstractHttp2ConnectionHandler before calling this
      // method.
      Http2Stream http2Stream = requireHttp2Stream(streamId);
      NettyServerStream stream = new NettyServerStream(ctx.channel(), http2Stream, this);
      http2Stream.setProperty(streamKey, stream);
      String method = determineMethod(streamId, headers);
      ServerStreamListener listener =
          transportListener.streamCreated(stream, method, Utils.convertHeaders(headers));
      stream.setListener(listener);
    } catch (Http2Exception e) {
      throw e;
    } catch (Throwable e) {
      logger.log(Level.WARNING, "Exception in onHeadersRead()", e);
      throw newStreamException(streamId, e);
    }
  }

  private void onDataRead(int streamId, ByteBuf data, boolean endOfStream) throws Http2Exception {
    try {
      NettyServerStream stream = serverStream(requireHttp2Stream(streamId));
      stream.inboundDataReceived(data, endOfStream);
    } catch (Throwable e) {
      logger.log(Level.WARNING, "Exception in onDataRead()", e);
      throw newStreamException(streamId, e);
    }
  }

  private void onRstStreamRead(int streamId) throws Http2Exception {
    try {
      NettyServerStream stream = serverStream(requireHttp2Stream(streamId));
      stream.abortStream(Status.CANCELLED, false);
    } catch (Throwable e) {
      logger.log(Level.WARNING, "Exception in onRstStreamRead()", e);
      throw newStreamException(streamId, e);
    }
  }

  @Override
  protected void onConnectionError(ChannelHandlerContext ctx, Throwable cause,
      Http2Exception http2Ex) {
    logger.log(Level.WARNING, "Connection Error", cause);
    connectionError = cause;
    super.onConnectionError(ctx, cause, http2Ex);
  }

  @Override
  protected void onStreamError(ChannelHandlerContext ctx, Throwable cause,
      StreamException http2Ex) {
    logger.log(Level.WARNING, "Stream Error", cause);
    Http2Stream stream = connection().stream(Http2Exception.streamId(http2Ex));
    if (stream != null) {
      // Abort the stream with a status to help the client with debugging.
      // Don't need to send a RST_STREAM since the end-of-stream flag will
      // be sent.
      serverStream(stream).abortStream(Status.fromThrowable(cause), true);
    } else {
      // Delegate to the base class to send a RST_STREAM.
      super.onStreamError(ctx, cause, http2Ex);
    }
  }

  /**
   * Handler for the Channel shutting down.
   */
  @Override
  public void channelInactive(ChannelHandlerContext ctx) throws Exception {
    super.channelInactive(ctx);
    // Any streams that are still active must be closed
    connection().forEachActiveStream(new Http2StreamVisitor() {
      @Override
      public boolean visit(Http2Stream stream) throws Http2Exception {
        serverStream(stream).abortStream(GOAWAY_STATUS, false);
        return true;
      }
    });
  }

  WriteQueue getWriteQueue() {
    return serverWriteQueue;
  }

  /**
   * Handler for commands sent from the stream.
   */
  @Override
  public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise)
      throws Http2Exception {
    if (msg instanceof SendGrpcFrameCommand) {
      sendGrpcFrame(ctx, (SendGrpcFrameCommand) msg, promise);
    } else if (msg instanceof SendResponseHeadersCommand) {
      sendResponseHeaders(ctx, (SendResponseHeadersCommand) msg, promise);
    } else if (msg instanceof RequestMessagesCommand) {
      ((RequestMessagesCommand) msg).requestMessages();
    } else {
      AssertionError e =
          new AssertionError("Write called for unexpected type: " + msg.getClass().getName());
      ReferenceCountUtil.release(msg);
      promise.setFailure(e);
      throw e;
    }
  }

  /**
   * Returns the given processed bytes back to inbound flow control.
   */
  void returnProcessedBytes(Http2Stream http2Stream, int bytes) {
    try {
      decoder().flowController().consumeBytes(ctx, http2Stream, bytes);
    } catch (Http2Exception e) {
      throw new RuntimeException(e);
    }
  }

  private void closeStreamWhenDone(ChannelPromise promise, int streamId) throws Http2Exception {
    final NettyServerStream stream = serverStream(requireHttp2Stream(streamId));
    promise.addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) {
        stream.complete();
      }
    });
  }

  /**
   * Sends the given gRPC frame to the client.
   */
  private void sendGrpcFrame(ChannelHandlerContext ctx, SendGrpcFrameCommand cmd,
      ChannelPromise promise) throws Http2Exception {
    if (cmd.endStream()) {
      closeStreamWhenDone(promise, cmd.streamId());
    }
    // Call the base class to write the HTTP/2 DATA frame.
    encoder().writeData(ctx, cmd.streamId(), cmd.content(), 0, cmd.endStream(), promise);
  }

  /**
   * Sends the response headers to the client.
   */
  private void sendResponseHeaders(ChannelHandlerContext ctx, SendResponseHeadersCommand cmd,
      ChannelPromise promise) throws Http2Exception {
    if (cmd.endOfStream()) {
      closeStreamWhenDone(promise, cmd.streamId());
    }
    encoder().writeHeaders(ctx, cmd.streamId(), cmd.headers(), 0, cmd.endOfStream(), promise);
  }

  private Http2Stream requireHttp2Stream(int streamId) {
    Http2Stream stream = connection().stream(streamId);
    if (stream == null) {
      // This should never happen.
      throw new AssertionError("Stream does not exist: " + streamId);
    }
    return stream;
  }

  private String determineMethod(int streamId, Http2Headers headers) throws Http2Exception {
    if (!HTTP_METHOD.equals(headers.method())) {
      throw Http2Exception.streamError(streamId, Http2Error.REFUSED_STREAM,
          "Method '%s' is not supported", headers.method());
    }
    checkHeader(streamId, headers, CONTENT_TYPE_HEADER, CONTENT_TYPE_GRPC);
    String methodName = TransportFrameUtil.getFullMethodNameFromPath(headers.path().toString());
    if (methodName == null) {
      throw Http2Exception.streamError(streamId, Http2Error.REFUSED_STREAM,
          "Malformatted path: %s", headers.path());
    }
    return methodName;
  }

  private static void checkHeader(int streamId, Http2Headers headers,
      ByteString header, ByteString expectedValue) throws Http2Exception {
    if (!expectedValue.equals(headers.get(header))) {
      throw Http2Exception.streamError(streamId, Http2Error.REFUSED_STREAM,
          "Header '%s'='%s', while '%s' is expected", header, headers.get(header), expectedValue);
    }
  }

  /**
   * Returns the server stream associated to the given HTTP/2 stream object.
   */
  private NettyServerStream serverStream(Http2Stream stream) {
    return stream.getProperty(streamKey);
  }

  private Http2Exception newStreamException(int streamId, Throwable cause) {
    return Http2Exception.streamError(
        streamId, Http2Error.INTERNAL_ERROR, cause.getMessage(), cause);
  }

  /**
   * Sends initial configuration of this endpoint to the remote endpoint.
   */
  private void sendInitialSettings() throws Http2Exception {
    if (!ctx.channel().isActive()) {
      return;
    }


    boolean needToFlush = false;

    // Send the initial settings for this endpoint.
    if (initialSettings != null) {
      needToFlush = true;
      encoder().writeSettings(ctx, initialSettings, ctx.newPromise());
      initialSettings = null;
    }

    // Send the initial connection window if different than the default.
    if (connectionWindowSize > 0) {
      needToFlush = true;
      Http2Stream connectionStream = connection().connectionStream();
      int currentSize = connection().local().flowController().windowSize(connectionStream);
      int delta = connectionWindowSize - currentSize;
      decoder().flowController().incrementWindowSize(ctx, connectionStream, delta);
      connectionWindowSize = -1;
    }

    if (needToFlush) {
      ctx.flush();
    }
  }

  private static class LazyFrameListener extends Http2FrameAdapter {
    private NettyServerHandler handler;

    void setHandler(NettyServerHandler handler) {
      this.handler = handler;
    }

    @Override
    public int onDataRead(ChannelHandlerContext ctx, int streamId, ByteBuf data, int padding,
        boolean endOfStream) throws Http2Exception {
      handler.onDataRead(streamId, data, endOfStream);
      return padding;
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
