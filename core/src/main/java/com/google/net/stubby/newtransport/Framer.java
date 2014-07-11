package com.google.net.stubby.newtransport;

import com.google.net.stubby.Status;

import java.io.InputStream;

/**
 * Implementations produce the GRPC byte sequence and then split it over multiple frames to be
 * delivered via the transport layer which implements {@link Framer.Sink}
 */
public interface Framer {

  /**
   * Sink implemented by the transport layer to receive frames and forward them to their destination
   */
  public interface Sink<T> {
    /**
     * Deliver a frame via the transport.
     *
     * @param frame the contents of the frame to deliver
     * @param endOfStream whether the frame is the last one for the GRPC stream
     */
    public void deliverFrame(T frame, boolean endOfStream);
  }

  /**
   * Write out a Context-Value message. {@code message} will be completely consumed.
   * {@code message.available()} must return the number of remaining bytes to be read.
   */
  public void writeContext(String type, InputStream message, int length);

  /**
   * Write out a Payload message. {@code payload} will be completely consumed.
   * {@code payload.available()} must return the number of remaining bytes to be read.
   */
  public void writePayload(InputStream payload, int length);

  /**
   * Write out a Status message.
   */
  // TODO(user): change this signature when we actually start writing out the complete Status.
  public void writeStatus(Status status);

  /**
   * Flush any buffered data in the framer to the sink.
   */
  public void flush();

  /**
   * Indicates whether or not this {@link Framer} has been closed via a call to either
   * {@link #close()} or {@link #dispose()}.
   */
  public boolean isClosed();

  /**
   * Flushes and closes the framer and releases any buffers. After the {@link Framer} is closed or
   * disposed, additional calls to this method will have no affect.
   */
  public void close();

  /**
   * Closes the framer and releases any buffers, but does not flush. After the {@link Framer} is
   * closed or disposed, additional calls to this method will have no affect.
   */
  public void dispose();
}
