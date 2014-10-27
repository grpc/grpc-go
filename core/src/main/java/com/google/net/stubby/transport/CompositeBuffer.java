package com.google.net.stubby.transport;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.util.ArrayDeque;
import java.util.Queue;

/**
 * A {@link Buffer} that is composed of 0 or more {@link Buffer}s. This provides a facade that
 * allows multiple buffers to be treated as one.
 *
 * <p>When a buffer is added to a composite, it's life cycle is controlled by the composite. Once
 * the composite has read past the end of a given buffer, that buffer is automatically closed and
 * removed from the composite.
 */
public class CompositeBuffer extends AbstractBuffer {

  private int readableBytes;
  private final Queue<Buffer> buffers = new ArrayDeque<Buffer>();

  /**
   * Adds a new {@link Buffer} at the end of the buffer list. After a buffer is added, it is
   * expected that this {@link CompositeBuffer} has complete ownership. Any attempt to modify the
   * buffer (i.e. modifying the readable bytes) may result in corruption of the internal state of
   * this {@link CompositeBuffer}.
   */
  public void addBuffer(Buffer buffer) {
    if (!(buffer instanceof CompositeBuffer)) {
      buffers.add(buffer);
      readableBytes += buffer.readableBytes();
      return;
    }

    CompositeBuffer compositeBuffer = (CompositeBuffer) buffer;
    while (!compositeBuffer.buffers.isEmpty()) {
      Buffer subBuffer = compositeBuffer.buffers.remove();
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
      int readInternal(Buffer buffer, int length) {
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
      public int readInternal(Buffer buffer, int length) {
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
      public int readInternal(Buffer buffer, int length) {
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
      public int readInternal(Buffer buffer, int length) {
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
      public int readInternal(Buffer buffer, int length) throws IOException {
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
  public CompositeBuffer readBytes(int length) {
    checkReadable(length);
    readableBytes -= length;

    CompositeBuffer newBuffer = new CompositeBuffer();
    while (length > 0) {
      Buffer buffer = buffers.peek();
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
   * Executes the given {@link ReadOperation} against the {@link Buffer}s required to satisfy the
   * requested {@code length}.
   */
  private void execute(ReadOperation op, int length) {
    checkReadable(length);

    for (; length > 0 && !buffers.isEmpty(); advanceBufferIfNecessary()) {
      Buffer buffer = buffers.peek();
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
    Buffer buffer = buffers.peek();
    if (buffer.readableBytes() == 0) {
      buffers.remove().close();
    }
  }

  /**
   * A simple read operation to perform on a single {@link Buffer}. All state management for the
   * buffers is done by {@link CompositeBuffer#execute(ReadOperation, int)}.
   */
  private abstract class ReadOperation {
    /**
     * Only used by {@link CompositeBuffer#readUnsignedByte()}.
     */
    int value;

    /**
     * Only used by {@link CompositeBuffer#readBytes(OutputStream, int)};
     */
    IOException ex;

    final void read(Buffer buffer, int length) {
      try {
        value = readInternal(buffer, length);
      } catch (IOException e) {
        ex = e;
      }
    }

    final boolean isError() {
      return ex != null;
    }

    abstract int readInternal(Buffer buffer, int length) throws IOException;
  }
}
