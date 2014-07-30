package com.google.net.stubby.newtransport;

import static java.nio.charset.StandardCharsets.UTF_8;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.io.ByteStreams;
import com.google.net.stubby.DeferredProtoInputStream;
import com.google.protobuf.ByteString;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.nio.charset.Charset;

/**
 * Utility methods for creating {@link Buffer} instances.
 */
public final class Buffers {
  private static final Buffer EMPTY_BUFFER = new ByteArrayWrapper(new byte[0]);

  /**
   * Returns an empty {@link Buffer} instance.
   */
  public static Buffer empty() {
    return EMPTY_BUFFER;
  }

  /**
   * Creates a new {@link Buffer} that is backed by the given {@link ByteString}.
   */
  public static Buffer wrap(ByteString bytes) {
    return new ByteStringWrapper(bytes);
  }

  /**
   * Shortcut for {@code wrap(bytes, 0, bytes.length}.
   */
  public static Buffer wrap(byte[] bytes) {
    return new ByteArrayWrapper(bytes, 0, bytes.length);
  }

  /**
   * Creates a new {@link Buffer} that is backed by the given byte array.
   *
   * @param bytes the byte array being wrapped.
   * @param offset the starting offset for the buffer within the byte array.
   * @param length the length of the buffer from the {@code offset} index.
   */
  public static Buffer wrap(byte[] bytes, int offset, int length) {
    return new ByteArrayWrapper(bytes, offset, length);
  }

  /**
   * Creates a new {@link Buffer} that is backed by the given {@link ByteBuffer}. Calls to read from
   * the buffer will increment the position of the {@link ByteBuffer}.
   */
  public static Buffer wrap(ByteBuffer bytes) {
    return new ByteBufferWrapper(bytes);
  }

  /**
   * Reads an entire {@link Buffer} to a new array. After calling this method, the buffer will
   * contain no readable bytes.
   */
  public static byte[] readArray(Buffer buffer) {
    Preconditions.checkNotNull(buffer, "buffer");
    int length = buffer.readableBytes();
    byte[] bytes = new byte[length];
    buffer.readBytes(bytes, 0, length);
    return bytes;
  }

  /**
   * Creates a new {@link Buffer} that contains the content from the given {@link InputStream}.
   *
   * @param in the source of the bytes.
   * @param length the number of bytes to be read from the stream.
   */
  public static Buffer copyFrom(InputStream in, int length) {
    Preconditions.checkNotNull(in, "in");
    Preconditions.checkArgument(length >= 0, "length must be positive");

    if (in instanceof DeferredProtoInputStream) {
      ByteString bstr = ((DeferredProtoInputStream) in).getMessage().toByteString();
      return new ByteStringWrapper(bstr.substring(0, length));
    }

    byte[] bytes = new byte[length];
    try {
      ByteStreams.readFully(in, bytes);
    } catch (IOException e) {
      Throwables.propagate(e);
    }
    return new ByteArrayWrapper(bytes, 0, length);
  }

  /**
   * Reads the entire {@link Buffer} to a new {@link String} with the given charset.
   */
  public static String readAsString(Buffer buffer, Charset charset) {
    Preconditions.checkNotNull(charset, "charset");
    byte[] bytes = readArray(buffer);
    return new String(bytes, charset);
  }

  /**
   * Reads the entire {@link Buffer} to a new {@link String} using UTF-8 decoding.
   */
  public static String readAsStringUtf8(Buffer buffer) {
    return readAsString(buffer, UTF_8);
  }

  /**
   * Creates a new {@link InputStream} backed by the given buffer. Any read taken on the stream will
   * automatically increment the read position of this buffer. Closing the stream, however, does not
   * affect the original buffer.
   *
   * @param buffer the buffer backing the new {@link InputStream}.
   * @param owner if {@code true}, the returned stream will close the buffer when closed.
   */
  public static InputStream openStream(Buffer buffer, boolean owner) {
    return new BufferInputStream(owner ? buffer : ignoreClose(buffer));
  }

  /**
   * Decorates the given {@link Buffer} to ignore calls to {@link Buffer#close}.
   *
   * @param buffer the buffer to be decorated.
   * @return a wrapper around {@code buffer} that ignores calls to {@link Buffer#close}.
   */
  public static Buffer ignoreClose(Buffer buffer) {
    return new ForwardingBuffer(buffer) {
      @Override
      public void close() {
        // Ignore.
      }
    };
  }

  /**
   * A {@link Buffer} that is backed by a byte array.
   */
  private static class ByteArrayWrapper extends AbstractBuffer {
    int offset;
    final int end;
    final byte[] bytes;

    ByteArrayWrapper(byte[] bytes) {
      this(bytes, 0, bytes.length);
    }

    ByteArrayWrapper(byte[] bytes, int offset, int length) {
      Preconditions.checkArgument(offset >= 0, "offset must be >= 0");
      Preconditions.checkArgument(length >= 0, "length must be >= 0");
      Preconditions.checkArgument(offset + length <= bytes.length,
          "offset + length exceeds array boundary");
      this.bytes = Preconditions.checkNotNull(bytes, "bytes");
      this.offset = offset;
      this.end = offset + length;
    }

