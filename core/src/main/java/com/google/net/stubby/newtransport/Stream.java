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
   * Closes the local side of this stream and flushes any remaining messages. After this is called,
   * no further messages may be sent on this stream, but additional messages may be received until
   * the remote end-point is closed.
   */
  void halfClose();

  /**
   * Writes the context name/value pair to the remote end-point. The bytes from the stream are
   * immediate read by the Transport. This method will always return immediately and will not wait
   * for the write to complete.
   *
   * <p>When the write is "accepted" by the transport, the given callback (if provided) will be
   * called. The definition of what it means to be "accepted" is up to the transport implementation,
   * but this is a general indication that the transport is capable of handling more out-bound data
   * on the stream. If the stream/connection is closed for any reason before the write could be
   * accepted, the callback will never be invoked. Any writes that are still pending upon receiving
   * a {@link StreamListener#closed} callback are assumed to be cancelled.
   *
   * @param name the unique application-defined name for the context property.
   * @param value the value of the context property.
   * @param length the length of the {@link InputStream}.
   * @param accepted an optional callback for when the transport has accepted the write.
   */
  void writeContext(String name, InputStream value, int length, @Nullable Runnable accepted);

  /**
   * Writes a message payload to the remote end-point. The bytes from the stream are immediate read
   * by the Transport. This method will always return immediately and will not wait for the write to
   * complete.
   *
   * <p>When the write is "accepted" by the transport, the given callback (if provided) will be
   * called. The definition of what it means to be "accepted" is up to the transport implementation,
   * but this is a general indication that the transport is capable of handling more out-bound data
   * on the stream. If the stream/connection is closed for any reason before the write could be
   * accepted, the callback will never be invoked. Any writes that are still pending upon receiving
   * a {@link StreamListener#closed} callback are assumed to be cancelled.
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
