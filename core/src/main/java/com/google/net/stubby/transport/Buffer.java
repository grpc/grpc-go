package com.google.net.stubby.transport;

import java.io.Closeable;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;

/**
 * Interface for an abstract byte buffer. Buffers are intended to be a read-only, except for the
 * read position which is incremented after each read call.
 *
 * <p>Buffers may optionally expose a backing array for optimization purposes, similar to what is
 * done in {@link ByteBuffer}. It is not expected that callers will attempt to modify the backing
 * array.
 */
public interface Buffer extends Closeable {

  /**
   * Gets the current number of readable bytes remaining in this buffer.
   */
  int readableBytes();

  /**
   * Reads the next unsigned byte from this buffer and increments the read position by 1.
   *
   * @throws IndexOutOfBoundsException if required bytes are not readable
   */
  int readUnsignedByte();

  /**
   * Reads a 3-byte unsigned integer from this buffer using big-endian byte ordering. Increments the
   * read position by 3.
   *
   * @throws IndexOutOfBoundsException if required bytes are not readable
   */
  int readUnsignedMedium();

  /**
   * Reads a 2-byte unsigned integer from this buffer using big-endian byte ordering. Increments the
   * read position by 2.
   *
   * @throws IndexOutOfBoundsException if required bytes are not readable
   */
  int readUnsignedShort();

  /**
   * Reads a 4-byte signed integer from this buffer using big-endian byte ordering. Increments the
   * read position by 4.
   *
   * @throws IndexOutOfBoundsException if required bytes are not readable
   */
  int readInt();

  /**
   * Increments the read position by the given length.
   *
   * @throws IndexOutOfBoundsException if required bytes are not readable
   */
  void skipBytes(int length);

  /**
   * Reads {@code length} bytes from this buffer and writes them to the destination array.
   * Increments the read position by {@code length}.
   *
   * @param dest the destination array to receive the bytes.
   * @param destOffset the starting offset in the destination array.
   * @param length the number of bytes to be copied.
   * @throws IndexOutOfBoundsException if required bytes are not readable or {@code dest} is too
   *     small.
   */
  void readBytes(byte[] dest, int destOffset, int length);

  /**
   * Reads from this buffer until the destination's position reaches its limit, and increases the
   * read position by the number of the transferred bytes.
   *
   * @param dest the destination buffer to receive the bytes.
   * @throws IndexOutOfBoundsException if required bytes are not readable
   */
  void readBytes(ByteBuffer dest);

  /**
   * Reads {@code length} bytes from this buffer and writes them to the destination stream.
   * Increments the read position by {@code length}. If the required bytes are not readable, throws
   * {@link IndexOutOfBoundsException}.
   *
   * @param dest the destination stream to receive the bytes.
   * @param length the number of bytes to be copied.
   * @throws IOException thrown if any error was encountered while writing to the stream.
   * @throws IndexOutOfBoundsException if required bytes are not readable
   */
  void readBytes(OutputStream dest, int length) throws IOException;

  /**
   * Reads {@code length} bytes from this buffer and returns a new Buffer containing them. Some
   * implementations may return a Buffer sharing the backing memory with this buffer to prevent
   * copying. However, that means that the returned buffer may keep the (possibly much larger)
   * backing memory in use even after this buffer is closed.
   *
   * @param length the number of bytes to contain in returned Buffer.
   * @throws IndexOutOfBoundsException if required bytes are not readable
   */
  Buffer readBytes(int length);

  /**
   * Indicates whether or not this buffer exposes a backing array.
   */
  boolean hasArray();

  /**
   * Gets the backing array for this buffer. This is an optional method, so callers should first
   * check {@link #hasArray}. Buffers not supporting this method will throw
   * {@link UnsupportedOperationException}.
   */
  byte[] array();

  /**
   * Gets the offset in the backing array of the current read position. This is an optional method,
   * so callers should first check {@link #hasArray}. Buffers not supporting this method will throw
   * {@link UnsupportedOperationException}.
   */
  int arrayOffset();

  /**
   * Closes this buffer and releases any resources.
   */
  @Override
  void close();
}
