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

package io.grpc.internal;

/**
 * Abstract base class for {@link ReadableBuffer} implementations.
 */
public abstract class AbstractReadableBuffer implements ReadableBuffer {
  @Override
  public final int readInt() {
    checkReadable(4);
    int b1 = readUnsignedByte();
    int b2 = readUnsignedByte();
    int b3 = readUnsignedByte();
    int b4 = readUnsignedByte();
    return (b1 << 24) | (b2 << 16) | (b3 << 8) | b4;
  }

  @Override
  public boolean hasArray() {
    return false;
  }

  @Override
  public byte[] array() {
    throw new UnsupportedOperationException();
  }

  @Override
  public int arrayOffset() {
    throw new UnsupportedOperationException();
  }

  @Override
  public void close() {}

  protected final void checkReadable(int length) {
    if (readableBytes() < length) {
      throw new IndexOutOfBoundsException();
    }
  }
}
