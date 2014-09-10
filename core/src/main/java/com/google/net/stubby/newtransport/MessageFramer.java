package com.google.net.stubby.newtransport;

import com.google.net.stubby.GrpcFramingUtil;
import com.google.net.stubby.Status;

import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;

/**
 * Default {@link Framer} implementation.
 */
public class MessageFramer implements Framer {

  private CompressionFramer framer;
  private final ByteBuffer scratch = ByteBuffer.allocate(16);

  public MessageFramer(Sink<ByteBuffer> sink, int maxFrameSize) {
    // TODO(user): maxFrameSize should probably come from a 'Platform' class
    framer = new CompressionFramer(sink, maxFrameSize, false, maxFrameSize / 16);
  }

  /**
   * Sets whether compression is encouraged.
   */
  public void setAllowCompression(boolean enable) {
    verifyNotClosed();
    framer.setAllowCompression(enable);
  }

  @Override
  public void writePayload(InputStream message, int messageLength) {
    verifyNotClosed();
    try {
      scratch.clear();
      scratch.put(GrpcFramingUtil.PAYLOAD_FRAME);
      scratch.putInt(messageLength);
      framer.write(scratch.array(), 0, scratch.position());
      if (messageLength != framer.write(message)) {
        throw new RuntimeException("Message length was inaccurate");
      }
      framer.endOfMessage();
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }


  @Override
  public void writeStatus(Status status) {
    verifyNotClosed();
    short code = (short) status.getCode().getNumber();
    scratch.clear();
    scratch.put(GrpcFramingUtil.STATUS_FRAME);
    int length = 2;
    scratch.putInt(length);
    scratch.putShort(code);
    framer.write(scratch.array(), 0, scratch.position());
    framer.endOfMessage();
  }

  @Override
  public void flush() {
    verifyNotClosed();
    framer.flush();
  }

  @Override
  public boolean isClosed() {
    return framer == null;
  }

  @Override
  public void close() {
    if (!isClosed()) {
      // TODO(user): Returning buffer to a pool would go here
      framer.close();
      framer = null;
    }
  }

  @Override
  public void dispose() {
    // TODO(user): Returning buffer to a pool would go here
    framer = null;
  }

  private void verifyNotClosed() {
    if (isClosed()) {
      throw new IllegalStateException("Framer already closed");
    }
  }
}
