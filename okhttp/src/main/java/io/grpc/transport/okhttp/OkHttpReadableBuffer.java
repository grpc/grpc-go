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

package io.grpc.transport.okhttp;

import io.grpc.transport.AbstractReadableBuffer;
import io.grpc.transport.ReadableBuffer;

import java.io.EOFException;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;

/**
 * A {@link io.grpc.transport.ReadableBuffer} implementation that is backed by an {@link okio.Buffer}.
 */
class OkHttpReadableBuffer extends AbstractReadableBuffer {
  private final okio.Buffer buffer;

  OkHttpReadableBuffer(okio.Buffer buffer) {
    this.buffer = buffer;
  }

  @Override
  public int readableBytes() {
    return (int) buffer.size();
  }

  @Override
  public int readUnsignedByte() {
    return buffer.readByte() & 0x000000FF;
  }

  @Override
  public void skipBytes(int length) {
    try {
      buffer.skip(length);
    } catch (EOFException e) {
      throw new IndexOutOfBoundsException(e.getMessage());
    }
  }

  @Override
  public void readBytes(byte[] dest, int destOffset, int length) {
    buffer.read(dest, destOffset, length);
  }

  @Override
  public void readBytes(ByteBuffer dest) {
    // We are not using it.
    throw new UnsupportedOperationException();
  }

  @Override
  public void readBytes(OutputStream dest, int length) throws IOException {
    buffer.writeTo(dest, length);
  }

  @Override
  public ReadableBuffer readBytes(int length) {
    okio.Buffer buf = new okio.Buffer();
    buf.write(buffer, length);
    return new OkHttpReadableBuffer(buf);
  }

  @Override
  public void close() {
    buffer.clear();
  }
}
