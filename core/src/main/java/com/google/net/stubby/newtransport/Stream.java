package com.google.net.stubby.newtransport;

import java.io.InputStream;

import javax.annotation.Nullable;

/**
 * A single stream of communication between two end-points within a transport.
 *
 * <p>An implementation doesn't need to be thread-safe.
 */
public interface Stream {

  /**
   * Gets the current state of this stream.
   */
  StreamState state();

  /**
   * Writes a message payload to the remote end-point. The bytes from the stream are immediate read
   * by the Transport. This method will always return immediately and will not wait for the write to
   * complete.
   *
   * <p>When the write is "accepted" by the transport, the given callback (if provided) will be
   * called. The definition of what it means to be "accepted" is up to the transport implementation,
   * but this is a general indication that the transport is capable of handling more out-bound data
   * on the stream. If the stream/connection is closed for any reason before the write could be
   * accepted, the callback will never be invoked.
   *
   * @param message stream containing the serialized message to be sent
   * @param length the length of the {@link InputStream}.
   * @param accepted an optional callback for when the transport has accepted the write.
   */
  void writeMessage(InputStream message, int length, @Nullable Runnable accepted);

  /**
   * Flushes any internally buffered messages to the remote end-point.
   */
  void flush();
}