    @Override
    public int readableBytes() {
      return end - offset;
    }

    @Override
    public void skipBytes(int length) {
      checkReadable(length);
      offset += length;
    }

    @Override
    public int readUnsignedByte() {
      checkReadable(1);
      return bytes[offset++] & 0xFF;
    }

    @Override
    public void readBytes(byte[] dest, int destIndex, int length) {
      System.arraycopy(bytes, offset, dest, destIndex, length);
      offset += length;
    }

    @Override
    public void readBytes(ByteBuffer dest) {
      Preconditions.checkNotNull(dest, "dest");
      int length = dest.remaining();
      checkReadable(length);
      dest.put(bytes, offset, length);
      offset += length;
    }

    @Override
    public void readBytes(OutputStream dest, int length) throws IOException {
      checkReadable(length);
      dest.write(bytes, offset, length);
      offset += length;
    }

    @Override
    public boolean hasArray() {
      return true;
    }

    @Override
    public byte[] array() {
      return bytes;
    }

    @Override
    public int arrayOffset() {
      return offset;
    }
  }

  /**
   * A {@link Buffer} that is backed by a {@link ByteString}.
   */
  private static class ByteStringWrapper extends AbstractBuffer {
    int offset;
    final ByteString bytes;

    ByteStringWrapper(ByteString bytes) {
      this.bytes = Preconditions.checkNotNull(bytes, "bytes");
    }

    @Override
    public int readableBytes() {
      return bytes.size() - offset;
    }

    @Override
    public int readUnsignedByte() {
      checkReadable(1);
      return bytes.byteAt(offset++) & 0xFF;
    }

    @Override
    public void skipBytes(int length) {
      checkReadable(length);
      offset += length;
    }

    @Override
    public void readBytes(byte[] dest, int destOffset, int length) {
      bytes.copyTo(dest, offset, destOffset, length);
      offset += length;
    }

    @Override
    public void readBytes(ByteBuffer dest) {
      Preconditions.checkNotNull(dest, "dest");
      int length = dest.remaining();
      checkReadable(length);
      internalSlice(length).copyTo(dest);
      offset += length;
    }

    @Override
    public void readBytes(OutputStream dest, int length) throws IOException {
      checkReadable(length);
      internalSlice(length).writeTo(dest);
      offset += length;
    }

    private ByteString internalSlice(int length) {
      return bytes.substring(offset, offset + length);
    }
  }

  /**
   * A {@link Buffer} that is backed by a {@link ByteBuffer}.
   */
  private static class ByteBufferWrapper extends AbstractBuffer {
    final ByteBuffer bytes;

    ByteBufferWrapper(ByteBuffer bytes) {
      this.bytes = Preconditions.checkNotNull(bytes, "bytes");
    }

    @Override
    public int readableBytes() {
      return bytes.remaining();
    }

    @Override
    public int readUnsignedByte() {
      checkReadable(1);
      return bytes.get() & 0xFF;
    }

    @Override
    public void skipBytes(int length) {
      checkReadable(length);
      bytes.position(bytes.position() + length);
    }

    @Override
    public void readBytes(byte[] dest, int destOffset, int length) {
      checkReadable(length);
      bytes.get(dest, destOffset, length);
    }

    @Override
    public void readBytes(ByteBuffer dest) {
      Preconditions.checkNotNull(dest, "dest");
      int length = dest.remaining();
      checkReadable(length);

      // Change the limit so that only length bytes are available.
      int prevLimit = bytes.limit();
      bytes.limit(bytes.position() + length);

      // Write the bytes and restore the original limit.
      dest.put(bytes);
      bytes.limit(prevLimit);
    }

    @Override
    public void readBytes(OutputStream dest, int length) throws IOException {
      checkReadable(length);
      if (hasArray()) {
        dest.write(array(), arrayOffset(), length);
        bytes.position(bytes.position() + length);
      } else {
        // The buffer doesn't support array(). Copy the data to an intermediate buffer.
        byte[] array = new byte[length];
        bytes.get(array);
        dest.write(array);
      }
    }

    @Override
    public boolean hasArray() {
      return bytes.hasArray();
    }

    @Override
    public byte[] array() {
      return bytes.array();
    }

    @Override
    public int arrayOffset() {
      return bytes.arrayOffset() + bytes.position();
    }
  }

  /**
   * An {@link InputStream} that is backed by a {@link Buffer}.
   */
  private static class BufferInputStream extends InputStream {
    final Buffer buffer;

    public BufferInputStream(Buffer buffer) {
      this.buffer = Preconditions.checkNotNull(buffer, "buffer");
    }

    @Override
    public int available() throws IOException {
      return buffer.readableBytes();
    }

    @Override
    public int read() {
      if (buffer.readableBytes() == 0) {
        // EOF.
        return -1;
      }
      return buffer.readUnsignedByte();
    }

    @Override
    public int read(byte[] dest, int destOffset, int length) throws IOException {
      if (buffer.readableBytes() == 0) {
        // EOF.
        return -1;
      }

      length = Math.min(buffer.readableBytes(), length);
      buffer.readBytes(dest, destOffset, length);
      return length;
    }
  }

  private Buffers() {}
}
