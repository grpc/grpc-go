package com.google.net.stubby.http2.netty;

import com.google.net.stubby.AbstractOperation;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;
import com.google.net.stubby.transport.Transport;

import io.netty.buffer.Unpooled;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.handler.codec.http2.draft10.frame.DefaultHttp2DataFrame;

import java.io.InputStream;
import java.nio.ByteBuffer;

/**
 * Base implementation of {@link Operation} that writes HTTP2 frames
 */
abstract class Http2Operation extends AbstractOperation implements Framer.Sink {

  protected final Framer framer;
  private final Channel channel;

  Http2Operation(int streamId, Channel channel, Framer framer) {
    super(streamId);
    this.channel = channel;
    this.framer = framer;
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
    DefaultHttp2DataFrame dataFrame = new DefaultHttp2DataFrame.Builder().setStreamId(getId())
        .setContent(Unpooled.wrappedBuffer(frame)).setEndOfStream(closed).build();
    try {
      ChannelFuture channelFuture = channel.writeAndFlush(dataFrame);
      if (!closed) {
        // Sync for all except the last frame to prevent buffer corruption.
        channelFuture.get();
      }
    } catch (Exception e) {
      close(new Status(Transport.Code.INTERNAL, e));
    } finally {
      if (closed) {
        framer.close();
      }
    }
  }
}
