package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.netty.NettyClientStream.PENDING_STREAM_ID;

import com.google.common.base.Preconditions;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.StreamState;

import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionDecoder;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.Http2CodecUtil;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionAdapter;
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

  private final DefaultHttp2InboundFlowController inboundFlow;
  private final Deque<PendingStream> pendingStreams = new ArrayDeque<PendingStream>();
  private Throwable connectionError;
  private ChannelHandlerContext ctx;

  public NettyClientHandler(Http2Connection connection,
      Http2FrameReader frameReader,
      Http2FrameWriter frameWriter,
      DefaultHttp2InboundFlowController inboundFlow,
      Http2OutboundFlowController outboundFlow) {
    super(connection, frameReader, frameWriter, inboundFlow, outboundFlow, new LazyFrameListener());
    initListener();
    this.inboundFlow = Preconditions.checkNotNull(inboundFlow, "inboundFlow");

    // Disallow stream creation by the server.
    connection.remote().maxStreams(0);
    connection.local().allowPushTo(false);

    // Observe the HTTP/2 connection for events.
    connection.addListener(new Http2ConnectionAdapter() {
      @Override
      public void streamHalfClosed(Http2Stream stream) {
        // Check for disallowed state: HALF_CLOSED_REMOTE.
        terminateIfInvalidState(stream);
      }

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

  public DefaultHttp2InboundFlowController inboundFlow() {
    return inboundFlow;
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

  private void initListener() {
    ((LazyFrameListener) ((DefaultHttp2ConnectionDecoder) this.decoder()).listener()).setHandler(
        this);
  }

  private void onHeadersRead(int streamId, Http2Headers headers, boolean endStream)
      throws Http2Exception {
    NettyClientStream stream = clientStream(connection().requireStream(streamId));
    stream.inboundHeadersRecieved(headers, endStream);
  }

  /**
   * Handler for an inbound HTTP/2 DATA frame.
   */
  private void onDataRead(ChannelHandlerContext ctx, int streamId, ByteBuf data,
      boolean endOfStream) throws Http2Exception {
    Http2Stream http2Stream = connection().requireStream(streamId);
    NettyClientStream stream = clientStream(http2Stream);
    stream.inboundDataReceived(data, endOfStream);
    if (stream.state() == StreamState.CLOSED && !endOfStream) {
      // TODO(user): This is a hack due to the test server not consistently
      // setting endOfStream on the last frame for the v1 protocol.
      // Remove this once b/17692766 is fixed.
      lifecycleManager().closeRemoteSide(http2Stream, ctx.newSucceededFuture());
    }
  }

  /**
   * Handler for an inbound HTTP/2 RST_STREAM frame, terminating a stream.
   */
  private void onRstStreamRead(int streamId)
      throws Http2Exception {
    // TODO(user): do something with errorCode?
    Http2Stream http2Stream = connection().requireStream(streamId);
    NettyClientStream stream = clientStream(http2Stream);
    stream.setStatus(Status.UNKNOWN, new Metadata.Trailers());
  }

  /**
   * Handler for the Channel shutting down.
   */
  @Override
  public void channelInactive(ChannelHandlerContext ctx) throws Exception {
    super.channelInactive(ctx);

    // Fail any streams that are awaiting creation.
    Status goAwayStatus = goAwayStatus();
    failPendingStreams(goAwayStatus);

    // Any streams that are still active must be closed.
    for (Http2Stream stream : http2Streams()) {
      clientStream(stream).setStatus(goAwayStatus, new Metadata.Trailers());
    }
  }

  @Override
  public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
    // Force the conversion of any exceptions into HTTP/2 exceptions.
    Http2Exception e = Http2CodecUtil.toHttp2Exception(cause);
    if (e instanceof Http2StreamException) {
      // Close the stream with a status that contains the cause.
      Http2Stream stream = connection().stream(((Http2StreamException) e).streamId());
      if (stream != null) {
        clientStream(stream).setStatus(Status.fromThrowable(cause), new Metadata.Trailers());
      }
    } else {
      connectionError = e;
    }

    // Delegate to the super class for proper handling of the Http2Exception.
    super.exceptionCaught(ctx, e);
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
    stream.setStatus(Status.CANCELLED, new Metadata.Trailers());

    // No need to set the stream status for a cancellation. It should already have been
    // set prior to sending the command.

    // If the stream hasn't been created yet, remove it from the pending queue.
    if (stream.id() == PENDING_STREAM_ID) {
      removePendingStream(stream);
      promise.setSuccess();
      return;
    }

    // Send a RST_STREAM frame to terminate this stream.
    Http2Stream http2Stream = connection().requireStream(stream.id());
    if (http2Stream.state() != Http2Stream.State.CLOSED) {
      // Note: RST_STREAM frames are automatically flushed.
      encoder().writeRstStream(ctx, stream.id(), Http2Error.CANCEL.code(), promise);
    }
  }

  /**
   * Sends the given GRPC frame for the stream.
   */
  private void sendGrpcFrame(ChannelHandlerContext ctx, SendGrpcFrameCommand cmd,
      ChannelPromise promise) throws Http2Exception {
    Http2Stream http2Stream = connection().requireStream(cmd.streamId());
    switch (http2Stream.state()) {
      case CLOSED:
      case HALF_CLOSED_LOCAL:
      case IDLE:
      case RESERVED_LOCAL:
      case RESERVED_REMOTE:
        cmd.release();
        promise.setFailure(new Exception("Closed before write could occur"));
        return;
      default:
        break;
    }

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

    if (connection().local().isGoAwayReceived()) {
      // Received a GOAWAY from the remote endpoint. Fail any streams that were created after the
      // last known stream.
      int lastKnownStream = connection().local().lastKnownStream();
      for (Http2Stream stream : http2Streams()) {
        if (lastKnownStream < stream.id()) {
          clientStream(stream).setStatus(goAwayStatus, new Metadata.Trailers());
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
    Http2Connection.Endpoint local = connection.local();
    Status goAwayStatus = goAwayStatus();
    while (!pendingStreams.isEmpty()) {
      final int streamId = local.nextStreamId();
      if (streamId <= 0) {
        // The HTTP/2 connection has exhausted its stream IDs. Permanently fail all stream creation
        // attempts for this transport.
        // TODO(user): send GO_AWAY?
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
    if (connectionError != null) {
      return Status.fromThrowable(connectionError);
    }
    return Status.UNAVAILABLE;
  }

  /**
   * Handles the successful creation of a new stream.
   */
  private void streamCreated(NettyClientStream stream, int streamId, ChannelPromise promise)
      throws Http2Exception {
    // Attach the client stream to the HTTP/2 stream object as user data.
    Http2Stream http2Stream = connection().requireStream(streamId);
    http2Stream.data(stream);

    // Notify the stream that it has been created.
    stream.id(streamId);
    promise.setSuccess();
  }

  /**
   * Gets the client stream associated to the given HTTP/2 stream object.
   */
  private NettyClientStream clientStream(Http2Stream stream) {
    return stream.<NettyClientStream>data();
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

  /**
   * Terminates the stream if it's in an unsupported state.
   */
  private void terminateIfInvalidState(Http2Stream stream) {
    switch (stream.state()) {
      case HALF_CLOSED_REMOTE:
      case IDLE:
      case RESERVED_LOCAL:
      case RESERVED_REMOTE:
        // Disallowed state, terminate the stream.
        clientStream(stream).setStatus(
            Status.INTERNAL.withDescription("Stream in invalid state: " + stream.state()),
            new Metadata.Trailers());
        encoder().writeRstStream(ctx, stream.id(), Http2Error.INTERNAL_ERROR.code(),
            ctx.newPromise());
        break;
      default:
        break;
    }
  }

  private static class LazyFrameListener extends Http2FrameAdapter {
    private NettyClientHandler handler;

    void setHandler(NettyClientHandler handler) {
      this.handler = handler;
    }

    @Override
    public void onDataRead(ChannelHandlerContext ctx, int streamId, ByteBuf data, int padding,
        boolean endOfStream) throws Http2Exception {
      handler.onDataRead(ctx, streamId, data, endOfStream);
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
  }
}
