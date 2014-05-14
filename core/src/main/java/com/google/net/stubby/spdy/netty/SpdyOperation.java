package com.google.net.stubby.spdy.netty;

import com.google.net.stubby.AbstractOperation;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;
import com.google.net.stubby.transport.Transport;

import io.netty.buffer.Unpooled;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.handler.codec.spdy.DefaultSpdyDataFrame;
import io.netty.handler.codec.spdy.SpdyHeadersFrame;

import java.io.InputStream;
import java.nio.ByteBuffer;

/**
 * Base implementation of {@link Operation} that writes SPDY frames
 */
abstract class SpdyOperation extends AbstractOperation implements Framer.Sink {

  protected final Framer framer;
  private final Channel channel;

  SpdyOperation(SpdyHeadersFrame headersFrame, Channel channel, Framer framer) {
    super(headersFrame.getStreamId());
    this.channel = channel;
    this.framer = framer;
    channel.write(headersFrame);
  }

  @Override
  public Operation addContext(String type, InputStream message, Phase nextPhase) {
    super.addContext(type, message, nextPhase);
    framer.writeContext(type, message, getPhase() == Phase.CLOSED, this);
    return this;
  }

  @Override
  public Operation addPayload(InputStream payload, Phase nextPhase) {
    super.addPayload(payload, nextPhase);
    framer.writePayload(payload, getPhase() == Phase.CLOSED, this);
    return this;
  }

  @Override
  public void deliverFrame(ByteBuffer frame, boolean endOfMessage) {
    boolean closed = getPhase() == Phase.CLOSED;
    DefaultSpdyDataFrame dataFrame = new DefaultSpdyDataFrame(getId(),
        Unpooled.wrappedBuffer(frame));
    boolean streamClosed = closed && endOfMessage;
    dataFrame.setLast(streamClosed);
    try {
      ChannelFuture channelFuture = channel.writeAndFlush(dataFrame);
      if (!streamClosed) {
        // Sync for all except the last frame to prevent buffer corruption.
        channelFuture.get();
      }
    } catch (Exception e) {
      close(new Status(Transport.Code.INTERNAL, e));
    } finally {
      if (streamClosed) {
        framer.close();
      }
    }
  }
}
