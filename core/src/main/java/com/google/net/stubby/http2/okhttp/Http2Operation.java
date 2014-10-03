package com.google.net.stubby.http2.okhttp;

import com.google.common.io.ByteBuffers;
import com.google.net.stubby.AbstractOperation;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;

import com.squareup.okhttp.internal.spdy.FrameWriter;

import okio.Buffer;

import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;

/**
 * Base implementation of {@link Operation} that writes HTTP2 frames
 */
abstract class Http2Operation extends AbstractOperation implements Framer.Sink {

  protected final Framer framer;
  private final FrameWriter frameWriter;

  Http2Operation(int id, FrameWriter frameWriter, Framer framer) {
    super(id);
    this.frameWriter = frameWriter;
    this.framer = framer;
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
      // Read the data into a buffer.
      // TODO(user): swap to NIO buffers or zero-copy if/when okhttp/okio supports it
      Buffer buffer = new Buffer().readFrom(ByteBuffers.newConsumingInputStream(frame));

      // Write the data to the remote endpoint.
      frameWriter.data(closed && endOfMessage, getId(), buffer, (int) buffer.size());
      frameWriter.flush();
    } catch (IOException ioe) {
      close(Status.INTERNAL.withCause(ioe));
    } finally {
      if (closed && endOfMessage) {
        framer.close();
      }
    }
  }
}
