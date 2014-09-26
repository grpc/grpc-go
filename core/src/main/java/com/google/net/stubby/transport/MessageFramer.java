package com.google.net.stubby.transport;

import com.google.net.stubby.GrpcFramingUtil;
import com.google.net.stubby.Status;

import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;

/**
 * Default {@link Framer} implementation.
 */
public class MessageFramer implements Framer  {

  private CompressionFramer framer;
  private final ByteBuffer scratch = ByteBuffer.allocate(16);

  public MessageFramer(int maxFrameSize) {
    // TODO(user): maxFrameSize should probably come from a 'Platform' class
    framer = new CompressionFramer(maxFrameSize, false, maxFrameSize / 16);
  }

  /**
   * Sets whether compression is encouraged.
   */
  public void setAllowCompression(boolean enable) {
    framer.setAllowCompression(enable);
  }

  @Override
  public void writePayload(InputStream message, boolean flush, Sink sink) {
    try {
      scratch.clear();
      scratch.put(GrpcFramingUtil.PAYLOAD_FRAME);
      int messageLength = message.available();
      scratch.putInt(messageLength);
      framer.write(scratch.array(), 0, scratch.position(), sink);
      if (messageLength != framer.write(message, sink)) {
        throw new RuntimeException("InputStream's available() was inaccurate");
      }
      framer.endOfMessage(sink);
      if (flush && framer != null) {
        framer.flush(sink);
      }
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }

  @Override
  public void writeStatus(Status status, boolean flush, Sink sink) {
    short code = (short) status.getCode().value();
    scratch.clear();
    scratch.put(GrpcFramingUtil.STATUS_FRAME);
    int length = 2;
    scratch.putInt(length);
    scratch.putShort(code);
    framer.write(scratch.array(), 0, scratch.position(), sink);
    framer.endOfMessage(sink);
    if (flush && framer != null) {
      framer.flush(sink);
    }
  }

  @Override
  public void flush(Sink sink) {
    framer.flush(sink);
  }

  @Override
  public void close() {
    // TODO(user): Returning buffer to a pool would go here
    framer = null;
  }
}
