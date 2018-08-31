/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.netty;

import static io.netty.handler.codec.http2.DefaultHttp2LocalFlowController.DEFAULT_WINDOW_UPDATE_RATIO;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import io.grpc.Attributes;
import io.grpc.InternalChannelz;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import io.grpc.internal.ClientTransport.PingCallback;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.Http2Ping;
import io.grpc.internal.KeepAliveManager;
import io.grpc.internal.TransportTracer;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2ClientHeadersDecoder;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufUtil;
import io.netty.buffer.Unpooled;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionDecoder;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionEncoder;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.DefaultHttp2LocalFlowController;
import io.netty.handler.codec.http2.DefaultHttp2RemoteFlowController;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionAdapter;
import io.netty.handler.codec.http2.Http2ConnectionDecoder;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2FlowController;
import io.netty.handler.codec.http2.Http2FrameAdapter;
import io.netty.handler.codec.http2.Http2FrameLogger;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2HeadersDecoder;
import io.netty.handler.codec.http2.Http2InboundFrameLogger;
import io.netty.handler.codec.http2.Http2OutboundFrameLogger;
import io.netty.handler.codec.http2.Http2Settings;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.handler.codec.http2.Http2StreamVisitor;
import io.netty.handler.codec.http2.StreamBufferingEncoder;
import io.netty.handler.codec.http2.WeightedFairQueueByteDistributor;
import io.netty.handler.logging.LogLevel;
import java.nio.channels.ClosedChannelException;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Client-side Netty handler for GRPC processing. All event handlers are executed entirely within
 * the context of the Netty Channel thread.
 */
class NettyClientHandler extends AbstractNettyHandler {
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
  private static final long USER_PING_PAYLOAD = 1111;

  private final Http2Connection.PropertyKey streamKey;
  private final ClientTransportLifecycleManager lifecycleManager;
  private final KeepAliveManager keepAliveManager;
  // Returns new unstarted stopwatches
  private final Supplier<Stopwatch> stopwatchFactory;
  private final TransportTracer transportTracer;
  private final Attributes eagAttributes;
  private final String authority;
  private WriteQueue clientWriteQueue;
  private Http2Ping ping;
  private Attributes attributes = Attributes.EMPTY;
  private InternalChannelz.Security securityInfo;

  static NettyClientHandler newHandler(
      ClientTransportLifecycleManager lifecycleManager,
      @Nullable KeepAliveManager keepAliveManager,
      int flowControlWindow,
      int maxHeaderListSize,
      Supplier<Stopwatch> stopwatchFactory,
      Runnable tooManyPingsRunnable,
      TransportTracer transportTracer,
      Attributes eagAttributes,
      String authority) {
    Preconditions.checkArgument(maxHeaderListSize > 0, "maxHeaderListSize must be positive");
    Http2HeadersDecoder headersDecoder = new GrpcHttp2ClientHeadersDecoder(maxHeaderListSize);
    Http2FrameReader frameReader = new DefaultHttp2FrameReader(headersDecoder);
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();
    Http2Connection connection = new DefaultHttp2Connection(false);
    WeightedFairQueueByteDistributor dist = new WeightedFairQueueByteDistributor(connection);
    dist.allocationQuantum(16 * 1024); // Make benchmarks fast again.
    DefaultHttp2RemoteFlowController controller =
        new DefaultHttp2RemoteFlowController(connection, dist);
    connection.remote().flowController(controller);

    return newHandler(
        connection,
        frameReader,
        frameWriter,
        lifecycleManager,
        keepAliveManager,
        flowControlWindow,
        maxHeaderListSize,
        stopwatchFactory,
        tooManyPingsRunnable,
        transportTracer,
        eagAttributes,
        authority);
  }

