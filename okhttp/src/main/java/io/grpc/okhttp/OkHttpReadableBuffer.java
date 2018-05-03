/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.okhttp;

import io.grpc.internal.AbstractReadableBuffer;
import io.grpc.internal.ReadableBuffer;
import java.io.EOFException;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;

/**
 * A {@link ReadableBuffer} implementation that is backed by an {@link okio.Buffer}.
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
    while (length > 0) {
      int bytesRead = buffer.read(dest, destOffset, length);
      if (bytesRead == -1) {
        throw new IndexOutOfBoundsException("EOF trying to read " + length + " bytes");
      }
      length -= bytesRead;
      destOffset += bytesRead;
    }
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
