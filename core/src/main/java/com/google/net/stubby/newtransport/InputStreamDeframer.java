package com.google.net.stubby.newtransport;

import com.google.common.io.ByteStreams;

import java.io.DataInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.zip.InflaterInputStream;

/**
 * Deframer that expects the input frames to be provided as {@link InputStream} instances
 * which accurately report their size using {@link java.io.InputStream#available()}.
 */
public class InputStreamDeframer extends Deframer<InputStream> {

  private final PrefixingInputStream prefixingInputStream;

  public InputStreamDeframer(GrpcDeframer.Sink target) {
    super(target);
    prefixingInputStream = new PrefixingInputStream(4096);
  }

  /**
   * Deframing a single input stream that contains multiple GRPC frames
   */
  @Override
  public void deliverFrame(InputStream frame, boolean endOfStream) {
    super.deliverFrame(frame, endOfStream);
    try {
      if (frame.available() > 0) {
        throw new AssertionError();
      }
    } catch (IOException ex) {
      throw new RuntimeException(ex);
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
        return;
      }
      int bufferLength = buffer == null ? 0 : buffer.length;
      int bytesInBuffer = bufferLength - bufferIndex;
      // Shift existing bytes
      if (bufferLength < bytesInBuffer + remainingSuffix) {
        // Buffer too small, so create a new buffer before copying in the suffix
        byte[] newBuffer = new byte[bytesInBuffer + remainingSuffix];
        if (bytesInBuffer > 0) {
          System.arraycopy(buffer, bufferIndex, newBuffer, 0, bytesInBuffer);
        }
        buffer = newBuffer;
        bufferIndex = 0;
      } else {
        // Enough space is in buffer, so shift the existing bytes to open up exactly enough bytes
        // for the suffix at the end.
        System.arraycopy(buffer, bufferIndex, buffer, bufferIndex - remainingSuffix, bytesInBuffer);
        bufferIndex -= remainingSuffix;
      }
      // Write suffix to buffer
      ByteStreams.readFully(suffix, buffer, buffer.length - remainingSuffix, remainingSuffix);
      suffix = null;
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
        // FIXME(ejona): This is likely broken with compressed streams.
        available += suffix.available();
      }
      return available;
    }
  }
}
