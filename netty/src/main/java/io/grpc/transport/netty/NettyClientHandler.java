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

import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.base.Preconditions;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.transport.HttpUtil;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionDecoder;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionEncoder;
import io.netty.handler.codec.http2.Http2ConnectionHandler;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2FrameAdapter;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2LocalFlowController;
import io.netty.handler.codec.http2.Http2Stream;

import javax.annotation.Nullable;

/**
 * Client-side Netty handler for GRPC processing. All event handlers are executed entirely within
 * the context of the Netty Channel thread.
 */
class NettyClientHandler extends Http2ConnectionHandler {

  private final Http2LocalFlowController inboundFlow;
  private int connectionWindowSize;
  private Throwable connectionError;
  private Status goAwayStatus;
  private ChannelHandlerContext ctx;
  private int nextStreamId;

  public NettyClientHandler(Http2ConnectionEncoder encoder, Http2Connection connection,
                            Http2FrameReader frameReader, Http2LocalFlowController inboundFlow,
                            int connectionWindowSize) {
    super(new DefaultHttp2ConnectionDecoder(connection, encoder, frameReader,
        new LazyFrameListener()), new BufferingHttp2ConnectionEncoder(encoder));
    Preconditions.checkArgument(connectionWindowSize > 0, "connectionWindowSize must be positive");
    this.connectionWindowSize =
        connectionWindowSize == inboundFlow.initialWindowSize() ? -1 : connectionWindowSize;
    this.inboundFlow = Preconditions.checkNotNull(inboundFlow, "inboundFlow");

    initListener();

    // Disallow stream creation by the server.
    connection.remote().maxActiveStreams(0);
    connection.local().allowPushTo(false);
    nextStreamId = connection.local().nextStreamId();
  }

  @Nullable
  public Throwable connectionError() {
    return connectionError;
  }

  @Override
  public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
    this.ctx = ctx;

    // Sends the connection preface if we haven't already.
    super.handlerAdded(ctx);

