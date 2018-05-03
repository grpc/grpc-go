/*
 * Copyright 2015 The gRPC Authors
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

import io.grpc.internal.WritableBuffer;
import okio.Buffer;

class OkHttpWritableBuffer implements WritableBuffer {

  private final Buffer buffer;
  private int writableBytes;
  private int readableBytes;

  OkHttpWritableBuffer(Buffer buffer, int capacity) {
    this.buffer = buffer;
    writableBytes = capacity;
  }

  @Override
  public void write(byte[] src, int srcIndex, int length) {
    buffer.write(src, srcIndex, length);
    writableBytes -= length;
    readableBytes += length;
  }

  @Override
  public void write(byte b) {
    buffer.writeByte(b);
    writableBytes -= 1;
    readableBytes += 1;
  }

  @Override
  public int writableBytes() {
    return writableBytes;
  }

  @Override
  public int readableBytes() {
    return readableBytes;
  }

  @Override
  public void release() {
  }

  Buffer buffer() {
    return buffer;
  }
}