  @VisibleForTesting
  static NettyClientHandler newHandler(
      final Http2Connection connection,
      Http2FrameReader frameReader,
      Http2FrameWriter frameWriter,
      ClientTransportLifecycleManager lifecycleManager,
      KeepAliveManager keepAliveManager,
      int flowControlWindow,
      int maxHeaderListSize,
      Supplier<Stopwatch> stopwatchFactory,
      Runnable tooManyPingsRunnable,
      TransportTracer transportTracer,
      Attributes eagAttributes,
      String authority) {
    Preconditions.checkNotNull(connection, "connection");
    Preconditions.checkNotNull(frameReader, "frameReader");
    Preconditions.checkNotNull(lifecycleManager, "lifecycleManager");
    Preconditions.checkArgument(flowControlWindow > 0, "flowControlWindow must be positive");
    Preconditions.checkArgument(maxHeaderListSize > 0, "maxHeaderListSize must be positive");
    Preconditions.checkNotNull(stopwatchFactory, "stopwatchFactory");
    Preconditions.checkNotNull(tooManyPingsRunnable, "tooManyPingsRunnable");
    Preconditions.checkNotNull(eagAttributes, "eagAttributes");
    Preconditions.checkNotNull(authority, "authority");

    Http2FrameLogger frameLogger = new Http2FrameLogger(LogLevel.DEBUG, NettyClientHandler.class);
    frameReader = new Http2InboundFrameLogger(frameReader, frameLogger);
    frameWriter = new Http2OutboundFrameLogger(frameWriter, frameLogger);

    StreamBufferingEncoder encoder = new StreamBufferingEncoder(
        new DefaultHttp2ConnectionEncoder(connection, frameWriter));

    // Create the local flow controller configured to auto-refill the connection window.
    connection.local().flowController(
        new DefaultHttp2LocalFlowController(connection, DEFAULT_WINDOW_UPDATE_RATIO, true));

    Http2ConnectionDecoder decoder = new DefaultHttp2ConnectionDecoder(connection, encoder,
        frameReader);

    transportTracer.setFlowControlWindowReader(new TransportTracer.FlowControlReader() {
      final Http2FlowController local = connection.local().flowController();
      final Http2FlowController remote = connection.remote().flowController();

      @Override
      public TransportTracer.FlowControlWindows read() {
        return new TransportTracer.FlowControlWindows(
            local.windowSize(connection.connectionStream()),
            remote.windowSize(connection.connectionStream()));
      }
    });

    Http2Settings settings = new Http2Settings();
    settings.pushEnabled(false);
    settings.initialWindowSize(flowControlWindow);
    settings.maxConcurrentStreams(0);
    settings.maxHeaderListSize(maxHeaderListSize);

    return new NettyClientHandler(
        decoder,
        encoder,
        settings,
        lifecycleManager,
        keepAliveManager,
        stopwatchFactory,
        tooManyPingsRunnable,
        transportTracer,
        eagAttributes,
        authority);
  }

  private NettyClientHandler(
      Http2ConnectionDecoder decoder,
      StreamBufferingEncoder encoder,
      Http2Settings settings,
      ClientTransportLifecycleManager lifecycleManager,
      KeepAliveManager keepAliveManager,
      Supplier<Stopwatch> stopwatchFactory,
      final Runnable tooManyPingsRunnable,
      TransportTracer transportTracer,
      Attributes eagAttributes,
      String authority) {
    super(/* channelUnused= */ null, decoder, encoder, settings);
    this.lifecycleManager = lifecycleManager;
    this.keepAliveManager = keepAliveManager;
    this.stopwatchFactory = stopwatchFactory;
    this.transportTracer = Preconditions.checkNotNull(transportTracer);
    this.eagAttributes = eagAttributes;
    this.authority = authority;

    // Set the frame listener on the decoder.
    decoder().frameListener(new FrameListener());

    Http2Connection connection = encoder.connection();
    streamKey = connection.newKey();

    connection.addListener(new Http2ConnectionAdapter() {
      @Override
      public void onGoAwayReceived(int lastStreamId, long errorCode, ByteBuf debugData) {
        byte[] debugDataBytes = ByteBufUtil.getBytes(debugData);
        goingAway(statusFromGoAway(errorCode, debugDataBytes));
        if (errorCode == Http2Error.ENHANCE_YOUR_CALM.code()) {
          String data = new String(debugDataBytes, UTF_8);
          logger.log(
              Level.WARNING, "Received GOAWAY with ENHANCE_YOUR_CALM. Debug data: {1}", data);
          if ("too_many_pings".equals(data)) {
            tooManyPingsRunnable.run();
          }
        }
      }

      @Override
      public void onStreamActive(Http2Stream stream) {
        if (connection().numActiveStreams() != 1) {
          return;
        }

        NettyClientHandler.this.lifecycleManager.notifyInUse(true);

        if (NettyClientHandler.this.keepAliveManager != null) {
          NettyClientHandler.this.keepAliveManager.onTransportActive();
        }
      }

      @Override
      public void onStreamClosed(Http2Stream stream) {
        if (connection().numActiveStreams() != 0) {
          return;
        }

        NettyClientHandler.this.lifecycleManager.notifyInUse(false);

        if (NettyClientHandler.this.keepAliveManager != null) {
          NettyClientHandler.this.keepAliveManager.onTransportIdle();
        }
      }
    });
  }

