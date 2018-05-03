/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.cronet;

import com.google.common.base.Preconditions;
import io.grpc.internal.WritableBuffer;
import java.nio.ByteBuffer;

class CronetWritableBuffer implements WritableBuffer {
  private final ByteBuffer buffer;

  public CronetWritableBuffer(ByteBuffer buffer, int capacity) {
    this.buffer = Preconditions.checkNotNull(buffer, "buffer");
  }

  @Override
  public void write(byte[] src, int srcIndex, int length) {
    buffer.put(src, srcIndex, length);
  }

  @Override
  public void write(byte b) {
    buffer.put(b);
  }

  @Override
  public int writableBytes() {
    return buffer.remaining();
  }

  @Override
  public int readableBytes() {
    return buffer.position();
  }

  @Override
  public void release() {
  }

  ByteBuffer buffer() {
    return buffer;
  }
}
