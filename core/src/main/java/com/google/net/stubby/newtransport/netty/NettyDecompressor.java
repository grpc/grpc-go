package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.TransportFrameUtil.COMPRESSION_HEADER_LENGTH;
import static com.google.net.stubby.newtransport.TransportFrameUtil.isFlateCompressed;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.io.Closeables;
import com.google.net.stubby.newtransport.Buffer;
import com.google.net.stubby.newtransport.Decompressor;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.buffer.ByteBufOutputStream;
import io.netty.buffer.CompositeByteBuf;

import java.io.Closeable;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.util.zip.InflaterInputStream;

import javax.annotation.Nullable;

/**
 * A {@link Decompressor} implementation based on Netty {@link CompositeByteBuf}s.
 */
public class NettyDecompressor implements Decompressor {

  private final CompositeByteBuf buffer;
  private final ByteBufAllocator alloc;
  private Frame frame;

  public NettyDecompressor(ByteBufAllocator alloc) {
    this.alloc = Preconditions.checkNotNull(alloc, "alloc");
    buffer = alloc.compositeBuffer();
  }

  @Override
  public void decompress(Buffer data) {
    ByteBuf buf = toByteBuf(data);

    // Add it to the compression frame buffer.
    buffer.addComponent(buf);
    buffer.writerIndex(buffer.writerIndex() + buf.readableBytes());
  }

  @Override
  public Buffer readBytes(final int maxLength) {
    Preconditions.checkArgument(maxLength > 0, "maxLength must be > 0");
    try {
      // Read the next frame if we don't already have one.
      if (frame == null) {
        frame = nextFrame();
      }

      ByteBuf byteBuf = null;
      if (frame != null) {
        // Read as many bytes as we can from the frame.
        byteBuf = frame.readBytes(maxLength);

        // If we reached the end of the frame, close it.
        if (frame.complete()) {
          frame.close();
          frame = null;
        }
      }

      if (byteBuf == null) {
        // No data was available.
        return null;
      }

      return new NettyBuffer(byteBuf);
    } finally {
      // Discard any component buffers that have been fully read.
      buffer.discardReadComponents();
    }
  }

  @Override
  public void close() {
    // Release the CompositeByteBuf. This will automatically release any components as well.
    buffer.release();
    if (frame != null) {
      frame.close();
    }
  }

  /**
   * Returns the next compression frame object, or {@code null} if the next frame header is
   * incomplete.
   */
  @SuppressWarnings("resource")
  @Nullable
  private Frame nextFrame() {
    if (buffer.readableBytes() < COMPRESSION_HEADER_LENGTH) {
      // Don't have all the required bytes for the frame header yet.
      return null;
    }

    // Read the header and create the frame object.
    boolean compressed = isFlateCompressed(buffer.readUnsignedByte());
    int frameLength = buffer.readUnsignedMedium();
    if (frameLength == 0) {
      return nextFrame();
    }

    return compressed ? new CompressedFrame(frameLength) : new UncompressedFrame(frameLength);
  }

  /**
   * Converts the given buffer into a {@link ByteBuf}.
   */
  private ByteBuf toByteBuf(Buffer data) {
    if (data instanceof NettyBuffer) {
      // Just return the contained ByteBuf.
      return ((NettyBuffer) data).buffer();
    }

    // Create a new ByteBuf and copy the content to it.
    try {
      int length = data.readableBytes();
      ByteBuf buf = alloc.buffer(length);
      data.readBytes(new ByteBufOutputStream(buf), length);
      return buf;
    } catch (IOException e) {
      throw Throwables.propagate(e);
    } finally {
      data.close();
    }
  }

  /**
   * A wrapper around the body of a compression frame. Provides a generic method for reading bytes
   * from any frame.
   */
  private interface Frame extends Closeable {
    @Nullable
    ByteBuf readBytes(int maxLength);

    boolean complete();

    @Override
    void close();
  }

  /**
   * An uncompressed frame. Just writes bytes directly from the compression frame.
   */
  private class UncompressedFrame implements Frame {
    int bytesRemainingInFrame;

    public UncompressedFrame(int frameLength) {
      this.bytesRemainingInFrame = frameLength;
    }

    @Override
    @Nullable
    public ByteBuf readBytes(int maxLength) {
      Preconditions.checkState(!complete(), "Must not call readBytes on a completed frame");
      int available = buffer.readableBytes();
      if (available == 0) {
        return null;
      }

      int bytesToRead = Math.min(available, Math.min(maxLength, bytesRemainingInFrame));
      bytesRemainingInFrame -= bytesToRead;

      return buffer.readBytes(bytesToRead);
    }

    @Override
    public boolean complete() {
      return bytesRemainingInFrame == 0;
    }

    @Override
    public void close() {
      // Do nothing.
    }
  }

  /**
   * A compressed frame that inflates the data as it reads from the frame.
   */
  private class CompressedFrame implements Frame {
    private final InputStream in;
    private ByteBuf nextBuf;

    public CompressedFrame(int frameLength) {
      // Limit the stream by the frameLength.
      in = new InflaterInputStream(new GrowableByteBufInputStream(frameLength));
    }

    @Override
    @Nullable
    public ByteBuf readBytes(int maxLength) {

      // If the pre-existing nextBuf is too small, release it.
      if (nextBuf != null && nextBuf.capacity() < maxLength) {
        nextBuf.release();
        nextBuf = null;
      }

      if (nextBuf == null) {
        nextBuf = alloc.buffer();
      }

      try {
        int bytesToWrite = Math.min(maxLength, nextBuf.writableBytes());
        nextBuf.writeBytes(in, bytesToWrite);
      } catch (EOFException e) {
        // The next compressed block is unavailable at the moment. Nothing to return.
        return null;
      } catch (IOException e) {
        throw new RuntimeException(e);
      }

      if (!nextBuf.isReadable()) {
        throw new AssertionError("Read zero bytes from the compression frame");
      }

      ByteBuf ret = nextBuf;
      nextBuf = null;
      return ret;
    }

    @Override
    public boolean complete() {
      try {
        return in.available() <= 0;
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }

    @Override
    public void close() {
      Closeables.closeQuietly(in);
    }
  }

  /**
   * A stream backed by the {@link #buffer}, which allows for additional reading as the buffer
   * grows. Not using Netty's stream class since it doesn't handle growth of the underlying buffer.
   */
  private class GrowableByteBufInputStream extends InputStream {
    final int startIndex;
    final int endIndex;

    GrowableByteBufInputStream(int length) {
      startIndex = buffer.readerIndex();
      endIndex = startIndex + length;
    }

    @Override
    public int read() throws IOException {
      if (available() == 0) {
        return -1;
      }
      return buffer.readByte() & 0xff;
    }

    @Override
    public int read(byte[] b, int off, int len) throws IOException {
      int available = available();
      if (available == 0) {
        return -1;
      }

      len = Math.min(available, len);
      buffer.readBytes(b, off, len);
      return len;
    }

    @Override
    public int available() throws IOException {
      return Math.min(endIndex - buffer.readerIndex(), buffer.readableBytes());
    }
  }
}
