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
