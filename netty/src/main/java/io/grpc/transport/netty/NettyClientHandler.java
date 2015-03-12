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
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionAdapter;
import io.netty.handler.codec.http2.Http2ConnectionHandler;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2FrameAdapter;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2LocalFlowController;
import io.netty.handler.codec.http2.Http2Stream;

import java.util.ArrayDeque;
import java.util.Deque;
import java.util.Iterator;

import javax.annotation.Nullable;

/**
 * Client-side Netty handler for GRPC processing. All event handlers are executed entirely within
 * the context of the Netty Channel thread.
 */
class NettyClientHandler extends Http2ConnectionHandler {

  /**
   * A pending stream creation.
   */
  private final class PendingStream {
    private final Http2Headers headers;
    private final NettyClientStream stream;
    private final ChannelPromise promise;

    public PendingStream(CreateStreamCommand command, ChannelPromise promise) {
      headers = command.headers();
      stream = command.stream();
      this.promise = promise;
    }
  }

  private final Deque<PendingStream> pendingStreams = new ArrayDeque<PendingStream>();
  private final Http2LocalFlowController inboundFlow;
  private Throwable connectionError;
  private Status goAwayStatus;
  private ChannelHandlerContext ctx;

  public NettyClientHandler(Http2Connection connection,
      Http2FrameReader frameReader,
      Http2FrameWriter frameWriter,
      Http2LocalFlowController inboundFlow) {
    super(connection, frameReader, frameWriter, new LazyFrameListener());
    this.inboundFlow = Preconditions.checkNotNull(inboundFlow, "inboundFlow");

    initListener();

    // Disallow stream creation by the server.
    connection.remote().maxStreams(0);
    connection.local().allowPushTo(false);

    // Observe the HTTP/2 connection for events.
    connection.addListener(new Http2ConnectionAdapter() {
      @Override
      public void streamInactive(Http2Stream stream) {
        // Whenever a stream has been closed, try to create a pending stream to fill its place.
        createPendingStreams();
      }

      @Override
      public void goingAway() {
        NettyClientHandler.this.goingAway();
      }
    });
  }

  @Nullable
  public Throwable connectionError() {
    return connectionError;
  }

  @Override
  public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
    this.ctx = ctx;
    super.handlerAdded(ctx);
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
    Status status = HttpUtil.Http2Error.statusForCode((int) errorCode);
    if (debugData.isReadable()) {
      // If a debug message was provided, use it.
      String msg = debugData.toString(UTF_8);
      status = status.augmentDescription(msg);
    }
    goAwayStatus(status);
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
      // Fail any streams that are awaiting creation.
      goAwayStatus(goAwayStatus().augmentDescription("Network channel closed"));
      failPendingStreams(goAwayStatus);

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
  private void createStream(CreateStreamCommand command, ChannelPromise promise) {
    // Add the creation request to the queue.
    pendingStreams.addLast(new PendingStream(command, promise));

    // Process the pending streams queue.
    createPendingStreams();
  }

