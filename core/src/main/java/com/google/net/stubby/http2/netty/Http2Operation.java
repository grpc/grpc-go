package com.google.net.stubby.http2.netty;

import com.google.net.stubby.AbstractOperation;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;
import com.google.net.stubby.transport.Transport;

import java.io.InputStream;
import java.nio.ByteBuffer;

import io.netty.buffer.Unpooled;
import io.netty.channel.ChannelFuture;

/**
 * Base implementation of {@link Operation} that writes HTTP2 frames
 */
abstract class Http2Operation extends AbstractOperation implements Framer.Sink {

  private final Framer framer;
  private final Http2Codec.Http2Writer writer;

  Http2Operation(int streamId, Http2Codec.Http2Writer writer, Framer framer) {
    super(streamId);
    this.writer = writer;
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
  public Operation close(Status status) {
    boolean alreadyClosed = getPhase() == Phase.CLOSED;
    super.close(status);
    if (!alreadyClosed) {
      framer.writeStatus(status, true, this);
    }
    return this;
  }

  @Override
  public void deliverFrame(ByteBuffer frame, boolean endOfMessage) {
    boolean closed = getPhase() == Phase.CLOSED;

    try {
      ChannelFuture channelFuture = writer.writeData(getId(),
          Unpooled.wrappedBuffer(frame), closed);
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
