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

import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.util.ArrayDeque;
import java.util.Queue;

/**
 * A {@link ReadableBuffer} that is composed of 0 or more {@link ReadableBuffer}s. This provides a
 * facade that allows multiple buffers to be treated as one.
 *
 * <p>When a buffer is added to a composite, its life cycle is controlled by the composite. Once
 * the composite has read past the end of a given buffer, that buffer is automatically closed and
 * removed from the composite.
 */
public class CompositeReadableBuffer extends AbstractReadableBuffer {

  private int readableBytes;
  private final Queue<ReadableBuffer> buffers = new ArrayDeque<>();

  /**
   * Adds a new {@link ReadableBuffer} at the end of the buffer list. After a buffer is added, it is
   * expected that this {@code CompositeBuffer} has complete ownership. Any attempt to modify the
   * buffer (i.e. modifying the readable bytes) may result in corruption of the internal state of
   * this {@code CompositeBuffer}.
   */
  public void addBuffer(ReadableBuffer buffer) {
    if (!(buffer instanceof CompositeReadableBuffer)) {
      buffers.add(buffer);
      readableBytes += buffer.readableBytes();
      return;
    }

    CompositeReadableBuffer compositeBuffer = (CompositeReadableBuffer) buffer;
    while (!compositeBuffer.buffers.isEmpty()) {
      ReadableBuffer subBuffer = compositeBuffer.buffers.remove();
      buffers.add(subBuffer);
    }
    readableBytes += compositeBuffer.readableBytes;
    compositeBuffer.readableBytes = 0;
    compositeBuffer.close();
  }

  @Override
  public int readableBytes() {
    return readableBytes;
  }

  @Override
  public int readUnsignedByte() {
    ReadOperation op = new ReadOperation() {
      @Override
      int readInternal(ReadableBuffer buffer, int length) {
        return buffer.readUnsignedByte();
      }
    };
    execute(op, 1);
    return op.value;
  }

  @Override
  public void skipBytes(int length) {
    execute(new ReadOperation() {
      @Override
      public int readInternal(ReadableBuffer buffer, int length) {
        buffer.skipBytes(length);
        return 0;
      }
    }, length);
  }

  @Override
  public void readBytes(final byte[] dest, final int destOffset, int length) {
    execute(new ReadOperation() {
      int currentOffset = destOffset;
      @Override
      public int readInternal(ReadableBuffer buffer, int length) {
        buffer.readBytes(dest, currentOffset, length);
        currentOffset += length;
        return 0;
      }
    }, length);
  }

  @Override
  public void readBytes(final ByteBuffer dest) {
    execute(new ReadOperation() {
      @Override
      public int readInternal(ReadableBuffer buffer, int length) {
        // Change the limit so that only lengthToCopy bytes are available.
        int prevLimit = dest.limit();
        dest.limit(dest.position() + length);

        // Write the bytes and restore the original limit.
        buffer.readBytes(dest);
        dest.limit(prevLimit);
        return 0;
      }
    }, dest.remaining());
  }

  @Override
  public void readBytes(final OutputStream dest, int length) throws IOException {
    ReadOperation op = new ReadOperation() {
      @Override
      public int readInternal(ReadableBuffer buffer, int length) throws IOException {
        buffer.readBytes(dest, length);
        return 0;
      }
    };
    execute(op, length);

    // If an exception occurred, throw it.
    if (op.isError()) {
      throw op.ex;
    }
  }

  @Override
  public CompositeReadableBuffer readBytes(int length) {
    checkReadable(length);
    readableBytes -= length;

    CompositeReadableBuffer newBuffer = new CompositeReadableBuffer();
    while (length > 0) {
      ReadableBuffer buffer = buffers.peek();
      if (buffer.readableBytes() > length) {
        newBuffer.addBuffer(buffer.readBytes(length));
        length = 0;
      } else {
        newBuffer.addBuffer(buffers.poll());
        length -= buffer.readableBytes();
      }
    }
    return newBuffer;
  }

  @Override
  public void close() {
    while (!buffers.isEmpty()) {
      buffers.remove().close();
    }
  }

  /**
   * Executes the given {@link ReadOperation} against the {@link ReadableBuffer}s required to
   * satisfy the requested {@code length}.
   */
  private void execute(ReadOperation op, int length) {
    checkReadable(length);

    if (!buffers.isEmpty()) {
      advanceBufferIfNecessary();
    }

    for (; length > 0 && !buffers.isEmpty(); advanceBufferIfNecessary()) {
      ReadableBuffer buffer = buffers.peek();
      int lengthToCopy = Math.min(length, buffer.readableBytes());

      // Perform the read operation for this buffer.
      op.read(buffer, lengthToCopy);
      if (op.isError()) {
        return;
      }

      length -= lengthToCopy;
      readableBytes -= lengthToCopy;
    }

    if (length > 0) {
      // Should never get here.
      throw new AssertionError("Failed executing read operation");
    }
  }

  /**
   * If the current buffer is exhausted, removes and closes it.
   */
  private void advanceBufferIfNecessary() {
    ReadableBuffer buffer = buffers.peek();
    if (buffer.readableBytes() == 0) {
      buffers.remove().close();
    }
  }

  /**
   * A simple read operation to perform on a single {@link ReadableBuffer}. All state management for
   * the buffers is done by {@link CompositeReadableBuffer#execute(ReadOperation, int)}.
   */
  private abstract static class ReadOperation {
    /**
     * Only used by {@link CompositeReadableBuffer#readUnsignedByte()}.
     */
    int value;

    /**
     * Only used by {@link CompositeReadableBuffer#readBytes(OutputStream, int)}.
     */
    IOException ex;

    final void read(ReadableBuffer buffer, int length) {
      try {
        value = readInternal(buffer, length);
      } catch (IOException e) {
        ex = e;
      }
    }

    final boolean isError() {
      return ex != null;
    }

    abstract int readInternal(ReadableBuffer buffer, int length) throws IOException;
  }
}
