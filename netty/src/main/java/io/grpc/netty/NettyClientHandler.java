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

package io.grpc.netty;

import static io.netty.handler.codec.http2.Http2CodecUtil.getEmbeddedHttp2Exception;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Ticker;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.internal.ClientTransport.PingCallback;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.Http2Ping;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionDecoder;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionAdapter;
import io.netty.handler.codec.http2.Http2ConnectionHandler;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2FrameAdapter;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Settings;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.handler.codec.http2.Http2StreamVisitor;

import java.util.Random;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;

/**
 * Client-side Netty handler for GRPC processing. All event handlers are executed entirely within
 * the context of the Netty Channel thread.
 */
class NettyClientHandler extends Http2ConnectionHandler {
  private static final Logger logger = Logger.getLogger(NettyClientHandler.class.getName());

  /**
   * A message that simply passes through the channel without any real processing. It is useful to
   * check if buffers have been drained and test the health of the channel in a single operation.
   */
  static final Object NOOP_MESSAGE = new Object();

  /**
   * Status used when the transport has exhausted the number of streams.
   */
  private static final Status EXHAUSTED_STREAMS_STATUS =
          Status.UNAVAILABLE.withDescription("Stream IDs have been exhausted");

  private final Http2Connection.PropertyKey streamKey;
  private final Ticker ticker;
  private final Random random = new Random();
  private WriteQueue clientWriteQueue;
  private int flowControlWindow;
  private Http2Settings initialSettings = new Http2Settings();
  private Http2Ping ping;
  private Status goAwayStatus;
  private ChannelHandlerContext ctx;
  private int nextStreamId;

  public NettyClientHandler(BufferingHttp2ConnectionEncoder encoder, Http2Connection connection,
                            Http2FrameReader frameReader,
                            int flowControlWindow) {
    this(encoder, connection, frameReader, flowControlWindow,
         Ticker.systemTicker());
  }

  @VisibleForTesting
  NettyClientHandler(BufferingHttp2ConnectionEncoder encoder, Http2Connection connection,
      Http2FrameReader frameReader, int flowControlWindow, Ticker ticker) {
    super(new DefaultHttp2ConnectionDecoder(connection, encoder, frameReader,
        new LazyFrameListener()), encoder);
    this.ticker = ticker;
    Preconditions.checkArgument(flowControlWindow > 0, "flowControlWindow must be positive");
    this.flowControlWindow = flowControlWindow;

    initListener();

    streamKey = connection.newKey();

    nextStreamId = connection.local().nextStreamId();
    connection.addListener(new Http2ConnectionAdapter() {
      @Override
      public void onGoAwayReceived(int lastStreamId, long errorCode, ByteBuf debugData) {
        goAwayStatus(statusFromGoAway(errorCode, debugData));
        goingAway();
      }
    });

    // TODO(nmittler): this is a temporary hack as we currently have to send a 2nd SETTINGS
    // frame. Once we upgrade to Netty 4.1.Beta6 we'll be able to pass in the initial SETTINGS
    // to the super class constructor.
    initialSettings.pushEnabled(false);
    initialSettings.initialWindowSize(flowControlWindow);
    initialSettings.maxConcurrentStreams(0);
  }

  @Nullable
  public Status errorStatus() {
    return goAwayStatus;
  }

