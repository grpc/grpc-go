/*
 * Copyright 2015, Google Inc. All rights reserved.
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

/**
 * An interface for a byte buffer that can only be written to.
 * {@link WritableBuffer}s are a generic way to transfer bytes to
 * the concrete network transports, like Netty and OkHttp.
 */
public interface WritableBuffer {

  /**
   * Appends {@code length} bytes to the buffer from the source
   * array starting at {@code srcIndex}.
   *
   * @throws IndexOutOfBoundsException
   *         if the specified {@code srcIndex} is less than {@code 0},
   *         if {@code srcIndex + length} is greater than
   *            {@code src.length}, or
   *         if {@code length} is greater than {@link #writableBytes()}
   */
  void write(byte[] src, int srcIndex, int length);

  /**
   * Returns the number of bytes one can write to the buffer.
   */
  int writableBytes();

  /**
   * Returns the number of bytes one can read from the buffer.
   */
  int readableBytes();

  /**
   * Releases the buffer, indicating to the {@link WritableBufferAllocator} that
   * this buffer is no longer used and its resources can be reused.
   */
  void release();
}
