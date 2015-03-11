/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.transport;

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
public interface ReadableBuffer extends Closeable {

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
  ReadableBuffer readBytes(int length);

  /**
   * Indicates whether or not this buffer exposes a backing array.
   */
  boolean hasArray();

  /**
   * Gets the backing array for this buffer. This is an optional method, so callers should first
   * check {@link #hasArray}.
   *
   * @throws UnsupportedOperationException the buffer does not support this method
   */
  byte[] array();

  /**
   * Gets the offset in the backing array of the current read position. This is an optional method,
   * so callers should first check {@link #hasArray}
   *
   * @throws UnsupportedOperationException the buffer does not support this method
   */
  int arrayOffset();

  /**
   * Closes this buffer and releases any resources.
   */
  @Override
  void close();
}
