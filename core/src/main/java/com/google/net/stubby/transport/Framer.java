package com.google.net.stubby.transport;

import com.google.net.stubby.Status;

import java.io.InputStream;
import java.nio.ByteBuffer;

/**
 * Implementations produce the GRPC byte sequence and then split it over multiple frames to be
 * delivered via the transport layer which implements {@link Framer.Sink}
 */
public interface Framer {

  /**
   * Sink implemented by the transport layer to receive frames and forward them to their
   * destination
   */
  public interface Sink {
    /**
     * Deliver a frame via the transport.
     * @param frame The contents of the frame to deliver
     * @param endOfMessage Whether the frame is the last one for the current GRPC message.
     */
    public void deliverFrame(ByteBuffer frame, boolean endOfMessage);
  }

  /**
   * Write out a Payload message. {@code payload} will be completely consumed.
   * {@code payload.available()} must return the number of remaining bytes to be read.
   */
  public void writePayload(InputStream payload, boolean flush, Sink sink);

  /**
   * Write out a Status message.
   */
  public void writeStatus(Status status, boolean flush, Sink sink);

  /**
   * Flush any buffered data in the framer to the sink.
   */
  public void flush(Sink sink);

  /**
   * Close the framer and release any buffers.
   */
  public void close();
}