    // Initialize the connection window if we haven't already.
    initConnectionWindow();
  }

  @Override
  public void channelActive(ChannelHandlerContext ctx) throws Exception {
    // Sends connection preface if we haven't already.
    super.channelActive(ctx);

    // Initialize the connection window if we haven't already.
    initConnectionWindow();
  }

  /**
   * Handler for commands sent from the stream.
   */
  @Override
  public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise) {
    try {
      if (msg instanceof CreateStreamCommand) {
        createStream((CreateStreamCommand) msg, promise);
      } else if (msg instanceof SendGrpcFrameCommand) {
        sendGrpcFrame(ctx, (SendGrpcFrameCommand) msg, promise);
      } else if (msg instanceof CancelStreamCommand) {
        cancelStream(ctx, (CancelStreamCommand) msg, promise);
      } else {
        throw new AssertionError("Write called for unexpected type: " + msg.getClass().getName());
      }
    } catch (Throwable t) {
      promise.setFailure(t);
    }
  }

  /**
   * Returns the given processed bytes back to inbound flow control.
   */
  void returnProcessedBytes(int streamId, int bytes) {
    try {
      Http2Stream http2Stream = connection().requireStream(streamId);
      inboundFlow.consumeBytes(ctx, http2Stream, bytes);
    } catch (Http2Exception e) {
      throw new RuntimeException(e);
    }
  }

  private void initListener() {
    ((LazyFrameListener) decoder().listener()).setHandler(this);
  }

  private void onHeadersRead(int streamId, Http2Headers headers, boolean endStream)
      throws Http2Exception {
    NettyClientStream stream = clientStream(connection().requireStream(streamId));
    stream.transportHeadersReceived(headers, endStream);
  }

  /**
   * Handler for an inbound HTTP/2 DATA frame.
   */
  private void onDataRead(int streamId, ByteBuf data, boolean endOfStream) throws Http2Exception {
    Http2Stream http2Stream = connection().requireStream(streamId);
    NettyClientStream stream = clientStream(http2Stream);
    stream.transportDataReceived(data, endOfStream);
  }

  /**
   * Handler for an inbound HTTP/2 RST_STREAM frame, terminating a stream.
   */
  private void onRstStreamRead(int streamId)
      throws Http2Exception {
    // TODO(nmittler): do something with errorCode?
    Http2Stream http2Stream = connection().requireStream(streamId);
    NettyClientStream stream = clientStream(http2Stream);
    stream.transportReportStatus(Status.UNKNOWN, false, new Metadata.Trailers());
  }

  private void onGoAwayRead(long errorCode, ByteBuf debugData) {
    goAwayStatus(statusFromGoAway(errorCode, debugData));
    goingAway();
  }

  @Override
  public void close(ChannelHandlerContext ctx, ChannelPromise promise) throws Exception {
    goAwayStatus(Status.UNAVAILABLE.withDescription("Network channel closed by the client"));
    
    super.close(ctx, promise);
  }

  /**
   * Handler for the Channel shutting down.
   */
  @Override
  public void channelInactive(ChannelHandlerContext ctx) throws Exception {
    try {
      goAwayStatus(goAwayStatus().augmentDescription("Network channel closed"));
      // Report status to the application layer for any open streams
      for (Http2Stream stream : http2Streams()) {
        clientStream(stream).transportReportStatus(goAwayStatus, false, new Metadata.Trailers());
      }
    } finally {
      // Close any open streams
      super.channelInactive(ctx);
    }
  }

  @Override
  protected void onConnectionError(ChannelHandlerContext ctx, Throwable cause,
      Http2Exception http2Ex) {
    // Save the error.
    connectionError = cause;
    goAwayStatus(Status.fromThrowable(connectionError));

    super.onConnectionError(ctx, cause, http2Ex);
  }

  @Override
  protected void onStreamError(ChannelHandlerContext ctx, Throwable cause,
      Http2Exception.StreamException http2Ex) {
    // Close the stream with a status that contains the cause.
    Http2Stream stream = connection().stream(http2Ex.streamId());
    if (stream != null) {
      clientStream(stream).transportReportStatus(Status.fromThrowable(cause), false,
          new Metadata.Trailers());
    }

    // Delegate to the base class to send a RST_STREAM.
    super.onStreamError(ctx, cause, http2Ex);
  }

  /**
   * Attempts to create a new stream from the given command. If there are too many active streams,
   * the creation request is queued.
   */
  private void createStream(CreateStreamCommand command, final ChannelPromise promise) {
    final Http2Connection.Endpoint<Http2LocalFlowController> local = connection().local();
    final int streamId = getAndIncrementNextStreamId();
    final NettyClientStream stream = command.stream();
    final Http2Headers headers = command.headers();
    // TODO: Send GO_AWAY if streamId overflows
    stream.id(streamId);
    encoder().writeHeaders(ctx, streamId, headers, 0, false, promise)
            .addListener(new ChannelFutureListener() {
              @Override
              public void operationComplete(ChannelFuture future) throws Exception {
                if (future.isSuccess()) {
                  // Attach the client stream to the HTTP/2 stream object as user data.
                  Http2Stream http2Stream = connection().stream(streamId);
                  // The http2Stream will be null in case a stream buffered in the encoder
                  // was canceled via RST_STREAM.
                  if (http2Stream != null) {
                    http2Stream.setProperty(NettyClientStream.class, stream);
                  }
                } else {
                  if (future.cause() instanceof GoAwayClosedStreamException) {
                    GoAwayClosedStreamException e = (GoAwayClosedStreamException) future.cause();
                    goAwayStatus(statusFromGoAway(e.errorCode(), e.debugData()));
                    stream.transportReportStatus(goAwayStatus, false, new Metadata.Trailers());
                  } else {
                    stream.transportReportStatus(Status.fromThrowable(future.cause()), true,
                        new Metadata.Trailers());
                  }
                }
              }
            });
  }

  /**
   * Cancels this stream.
   */
  private void cancelStream(ChannelHandlerContext ctx, CancelStreamCommand cmd,
      ChannelPromise promise) throws Http2Exception {
    NettyClientStream stream = cmd.stream();
    stream.transportReportStatus(Status.CANCELLED, true, new Metadata.Trailers());
    encoder().writeRstStream(ctx, stream.id(), Http2Error.CANCEL.code(), promise);
  }

  /**
   * Sends the given GRPC frame for the stream.
   */
  private void sendGrpcFrame(ChannelHandlerContext ctx, SendGrpcFrameCommand cmd,
      ChannelPromise promise) {
    // Call the base class to write the HTTP/2 DATA frame.
    // Note: no need to flush since this is handled by the outbound flow controller.
    encoder().writeData(ctx, cmd.streamId(), cmd.content(), 0, cmd.endStream(), promise);
  }

  /**
   * Handler for a GOAWAY being either sent or received.
   */
  private void goingAway() {
    Status goAwayStatus = goAwayStatus();
    if (connection().goAwayReceived()) {
      // Received a GOAWAY from the remote endpoint. Fail any streams that were created after the
      // last known stream.
      int lastKnownStream = connection().local().lastKnownStream();
      for (Http2Stream stream : http2Streams()) {
        if (lastKnownStream < stream.id()) {
          clientStream(stream).transportReportStatus(goAwayStatus, false, new Metadata.Trailers());
          stream.close();
        }
      }
    }
  }

  /**
   * Returns the appropriate status used to represent the cause for GOAWAY.
   */
  private Status goAwayStatus() {
    if (goAwayStatus != null) {
      return goAwayStatus;
    }
    return Status.UNAVAILABLE.withDescription("Connection going away, but for unknown reason");
  }

  private void goAwayStatus(Status status) {
    goAwayStatus = goAwayStatus == null ? status : goAwayStatus;
  }

  private Status statusFromGoAway(long errorCode, ByteBuf debugData) {
    Status status = HttpUtil.Http2Error.statusForCode((int) errorCode);
    if (debugData.isReadable()) {
      // If a debug message was provided, use it.
      String msg = debugData.toString(UTF_8);
      status = status.augmentDescription(msg);
    }
    return status;
  }

  /**
   * Gets the client stream associated to the given HTTP/2 stream object.
   */
  private NettyClientStream clientStream(Http2Stream stream) {
    return stream.getProperty(NettyClientStream.class);
  }

  /**
   * Gets a copy of the streams currently in the connection.
   */
  private Http2Stream[] http2Streams() {
    return connection().activeStreams().toArray(new Http2Stream[0]);
  }

  private int getAndIncrementNextStreamId() {
    int id = nextStreamId;
    nextStreamId += 2;
    return id;
  }

  /**
   * Initializes the connection window if we haven't already.
   */
  private void initConnectionWindow() throws Http2Exception {
    if (connectionWindowSize > 0 && ctx.channel().isActive()) {
      Http2Stream stream = connection().connectionStream();
      int currentSize = decoder().flowController().windowSize(stream);
      int delta = connectionWindowSize - currentSize;
      decoder().flowController().incrementWindowSize(ctx, stream, delta);
      connectionWindowSize = -1;
    }
  }

  private static class LazyFrameListener extends Http2FrameAdapter {
    private NettyClientHandler handler;

    void setHandler(NettyClientHandler handler) {
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
      handler.onHeadersRead(streamId, headers, endStream);
    }

    @Override
    public void onRstStreamRead(ChannelHandlerContext ctx, int streamId, long errorCode)
        throws Http2Exception {
      handler.onRstStreamRead(streamId);
    }

    @Override
    public void onGoAwayRead(ChannelHandlerContext ctx, int lastStreamId, long errorCode,
        ByteBuf debugData) throws Http2Exception {
      handler.onGoAwayRead(errorCode, debugData);
    }
  }
}