  @Override
  public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
    this.ctx = ctx;
    // Sends the connection preface if we haven't already.
    super.handlerAdded(ctx);
    sendInitialSettings();
  }

  @Override
  public void channelActive(ChannelHandlerContext ctx) throws Exception {
    // Sends connection preface if we haven't already.
    super.channelActive(ctx);
    sendInitialSettings();
  }

  /**
   * Handler for commands sent from the stream.
   */
  @Override
  public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise)
          throws Exception {
    if (msg instanceof CreateStreamCommand) {
      createStream((CreateStreamCommand) msg, promise);
    } else if (msg instanceof SendGrpcFrameCommand) {
      sendGrpcFrame(ctx, (SendGrpcFrameCommand) msg, promise);
    } else if (msg instanceof CancelClientStreamCommand) {
      cancelStream(ctx, (CancelClientStreamCommand) msg, promise);
    } else if (msg instanceof RequestMessagesCommand) {
      ((RequestMessagesCommand) msg).requestMessages();
    } else if (msg instanceof SendPingCommand) {
      sendPingFrame(ctx, (SendPingCommand) msg, promise);
    } else if (msg == NOOP_MESSAGE) {
      ctx.write(Unpooled.EMPTY_BUFFER, promise);
    } else {
      throw new AssertionError("Write called for unexpected type: " + msg.getClass().getName());
    }
  }

  void startWriteQueue(Channel channel) {
    clientWriteQueue = new WriteQueue(channel);
  }

  WriteQueue getWriteQueue() {
    return clientWriteQueue;
  }

  /**
   * Returns the given processed bytes back to inbound flow control.
   */
  void returnProcessedBytes(Http2Stream stream, int bytes) {
    try {
      decoder().flowController().consumeBytes(stream, bytes);
    } catch (Http2Exception e) {
      throw new RuntimeException(e);
    }
  }

  private void initListener() {
    ((LazyFrameListener) decoder().listener()).setHandler(this);
  }

  private void onHeadersRead(int streamId, Http2Headers headers, boolean endStream)
      throws Http2Exception {
    NettyClientStream stream = clientStream(requireHttp2Stream(streamId));
    stream.transportHeadersReceived(headers, endStream);
  }

  /**
   * Handler for an inbound HTTP/2 DATA frame.
   */
  private void onDataRead(int streamId, ByteBuf data, boolean endOfStream) throws Http2Exception {
    NettyClientStream stream = clientStream(requireHttp2Stream(streamId));
    stream.transportDataReceived(data, endOfStream);
  }

  /**
   * Handler for an inbound HTTP/2 RST_STREAM frame, terminating a stream.
   */
  private void onRstStreamRead(int streamId, long errorCode) throws Http2Exception {
    NettyClientStream stream = clientStream(requireHttp2Stream(streamId));
    Status status = GrpcUtil.Http2Error.statusForCode((int) errorCode);
    stream.transportReportStatus(status, false /*stop delivery*/, new Metadata());
  }

  @Override
  public void close(ChannelHandlerContext ctx, ChannelPromise promise) throws Exception {
    logger.fine("Network channel being closed by the application.");
    super.close(ctx, promise);
  }

  /**
   * Handler for the Channel shutting down.
   */
  @Override
  public void channelInactive(ChannelHandlerContext ctx) throws Exception {
    try {
      logger.fine("Network channel is closed");
      goAwayStatus(goAwayStatus().augmentDescription("Network channel closed"));
      cancelPing();
      // Report status to the application layer for any open streams
      connection().forEachActiveStream(new Http2StreamVisitor() {
        @Override
        public boolean visit(Http2Stream stream) throws Http2Exception {
          clientStream(stream).transportReportStatus(goAwayStatus, false, new Metadata());
          return true;
        }
      });
    } finally {
      // Close any open streams
      super.channelInactive(ctx);
    }
  }

  @Override
  public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
    if (getEmbeddedHttp2Exception(cause) == null) {
      // Kill the connection instead of propagating the exceptionCaught(). Http2ConnectionHandler
      // only handles Http2Exceptions and propagates everything else.
      goAwayStatus(Status.fromThrowable(cause));
      cause = new Http2Exception(Http2Error.INTERNAL_ERROR, null, cause);
    }
    super.exceptionCaught(ctx, cause);
  }

  @Override
  protected void onConnectionError(ChannelHandlerContext ctx, Throwable cause,
      Http2Exception http2Ex) {
    logger.log(Level.FINE, "Caught a connection error", cause);
    goAwayStatus(statusFromError(cause));
    super.onConnectionError(ctx, cause, http2Ex);
  }

  @Override
  protected void onStreamError(ChannelHandlerContext ctx, Throwable cause,
      Http2Exception.StreamException http2Ex) {
    // Close the stream with a status that contains the cause.
    Http2Stream stream = connection().stream(http2Ex.streamId());
    if (stream != null) {
      clientStream(stream).transportReportStatus(statusFromError(cause), false, new Metadata());
    }

    // Delegate to the base class to send a RST_STREAM.
    super.onStreamError(ctx, cause, http2Ex);
  }

  private Status statusFromError(Throwable cause) {
    return cause instanceof Http2Exception ? Status.INTERNAL.withCause(cause)
        : Status.fromThrowable(cause);
  }

  @Override
  protected boolean isGracefulShutdownComplete() {
    // Only allow graceful shutdown to complete after all pending streams have completed.
    return super.isGracefulShutdownComplete()
        && ((BufferingHttp2ConnectionEncoder) encoder()).numBufferedStreams() == 0;
  }

  /**
   * Attempts to create a new stream from the given command. If there are too many active streams,
   * the creation request is queued.
   */
  private void createStream(CreateStreamCommand command, final ChannelPromise promise)
          throws Exception {
    // Get the stream ID for the new stream.
    final int streamId;
    try {
      streamId = getAndIncrementNextStreamId();
    } catch (StatusException e) {
      // Stream IDs have been exhausted for this connection. Fail the promise immediately.
      promise.setFailure(e);

      // Initiate a graceful shutdown if we haven't already.
      if (!connection().goAwaySent()) {
        logger.fine("Stream IDs have been exhausted for this connection. "
                + "Initiating graceful shutdown of the connection.");
        super.close(ctx, ctx.newPromise());
      }
      return;
    }

    final NettyClientStream stream = command.stream();
    final Http2Headers headers = command.headers();
    stream.id(streamId);
    encoder().writeHeaders(ctx, streamId, headers, 0, false, promise)
            .addListener(new ChannelFutureListener() {
              @Override
              public void operationComplete(ChannelFuture future) throws Exception {
                if (future.isSuccess()) {
                  // The http2Stream will be null in case a stream buffered in the encoder
                  // was canceled via RST_STREAM.
                  Http2Stream http2Stream = connection().stream(streamId);
                  if (http2Stream != null) {
                    http2Stream.setProperty(streamKey, stream);
                  }
                  // Attach the client stream to the HTTP/2 stream object as user data.
                  stream.setHttp2Stream(http2Stream);
                } else {
                  if (future.cause() instanceof GoAwayClosedStreamException) {
                    GoAwayClosedStreamException e = (GoAwayClosedStreamException) future.cause();
                    goAwayStatus(statusFromGoAway(e.errorCode(), e.debugData()));
                    stream.transportReportStatus(goAwayStatus, false, new Metadata());
                  } else {
                    stream.transportReportStatus(Status.fromThrowable(future.cause()), true,
                        new Metadata());
                  }
                }
              }
            });
  }

  /**
   * Cancels this stream.
   */
  private void cancelStream(ChannelHandlerContext ctx, CancelClientStreamCommand cmd,
      ChannelPromise promise) {
    NettyClientStream stream = cmd.stream();
    stream.transportReportStatus(cmd.reason(), true, new Metadata());
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
   * Sends a PING frame. If a ping operation is already outstanding, the callback in the message is
   * registered to be called when the existing operation completes, and no new frame is sent.
   */
  private void sendPingFrame(ChannelHandlerContext ctx, SendPingCommand msg,
      ChannelPromise promise) {
    PingCallback callback = msg.callback();
    Executor executor = msg.executor();
    if (!ctx.channel().isOpen()) {
      Http2Ping.notifyFailed(callback, executor, goAwayStatus().asException());
      return;
    }

    // we only allow one outstanding ping at a time, so just add the callback to
    // any outstanding operation
    if (ping != null) {
      ping.addCallback(callback, executor);
      return;
    }

    // set outstanding operation
    long data = random.nextLong();
    ByteBuf buffer = ctx.alloc().buffer(8);
    buffer.writeLong(data);
    Stopwatch stopwatch = Stopwatch.createStarted(ticker);
    ping = new Http2Ping(data, stopwatch);
    ping.addCallback(callback, executor);
    // and then write the ping
    encoder().writePing(ctx, false, buffer, promise);
    ctx.flush();
    final Http2Ping finalPing = ping;
    promise.addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (!future.isSuccess()) {
          finalPing.failed(future.cause());
          if (ping == finalPing) {
            ping = null;
          }
        }
      }
    });
  }

  /**
   * Handler for a GOAWAY being either sent or received. Fails any streams created after the
   * last known stream.
   */
  private void goingAway() {
    final Status goAwayStatus = goAwayStatus();
    final int lastKnownStream = connection().local().lastStreamKnownByPeer();
    try {
      connection().forEachActiveStream(new Http2StreamVisitor() {
        @Override
        public boolean visit(Http2Stream stream) throws Http2Exception {
          if (stream.id() > lastKnownStream) {
            clientStream(stream)
                .transportReportStatus(goAwayStatus, false, new Metadata());
            stream.close();
          }
          return true;
        }
      });
    } catch (Http2Exception e) {
      throw new RuntimeException(e);
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

  private void cancelPing() {
    if (ping != null) {
      ping.failed(goAwayStatus().asException());
      ping = null;
    }
  }

  private Status statusFromGoAway(long errorCode, ByteBuf debugData) {
    Status status = GrpcUtil.Http2Error.statusForCode((int) errorCode);
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
    return stream.getProperty(streamKey);
  }

  private int getAndIncrementNextStreamId() throws StatusException {
    if (nextStreamId < 0) {
      logger.fine("Stream IDs have been exhausted for this connection. "
              + "Initiating graceful shutdown of the connection.");
      throw EXHAUSTED_STREAMS_STATUS.asException();
    }

    int id = nextStreamId;
    nextStreamId += 2;
    return id;
  }

  private Http2Stream requireHttp2Stream(int streamId) {
    Http2Stream stream = connection().stream(streamId);
    if (stream == null) {
      // This should never happen.
      throw new AssertionError("Stream does not exist: " + streamId);
    }
    return stream;
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
    if (flowControlWindow > 0) {
      needToFlush = true;
      Http2Stream connectionStream = connection().connectionStream();
      int currentSize = connection().local().flowController().windowSize(connectionStream);
      int delta = flowControlWindow - currentSize;
      decoder().flowController().incrementWindowSize(connectionStream, delta);
      flowControlWindow = -1;
    }

    if (needToFlush) {
      ctx.flush();
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
      handler.onRstStreamRead(streamId, errorCode);
    }

    @Override public void onPingAckRead(ChannelHandlerContext ctx, ByteBuf data)
        throws Http2Exception {
      Http2Ping p = handler.ping;
      if (p != null) {
        long ackPayload = data.readLong();
        if (p.payload() == ackPayload) {
          p.complete();
          handler.ping = null;
        } else {
          logger.log(Level.WARNING, String.format("Received unexpected ping ack. "
              + "Expecting %d, got %d", p.payload(), ackPayload));
        }
      } else {
        logger.warning("Received unexpected ping ack. No ping outstanding");
      }
    }
  }
}