  /**
   * Cancels this stream.
   */
  private void cancelStream(ChannelHandlerContext ctx, CancelStreamCommand cmd,
      ChannelPromise promise) throws Http2Exception {
    NettyClientStream stream = cmd.stream();
    stream.transportReportStatus(Status.CANCELLED, true, new Metadata.Trailers());

    // No need to set the stream status for a cancellation. It should already have been
    // set prior to sending the command.

    // If the stream hasn't been created yet, remove it from the pending queue.
    if (stream.id() == null) {
      removePendingStream(stream);
      promise.setSuccess();
      return;
    }

    // Send a RST_STREAM frame to terminate this stream. If the stream doesn't exist, assume it is
    // already closed.
    Http2Stream http2Stream = connection().stream(stream.id());
    if (http2Stream != null && http2Stream.state() != Http2Stream.State.CLOSED) {
      // Note: RST_STREAM frames are automatically flushed.
      encoder().writeRstStream(ctx, stream.id(), Http2Error.CANCEL.code(), promise);
    } else {
      // This does allow for a race in the case of two consecutive cancels where the RST_STREAM
      // from the first hasn't completed when we setSuccess here for the second. But we don't care.
      promise.setSuccess();
    }
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
    // Fail any streams that are awaiting creation.
    Status goAwayStatus = goAwayStatus();
    failPendingStreams(goAwayStatus);

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
   * Processes the pending stream creation requests. This considers several conditions:
   *
   * <p>
   * 1) The HTTP/2 connection has exhausted its stream IDs. In this case all pending streams are
   * immediately failed.
   * <p>
   * 2) The HTTP/2 connection is going away. In this case all pending streams are immediately
   * failed.
   * <p>
   * 3) The HTTP/2 connection's MAX_CONCURRENT_STREAMS limit has been reached. In this case,
   * processing of pending streams stops until an active stream has been closed.
   */
  private void createPendingStreams() {
    Http2Connection connection = connection();
    Http2Connection.Endpoint<Http2LocalFlowController> local = connection.local();
    Status goAwayStatus = goAwayStatus();
    while (!pendingStreams.isEmpty()) {
      final int streamId = local.nextStreamId();
      if (streamId <= 0) {
        // The HTTP/2 connection has exhausted its stream IDs. Permanently fail all stream creation
        // attempts for this transport.
        // TODO(nmittler): send GO_AWAY?
        failPendingStreams(goAwayStatus);
        return;
      }

      if (connection.isGoAway()) {
        failPendingStreams(goAwayStatus);
        return;
      }

      if (!local.acceptingNewStreams()) {
        // We're bumping up against the MAX_CONCURRENT_STEAMS threshold for this endpoint. Need to
        // wait until the endpoint is accepting new streams.
        return;
      }

      // Finish creation of the stream by writing a headers frame.
      final PendingStream pendingStream = pendingStreams.remove();
      encoder().writeHeaders(ctx, streamId, pendingStream.headers, 0, false, ctx.newPromise())
          .addListener(new ChannelFutureListener() {
            @Override
            public void operationComplete(ChannelFuture future) throws Exception {
              if (future.isSuccess()) {
                streamCreated(pendingStream.stream, streamId, pendingStream.promise);
              } else {
                // Fail the creation request.
                pendingStream.promise.setFailure(future.cause());
              }
            }
          });
      ctx.flush();
    }
  }

  /**
   * Returns the appropriate status used to represent the cause for GOAWAY.
   */
  private Status goAwayStatus() {
    if (goAwayStatus != null) {
      return goAwayStatus;
    }
    return Status.UNAVAILABLE;
  }

  private void goAwayStatus(Status status) {
    goAwayStatus = goAwayStatus == null ? status : goAwayStatus;
  }

  /**
   * Handles the successful creation of a new stream.
   */
  private void streamCreated(NettyClientStream stream, int streamId, ChannelPromise promise)
      throws Http2Exception {
    // Attach the client stream to the HTTP/2 stream object as user data.
    Http2Stream http2Stream = connection().requireStream(streamId);
    http2Stream.setProperty(NettyClientStream.class, stream);

    // Notify the stream that it has been created.
    stream.id(streamId);
    promise.setSuccess();
  }

  /**
   * Gets the client stream associated to the given HTTP/2 stream object.
   */
  private NettyClientStream clientStream(Http2Stream stream) {
    return stream.getProperty(NettyClientStream.class);
  }

  /**
   * Fails all pending streams with the given status and clears the queue.
   */
  private void failPendingStreams(Status status) {
    while (!pendingStreams.isEmpty()) {
      PendingStream pending = pendingStreams.remove();
      pending.promise.setFailure(status.asException());
    }
  }

  /**
   * Removes the given stream from the pending queue
   *
   * @param stream the stream to be removed.
   */
  private void removePendingStream(NettyClientStream stream) {
    for (Iterator<PendingStream> iter = pendingStreams.iterator(); iter.hasNext();) {
      PendingStream pending = iter.next();
      if (pending.stream == stream) {
        iter.remove();
        return;
      }
    }
  }

  /**
   * Gets a copy of the streams currently in the connection.
   */
  private Http2Stream[] http2Streams() {
    return connection().activeStreams().toArray(new Http2Stream[0]);
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