  /**
   * The protocol negotiation attributes, available once the protocol negotiation completes;
   * otherwise returns {@code Attributes.EMPTY}.
   */
  Attributes getAttributes() {
    return attributes;
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
    } else if (msg instanceof SendPingCommand) {
      sendPingFrame(ctx, (SendPingCommand) msg, promise);
    } else if (msg instanceof GracefulCloseCommand) {
      gracefulClose(ctx, (GracefulCloseCommand) msg, promise);
    } else if (msg instanceof ForcefulCloseCommand) {
      forcefulClose(ctx, (ForcefulCloseCommand) msg, promise);
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

  ClientTransportLifecycleManager getLifecycleManager() {
    return lifecycleManager;
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

  private void onHeadersRead(int streamId, Http2Headers headers, boolean endStream) {
    NettyClientStream.TransportState stream = clientStream(requireHttp2Stream(streamId));
    stream.transportHeadersReceived(headers, endStream);
    if (keepAliveManager != null) {
      keepAliveManager.onDataReceived();
    }
  }

  /**
   * Handler for an inbound HTTP/2 DATA frame.
   */
  private void onDataRead(int streamId, ByteBuf data, int padding, boolean endOfStream) {
    flowControlPing().onDataRead(data.readableBytes(), padding);
    NettyClientStream.TransportState stream = clientStream(requireHttp2Stream(streamId));
    stream.transportDataReceived(data, endOfStream);
    if (keepAliveManager != null) {
      keepAliveManager.onDataReceived();
    }
  }


  /**
   * Handler for an inbound HTTP/2 RST_STREAM frame, terminating a stream.
   */
  private void onRstStreamRead(int streamId, long errorCode) {
    NettyClientStream.TransportState stream = clientStream(connection().stream(streamId));
    if (stream != null) {
      Status status = GrpcUtil.Http2Error.statusForCode((int) errorCode)
          .augmentDescription("Received Rst Stream");
      stream.transportReportStatus(
          status,
          errorCode == Http2Error.REFUSED_STREAM.code()
              ? RpcProgress.REFUSED : RpcProgress.PROCESSED,
          false /*stop delivery*/,
          new Metadata());
      if (keepAliveManager != null) {
        keepAliveManager.onDataReceived();
      }
    }
  }

  @Override
  public void close(ChannelHandlerContext ctx, ChannelPromise promise) throws Exception {
    logger.fine("Network channel being closed by the application.");
    if (ctx.channel().isActive()) { // Ignore notification that the socket was closed
      lifecycleManager.notifyShutdown(
          Status.UNAVAILABLE.withDescription("Transport closed for unknown reason"));
    }
    super.close(ctx, promise);
  }

  /**
   * Handler for the Channel shutting down.
   */
  @Override
  public void channelInactive(ChannelHandlerContext ctx) throws Exception {
    try {
      logger.fine("Network channel is closed");
      Status status = Status.UNAVAILABLE.withDescription("Network closed for unknown reason");
      lifecycleManager.notifyShutdown(status);
      try {
        cancelPing(lifecycleManager.getShutdownThrowable());
        // Report status to the application layer for any open streams
        connection().forEachActiveStream(new Http2StreamVisitor() {
          @Override
          public boolean visit(Http2Stream stream) throws Http2Exception {
            NettyClientStream.TransportState clientStream = clientStream(stream);
            if (clientStream != null) {
              clientStream.transportReportStatus(
                  lifecycleManager.getShutdownStatus(), false, new Metadata());
            }
            return true;
          }
        });
      } finally {
        lifecycleManager.notifyTerminated(status);
      }
    } finally {
      // Close any open streams
      super.channelInactive(ctx);
      if (keepAliveManager != null) {
        keepAliveManager.onTransportTermination();
      }
    }
  }

  @Override
  public void handleProtocolNegotiationCompleted(
      Attributes attributes, InternalChannelz.Security securityInfo) {
    this.attributes = attributes;
    this.securityInfo = securityInfo;
    super.handleProtocolNegotiationCompleted(attributes, securityInfo);
  }

  @Override
  public Attributes getEagAttributes() {
    return eagAttributes;
  }

  @Override
  public String getAuthority() {
    return authority;
  }

  InternalChannelz.Security getSecurityInfo() {
    return securityInfo;
  }


  @Override
  protected void onConnectionError(ChannelHandlerContext ctx,  boolean outbound, Throwable cause,
      Http2Exception http2Ex) {
    logger.log(Level.FINE, "Caught a connection error", cause);
    lifecycleManager.notifyShutdown(Utils.statusFromThrowable(cause));
    // Parent class will shut down the Channel
    super.onConnectionError(ctx, outbound, cause, http2Ex);
  }

  @Override
  protected void onStreamError(ChannelHandlerContext ctx, boolean outbound, Throwable cause,
      Http2Exception.StreamException http2Ex) {
    // Close the stream with a status that contains the cause.
    NettyClientStream.TransportState stream = clientStream(connection().stream(http2Ex.streamId()));
    if (stream != null) {
      stream.transportReportStatus(Utils.statusFromThrowable(cause), false, new Metadata());
    } else {
      logger.log(Level.FINE, "Stream error for unknown stream " + http2Ex.streamId(), cause);
    }

    // Delegate to the base class to send a RST_STREAM.
    super.onStreamError(ctx, outbound, cause, http2Ex);
  }

  @Override
  protected boolean isGracefulShutdownComplete() {
    // Only allow graceful shutdown to complete after all pending streams have completed.
    return super.isGracefulShutdownComplete()
        && ((StreamBufferingEncoder) encoder()).numBufferedStreams() == 0;
  }

  /**
   * Attempts to create a new stream from the given command. If there are too many active streams,
   * the creation request is queued.
   */
  private void createStream(CreateStreamCommand command, final ChannelPromise promise)
          throws Exception {
    if (lifecycleManager.getShutdownThrowable() != null) {
      // The connection is going away (it is really the GOAWAY case),
      // just terminate the stream now.
      command.stream().transportReportStatus(
          lifecycleManager.getShutdownStatus(), RpcProgress.REFUSED, true, new Metadata());
      promise.setFailure(lifecycleManager.getShutdownThrowable());
      return;
    }

    // Get the stream ID for the new stream.
    final int streamId;
    try {
      streamId = incrementAndGetNextStreamId();
    } catch (StatusException e) {
      // Stream IDs have been exhausted for this connection. Fail the promise immediately.
      promise.setFailure(e);

      // Initiate a graceful shutdown if we haven't already.
      if (!connection().goAwaySent()) {
        logger.fine("Stream IDs have been exhausted for this connection. "
                + "Initiating graceful shutdown of the connection.");
        lifecycleManager.notifyShutdown(e.getStatus());
        close(ctx(), ctx().newPromise());
      }
      return;
    }

    final NettyClientStream.TransportState stream = command.stream();
    final Http2Headers headers = command.headers();
    stream.setId(streamId);

    // Create an intermediate promise so that we can intercept the failure reported back to the
    // application.
    ChannelPromise tempPromise = ctx().newPromise();
    encoder().writeHeaders(ctx(), streamId, headers, 0, command.isGet(), tempPromise)
            .addListener(new ChannelFutureListener() {
              @Override
              public void operationComplete(ChannelFuture future) throws Exception {
                if (future.isSuccess()) {
                  // The http2Stream will be null in case a stream buffered in the encoder
                  // was canceled via RST_STREAM.
                  Http2Stream http2Stream = connection().stream(streamId);
                  if (http2Stream != null) {
                    stream.getStatsTraceContext().clientOutboundHeaders();
                    http2Stream.setProperty(streamKey, stream);

                    // Attach the client stream to the HTTP/2 stream object as user data.
                    stream.setHttp2Stream(http2Stream);
                  }
                  // Otherwise, the stream has been cancelled and Netty is sending a
                  // RST_STREAM frame which causes it to purge pending writes from the
                  // flow-controller and delete the http2Stream. The stream listener has already
                  // been notified of cancellation so there is nothing to do.

                  // Just forward on the success status to the original promise.
                  promise.setSuccess();
                } else {
                  final Throwable cause = future.cause();
                  if (cause instanceof StreamBufferingEncoder.Http2GoAwayException) {
                    StreamBufferingEncoder.Http2GoAwayException e =
                        (StreamBufferingEncoder.Http2GoAwayException) cause;
                    lifecycleManager.notifyShutdown(statusFromGoAway(e.errorCode(), e.debugData()));
                    promise.setFailure(lifecycleManager.getShutdownThrowable());
                  } else {
                    promise.setFailure(cause);
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
    NettyClientStream.TransportState stream = cmd.stream();
    Status reason = cmd.reason();
    if (reason != null) {
      stream.transportReportStatus(reason, true, new Metadata());
    }
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
    // Don't check lifecycleManager.getShutdownStatus() since we want to allow pings after shutdown
    // but before termination. After termination, messages will no longer arrive because the
    // pipeline clears all handlers on channel close.

    PingCallback callback = msg.callback();
    Executor executor = msg.executor();
    // we only allow one outstanding ping at a time, so just add the callback to
    // any outstanding operation
    if (ping != null) {
      promise.setSuccess();
      ping.addCallback(callback, executor);
      return;
    }

    // Use a new promise to prevent calling the callback twice on write failure: here and in
    // NettyClientTransport.ping(). It may appear strange, but it will behave the same as if
    // ping != null above.
    promise.setSuccess();
    promise = ctx().newPromise();
    // set outstanding operation
    long data = USER_PING_PAYLOAD;
    Stopwatch stopwatch = stopwatchFactory.get();
    stopwatch.start();
    ping = new Http2Ping(data, stopwatch);
    ping.addCallback(callback, executor);
    // and then write the ping
    encoder().writePing(ctx, false, USER_PING_PAYLOAD, promise);
    ctx.flush();
    final Http2Ping finalPing = ping;
    promise.addListener(new ChannelFutureListener() {
      @Override
      public void operationComplete(ChannelFuture future) throws Exception {
        if (future.isSuccess()) {
          transportTracer.reportKeepAliveSent();
        } else {
          Throwable cause = future.cause();
          if (cause instanceof ClosedChannelException) {
            cause = lifecycleManager.getShutdownThrowable();
            if (cause == null) {
              cause = Status.UNKNOWN.withDescription("Ping failed but for unknown reason.")
                  .withCause(future.cause()).asException();
            }
          }
          finalPing.failed(cause);
          if (ping == finalPing) {
            ping = null;
          }
        }
      }
    });
  }

  private void gracefulClose(ChannelHandlerContext ctx, GracefulCloseCommand msg,
      ChannelPromise promise) throws Exception {
    lifecycleManager.notifyShutdown(msg.getStatus());
    // Explicitly flush to create any buffered streams before sending GOAWAY.
    // TODO(ejona): determine if the need to flush is a bug in Netty
    flush(ctx);
    close(ctx, promise);
  }

  private void forcefulClose(final ChannelHandlerContext ctx, final ForcefulCloseCommand msg,
      ChannelPromise promise) throws Exception {
    // close() already called by NettyClientTransport, so just need to clean up streams
    connection().forEachActiveStream(new Http2StreamVisitor() {
      @Override
      public boolean visit(Http2Stream stream) throws Http2Exception {
        NettyClientStream.TransportState clientStream = clientStream(stream);
        if (clientStream != null) {
          clientStream.transportReportStatus(msg.getStatus(), true, new Metadata());
          resetStream(ctx, stream.id(), Http2Error.CANCEL.code(), ctx.newPromise());
        }
        stream.close();
        return true;
      }
    });
    promise.setSuccess();
  }

  /**
   * Handler for a GOAWAY being received. Fails any streams created after the
   * last known stream.
   */
  private void goingAway(Status status) {
    lifecycleManager.notifyShutdown(status);
    final Status goAwayStatus = lifecycleManager.getShutdownStatus();
    final int lastKnownStream = connection().local().lastStreamKnownByPeer();
    try {
      connection().forEachActiveStream(new Http2StreamVisitor() {
        @Override
        public boolean visit(Http2Stream stream) throws Http2Exception {
          if (stream.id() > lastKnownStream) {
            NettyClientStream.TransportState clientStream = clientStream(stream);
            if (clientStream != null) {
              clientStream.transportReportStatus(
                  goAwayStatus, RpcProgress.REFUSED, false, new Metadata());
            }
            stream.close();
          }
          return true;
        }
      });
    } catch (Http2Exception e) {
      throw new RuntimeException(e);
    }
  }

  private void cancelPing(Throwable t) {
    if (ping != null) {
      ping.failed(t);
      ping = null;
    }
  }

  private Status statusFromGoAway(long errorCode, byte[] debugData) {
    Status status = GrpcUtil.Http2Error.statusForCode((int) errorCode)
        .augmentDescription("Received Goaway");
    if (debugData != null && debugData.length > 0) {
      // If a debug message was provided, use it.
      String msg = new String(debugData, UTF_8);
      status = status.augmentDescription(msg);
    }
    return status;
  }

  /**
   * Gets the client stream associated to the given HTTP/2 stream object.
   */
  private NettyClientStream.TransportState clientStream(Http2Stream stream) {
    return stream == null ? null : (NettyClientStream.TransportState) stream.getProperty(streamKey);
  }

  private int incrementAndGetNextStreamId() throws StatusException {
    int nextStreamId = connection().local().incrementAndGetNextStreamId();
    if (nextStreamId < 0) {
      logger.fine("Stream IDs have been exhausted for this connection. "
              + "Initiating graceful shutdown of the connection.");
      throw EXHAUSTED_STREAMS_STATUS.asException();
    }
    return nextStreamId;
  }

  private Http2Stream requireHttp2Stream(int streamId) {
    Http2Stream stream = connection().stream(streamId);
    if (stream == null) {
      // This should never happen.
      throw new AssertionError("Stream does not exist: " + streamId);
    }
    return stream;
  }

  private class FrameListener extends Http2FrameAdapter {
    private boolean firstSettings = true;

    @Override
    public void onSettingsRead(ChannelHandlerContext ctx, Http2Settings settings) {
      if (firstSettings) {
        firstSettings = false;
        lifecycleManager.notifyReady();
      }
    }

    @Override
    public int onDataRead(ChannelHandlerContext ctx, int streamId, ByteBuf data, int padding,
        boolean endOfStream) throws Http2Exception {
      NettyClientHandler.this.onDataRead(streamId, data, padding, endOfStream);
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
      NettyClientHandler.this.onHeadersRead(streamId, headers, endStream);
    }

    @Override
    public void onRstStreamRead(ChannelHandlerContext ctx, int streamId, long errorCode)
        throws Http2Exception {
      NettyClientHandler.this.onRstStreamRead(streamId, errorCode);
    }

    @Override
    public void onPingAckRead(ChannelHandlerContext ctx, long ackPayload) throws Http2Exception {
      Http2Ping p = ping;
      if (ackPayload == flowControlPing().payload()) {
        flowControlPing().updateWindow();
        if (logger.isLoggable(Level.FINE)) {
          logger.log(Level.FINE, String.format("Window: %d",
              decoder().flowController().initialWindowSize(connection().connectionStream())));
        }
      } else if (p != null) {
        if (p.payload() == ackPayload) {
          p.complete();
          ping = null;
        } else {
          logger.log(Level.WARNING, String.format(
              "Received unexpected ping ack. Expecting %d, got %d", p.payload(), ackPayload));
        }
      } else {
        logger.warning("Received unexpected ping ack. No ping outstanding");
      }
      if (keepAliveManager != null) {
        keepAliveManager.onDataReceived();
      }
    }

    @Override
    public void onPingRead(ChannelHandlerContext ctx, long data) throws Http2Exception {
      if (keepAliveManager != null) {
        keepAliveManager.onDataReceived();
      }
    }
  }
}
