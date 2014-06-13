package com.google.net.stubby.transport;

import com.google.common.io.ByteStreams;
import com.google.net.stubby.Operation;

import java.io.DataInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.zip.InflaterInputStream;

/**
 * Deframer that expects the input frames to be provided as {@link InputStream} instances
 * which accurately report their size using {@link java.io.InputStream#available()}.
 */
public class InputStreamDeframer extends Deframer<InputStream> {

  private final InputStreamDeframer.PrefixingInputStream prefixingInputStream;

  public InputStreamDeframer() {
    prefixingInputStream = new PrefixingInputStream(4096);
  }

  /**
   * Deframing a single input stream that contains multiple GRPC frames
   *
   * @return the number of unconsumed bytes remaining in the buffer
   */
  @Override
  public int deframe(InputStream frame, Operation target) {
    try {
      int remaining;
      do {
        remaining = super.deframe(frame, target);
      } while (frame.available() > 0);
      return remaining;
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }

  @Override
  protected DataInputStream prefix(InputStream frame) throws IOException {
    prefixingInputStream.consolidate();
    prefixingInputStream.prefix(frame);
    return new DataInputStream(prefixingInputStream);
  }

  @Override
  protected int consolidate() throws IOException {
    prefixingInputStream.consolidate();
    return prefixingInputStream.available();
  }

  @Override
  protected InputStream decompress(InputStream frame) throws IOException {
    int compressionType = frame.read();
    int frameLength =  frame.read() << 16 | frame.read() << 8 | frame.read();
    InputStream raw = ByteStreams.limit(frame, frameLength);
    if (TransportFrameUtil.isNotCompressed(compressionType)) {
      return raw;
    } else if (TransportFrameUtil.isFlateCompressed(compressionType)) {
      return new InflaterInputStream(raw);
    }
    throw new IOException("Unknown compression type " + compressionType);
  }

  /**
   * InputStream that prefixes another input stream with a fixed buffer.
   */
  private class PrefixingInputStream extends InputStream {

    private InputStream suffix;
    private byte[] buffer;
    private int bufferIndex;
    private int maxRetainedBuffer;

    private PrefixingInputStream(int maxRetainedBuffer) {
      // TODO(user): Implement support for this.
      this.maxRetainedBuffer = maxRetainedBuffer;
    }

    void prefix(InputStream suffix) {
      this.suffix = suffix;
    }

    void consolidate() throws IOException {
      int remainingSuffix = suffix == null ? 0 : suffix.available();
      if (remainingSuffix == 0) {
        // No suffix so clear
        suffix = null;
      } else if (buffer == null || remainingSuffix > buffer.length - bufferIndex) {
        // Suffix exceeds current buffer size
        buffer = ByteStreams.toByteArray(suffix);
        bufferIndex = 0;
      } else if (buffer.length == bufferIndex) {
        // Buffer has been fully consumed, copy suffix into it
        ByteStreams.readFully(suffix, buffer, buffer.length - remainingSuffix, remainingSuffix);
        bufferIndex = buffer.length - remainingSuffix;
      } else {
        // Buffer has been partially consumed so shift the buffer before copying in the suffix
        System.arraycopy(buffer, bufferIndex, buffer, bufferIndex - remainingSuffix,
            buffer.length - bufferIndex);
        ByteStreams.readFully(suffix, buffer, buffer.length - remainingSuffix, remainingSuffix);
        bufferIndex -= remainingSuffix;
      }
    }

    @Override
    public int read(byte[] b, int off, int len) throws IOException {
      int read = readFromBuffer(b, off, len);
      if (suffix != null) {
        read += suffix.read(b, off + read, len - read);
      }
      return read;
    }

    private int readFromBuffer(byte[] b, int off, int len) {
      if (buffer == null) {
        return 0;
      }
      len = Math.min(buffer.length - bufferIndex, len);
      System.arraycopy(buffer, bufferIndex, b, off, len);
      bufferIndex += len;
      return len;
    }

    @Override
    public int read() throws IOException {
      if (buffer == null || bufferIndex == buffer.length) {
        return suffix == null ? -1 : suffix.read();
      }
      return buffer[bufferIndex++];
    }

    @Override
    public int available() throws IOException {
      int available = buffer != null ? buffer.length - bufferIndex : 0;
      if (suffix != null) {
        available += suffix.available();
      }
      return available;
    }
  }
}
