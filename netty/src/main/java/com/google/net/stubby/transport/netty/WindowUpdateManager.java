package com.google.net.stubby.transport.netty;

import static io.netty.handler.codec.http2.DefaultHttp2InboundFlowController.DEFAULT_WINDOW_UPDATE_RATIO;
import static io.netty.handler.codec.http2.DefaultHttp2InboundFlowController.WINDOW_UPDATE_OFF;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;

import io.netty.channel.Channel;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;

import javax.annotation.Nullable;

/**
 * An object that manages inbound flow control for a single stream by disabling sending of HTTP/2
 * {@code WINDOW_UPDATE} frames until the previously delivered message completes.
 */
public class WindowUpdateManager {

  private int streamId = -1;
  private final Channel channel;
  private final DefaultHttp2InboundFlowController inboundFlow;
  private final Runnable enableWindowUpdateTask;

  public WindowUpdateManager(Channel channel, DefaultHttp2InboundFlowController inboundFlow) {
    this.channel = Preconditions.checkNotNull(channel, "channel");
    this.inboundFlow = Preconditions.checkNotNull(inboundFlow, "inboundFlow");
    enableWindowUpdateTask = new Runnable() {
      @Override
      public void run() {
        // Restore the window update ratio for this stream.
        setWindowUpdateRatio(DEFAULT_WINDOW_UPDATE_RATIO);
      }
    };
  }

  /**
   * Sets the ID of the stream for which inbound data should be controlled.
   */
  public void streamId(int streamId) {
    this.streamId = streamId;
  }


  /**
   * Temporarily disables the sending of HTTP/2 {@code WINDOW_UPDATE} frames until the given future
   * completes. If the future is {@code null} or is already completed, this method does nothing.
   */
  public void disableWindowUpdate(@Nullable ListenableFuture<Void> processingFuture) {
    if (processingFuture != null && !processingFuture.isDone()) {
      setWindowUpdateRatio(WINDOW_UPDATE_OFF);

      // When the future completes, re-enable window updates in the channel thread.
      processingFuture.addListener(enableWindowUpdateTask, channel.eventLoop());
    }
  }

  private void setWindowUpdateRatio(double ratio) {
    inboundFlow.setWindowUpdateRatio(channel.pipeline().firstContext(), streamId, ratio);
  }
}
