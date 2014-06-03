package com.google.net.stubby.newtransport;

import java.io.InputStream;

/**
 * A single stream of communication between two end-points within a transport.
 */
public interface Stream<T extends Stream<T>> {

  /**
   * Gets the current state of this stream.
   */
  StreamState state();

  /**
   * Closes the local side of this stream and flushes any remaining messages. After this is called,
   * no further messages may be sent on this stream, but additional messages may be received until
   * the remote end-point is closed. Calling this method automatically causes a {@link #flush()} to
   * occur, so this method may block if awaiting resources.
   */
  void close();

  /**
   * Writes the context name/value pair to the remote end-point. This method may block if awaiting
   * resources.
   *
   * @param name the unique application-defined name for the context propery.
   * @param value the value of the context property.
   * @param offset the offset within the value array that is the start of the value.
   * @param length the length of the value starting from the offset index.
   * @return this stream instance.
   */
  T writeContext(String name, byte[] value, int offset, int length);

  /**
   * Writes the context name/value pair to the remote end-point. This method may block if awaiting
   * resources.
   *
   * @param name the unique application-defined name for the context propery.
   * @param value the value of the context property.
   * @param length the length of the {@link InputStream}.
   * @return this stream instance.
   */
  T writeContext(String name, InputStream value, int length);

  /**
   * Writes a message payload to the remote end-point. This method may block if awaiting resources.
   *
   * @param message array containing the serialized message to be sent
   * @param offset the offset within the message array that is the start of the value.
   * @param length the length of the message starting from the offset index.
   * @return this stream instance.
   */
  T writeMessage(byte[] message, int offset, int length);

  /**
   * Writes a message payload to the remote end-point. This method may block if awaiting resources.
   *
   * @param message stream containing the serialized message to be sent
   * @param length the length of the {@link InputStream}.
   * @return this stream instance.
   */
  T writeMessage(InputStream message, int length);

  /**
   * Flushes any internally buffered messages to the remote end-point. This method may block if
   * awaiting resources.
   */
  T flush();
}
