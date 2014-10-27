package com.google.net.stubby.transport;

import java.io.Closeable;

import javax.annotation.Nullable;

/**
 * An object responsible for reading GRPC compression frames for a single stream.
 */
public interface Decompressor extends Closeable {

  /**
   * Adds the given chunk of a GRPC compression frame to the internal buffers. If the data is
   * compressed, it is uncompressed whenever possible (which may only be after the entire
   * compression frame has been received).
   *
   * <p>Some or all of the given {@code data} chunk may not be made immediately available via
   * {@link #readBytes} due to internal buffering.
   *
   * @param data a received chunk of a GRPC compression frame. Control over the life cycle for this
   *        buffer is given to this {@link Decompressor}. Only this {@link Decompressor} should call
   *        {@link Buffer#close} after this point.
   */
  void decompress(Buffer data);

  /**
   * Reads up to the given number of bytes. Ownership of the returned {@link Buffer} is transferred
   * to the caller who is responsible for calling {@link Buffer#close}.
   *
   * <p>The length of the returned {@link Buffer} may be less than {@code maxLength}, but will never
   * be 0. If no data is available, {@code null} is returned. To ensure that all available data is
   * read, the caller should repeatedly call {@link #readBytes} until it returns {@code null}.
   *
   * @param maxLength the maximum number of bytes to read. This value must be > 0, otherwise throws
   *        an {@link IllegalArgumentException}.
   * @return a {@link Buffer} containing the number of bytes read or {@code null} if no data is
   *         currently available.
   */
  @Nullable
  Buffer readBytes(int maxLength);

  /**
   * Closes this decompressor and frees any resources.
   */
  @Override
  void close();
}
