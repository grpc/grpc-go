package com.google.net.stubby.transport;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.io.ByteStreams;
import com.google.net.stubby.DeferredInputStream;
import com.google.net.stubby.transport.Framer.Sink;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.util.logging.Level;
import java.util.logging.Logger;
import java.util.zip.Deflater;

/**
 * Compression framer for HTTP/2 transport frames, for use in both compression and
 * non-compression scenarios. Receives message-stream as input. It is able to change compression
 * configuration on-the-fly, but will not actually begin using the new configuration until the next
 * full frame.
 */
class CompressionFramer {
  /**
   * Compression level to indicate using this class's default level. Note that this value is
   * allowed to conflict with Deflate.DEFAULT_COMPRESSION, in which case this class's default
   * prevails.
   */
  public static final int DEFAULT_COMPRESSION_LEVEL = -1;
  private static final byte[] EMPTY_BYTE_ARRAY = new byte[0];
  /**
   * Size of the GRPC compression frame header which consists of:
   * 1 byte for the compression type,
   * 3 bytes for the length of the compression frame.
   */
  @VisibleForTesting
  static final int HEADER_LENGTH = 4;
  /**
   * Number of frame bytes to reserve to allow for zlib overhead. This does not include data-length
   * dependent overheads and compression latency (delay between providing data to zlib and output of
   * the compressed data).
   *
   * <p>References:
   *   deflate framing: http://www.gzip.org/zlib/rfc-deflate.html
   *     (note that bit-packing is little-endian (section 3.1.1) whereas description of sequences
   *     is big-endian, so bits appear reversed),
   *   zlib framing: http://tools.ietf.org/html/rfc1950,
   *   details on flush behavior: http://www.zlib.net/manual.html
   */
  @VisibleForTesting
  static final int MARGIN
      = 5 /* deflate current block overhead, assuming no compression:
             block type (1) + len (2) + nlen (2) */
      + 5 /* deflate flush; adds an empty block after current:
             00 (not end; no compression) 00 00 (len) FF FF (nlen) */
      + 5 /* deflate flush; some versions of zlib output two empty blocks on some flushes */
      + 5 /* deflate finish; adds empty block to mark end, since we commonly flush before finish:
             03 (end; fixed Huffman + 5 bits of end of block) 00 (last 3 bits + padding),
             or if compression level is 0: 01 (end; no compression) 00 00 (len) FF FF (nlen) */
      + 2 /* zlib header; CMF (1) + FLG (1) */ + 4 /* zlib ADLER32 (4) */
      + 5 /* additional safety for good measure */;

  private static final Logger log = Logger.getLogger(CompressionFramer.class.getName());

  /**
   * Bytes of frame being constructed. {@code position() == 0} when no frame in progress.
   */
  private final ByteBuffer bytebuf;
  /** Number of frame bytes it is acceptable to leave unused when compressing. */
  private final int sufficient;
  private Deflater deflater;
  /** Number of bytes written to deflater since last deflate sync. */
  private int writtenSinceSync;
  /** Number of bytes read from deflater since last deflate sync. */
  private int readSinceSync;
  /**
   * Whether the current frame is actually being compressed. If {@code bytebuf.position() == 0},
   * then this value has no meaning.
   */
  private boolean usingCompression;
  /**
   * Whether compression is requested. This does not imply we are compressing the current frame
   * (see {@link #usingCompression}), or that we will even compress the next frame (see {@link
   * #compressionUnsupported}).
   */
  private boolean allowCompression;
  /** Whether compression is possible with current configuration and platform. */
  private final boolean compressionUnsupported;
  /**
   * Compression level to set on the Deflater, where {@code DEFAULT_COMPRESSION_LEVEL} implies this
   * class's default.
   */
  private int compressionLevel = DEFAULT_COMPRESSION_LEVEL;
  private final OutputStreamAdapter outputStreamAdapter = new OutputStreamAdapter();

  /**
   * Since compression tries to form full frames, if compression is working well then it will
   * consecutively compress smaller amounts of input data in order to not exceed the frame size. For
   * example, if the data is getting 50% compression and a maximum frame size of 128, then it will
   * encode roughly 128 bytes which leaves 64, so we encode 64, 32, 16, 8, 4, 2, 1, 1.
   * {@code sufficient} cuts off the long tail and says that at some point the frame is "good
   * enough" to stop. Choosing a value of {@code 0} is not outrageous.
   *
   * @param maxFrameSize maximum number of bytes allowed for output frames
   * @param allowCompression whether frames should be compressed
   * @param sufficient number of frame bytes it is acceptable to leave unused when compressing
   */
  public CompressionFramer(int maxFrameSize, boolean allowCompression, int sufficient) {
    this.allowCompression = allowCompression;
    int maxSufficient = maxFrameSize - HEADER_LENGTH - MARGIN
        - 1 /* to force at least one byte of data */;
    boolean compressionUnsupported = false;
    if (maxSufficient < 0) {
      compressionUnsupported = true;
      log.log(Level.INFO, "Frame not large enough for compression");
    } else if (maxSufficient < sufficient) {
      log.log(Level.INFO, "Compression sufficient reduced to {0} from {1} to fit in frame size {2}",
          new Object[] {maxSufficient, sufficient, maxFrameSize});
      sufficient = maxSufficient;
    }
    this.sufficient = sufficient;
    // TODO(user): Benchmark before switching to direct buffers
    bytebuf = ByteBuffer.allocate(maxFrameSize);
    if (!bytebuf.hasArray()) {
      compressionUnsupported = true;
      log.log(Level.INFO, "Byte buffer doesn't support array(), which is required for compression");
    }
    this.compressionUnsupported = compressionUnsupported;
  }

  /**
   * Sets whether compression is encouraged.
   */
  public void setAllowCompression(boolean allow) {
    this.allowCompression = allow;
  }

  /**
   * Set the preferred compression level for when compression is enabled.
   *
   * @param level the preferred compression level (0-9), or {@code DEFAULT_COMPRESSION_LEVEL} to use
   *     this class's default
   * @see java.util.zip.Deflater#setLevel
   */
  public void setCompressionLevel(int level) {
    Preconditions.checkArgument(level == DEFAULT_COMPRESSION_LEVEL
        || (level >= Deflater.NO_COMPRESSION && level <= Deflater.BEST_COMPRESSION),
        "invalid compression level");
    this.compressionLevel = level;
  }

  /**
   * Ensures state and buffers are initialized for writing data to a frame. Callers should be very
   * aware this method may modify {@code usingCompression}.
   */
  private void checkInitFrame() {
    if (bytebuf.position() != 0) {
      return;
    }
    bytebuf.position(HEADER_LENGTH);
    usingCompression = compressionUnsupported ? false : allowCompression;
    if (usingCompression) {
      if (deflater == null) {
        deflater = new Deflater();
      } else {
        deflater.reset();
      }
      deflater.setLevel(compressionLevel == DEFAULT_COMPRESSION_LEVEL
          ? Deflater.DEFAULT_COMPRESSION : compressionLevel);
      writtenSinceSync = 0;
      readSinceSync = 0;
    }
  }

  /** Frame contents of {@code message}, flushing to {@code sink} as necessary. */
  public int write(InputStream message, Sink sink) throws IOException {
    checkInitFrame();
    if (!usingCompression && bytebuf.hasArray()) {
      if (bytebuf.remaining() == 0) {
        commitToSink(sink, false);
      }
      int available = message.available();
      if (available <= bytebuf.remaining()) {
        // When InputStream is DeferredProtoInputStream, this is zero-copy because bytebuf is large
        // enough for the proto to be serialized directly into it.
        int read = ByteStreams.read(message,
            bytebuf.array(), bytebuf.arrayOffset() + bytebuf.position(), bytebuf.remaining());
        bytebuf.position(bytebuf.position() + read);
        if (read != available) {
          throw new RuntimeException("message.available() did not follow our semantics of always "
              + "returning the number of remaining bytes");
        }
        return read;
      }
    }
    outputStreamAdapter.setSink(sink);
    try {
      if (message instanceof DeferredInputStream) {
        return ((DeferredInputStream) message).flushTo(outputStreamAdapter);
      } else {
        // This could be optimized when compression is off, but we expect performance-critical code
        // to provide a DeferredInputStream.
        return (int) ByteStreams.copy(message, outputStreamAdapter);
      }
    } finally {
      outputStreamAdapter.setSink(null);
    }
  }

  /**
   * Frame contents of {@code b} between {@code off} (inclusive) and {@code off + len} (exclusive),
   * flushing to {@code sink} as necessary.
   */
  public void write(byte[] b, int off, int len, Sink sink) {
    while (len > 0) {
      checkInitFrame();
      if (!usingCompression) {
        if (bytebuf.remaining() == 0) {
          commitToSink(sink, false);
          continue;
        }
        int toWrite = Math.min(len, bytebuf.remaining());
        bytebuf.put(b, off, toWrite);
        off += toWrite;
        len -= toWrite;
      } else {
        if (bytebuf.remaining() <= MARGIN + sufficient) {
          commitToSink(sink, false);
          continue;
        }
        // Amount of memory that is guaranteed not to be consumed, including in-flight data in zlib.
        int safeCapacity = bytebuf.remaining() - MARGIN
            - (writtenSinceSync - readSinceSync) - dataLengthDependentOverhead(writtenSinceSync);
        if (safeCapacity <= 0) {
          while (deflatePut(deflater, bytebuf, Deflater.SYNC_FLUSH) != 0) {}
          writtenSinceSync = 0;
          readSinceSync = 0;
          continue;
        }
        int toWrite = Math.min(len, safeCapacity - dataLengthDependentOverhead(safeCapacity));
        deflater.setInput(b, off, toWrite);
        writtenSinceSync += toWrite;
        while (!deflater.needsInput()) {
          readSinceSync += deflatePut(deflater, bytebuf, Deflater.NO_FLUSH);
        }
        // Clear internal references of byte[] b.
        deflater.setInput(EMPTY_BYTE_ARRAY);
        off += toWrite;
        len -= toWrite;
      }
    }
  }

  /**
   * When data is uncompressable, there are 5B of overhead per deflate block, which is generally
   * 16 KiB for zlib, but the format supports up to 32 KiB. One block's overhead is already
   * accounted for in MARGIN. We use 1B/2KiB to circumvent dealing with rounding errors. Note that
   * 1B/2KiB is not enough to support 8 KiB blocks due to rounding errors.
   */
  private static int dataLengthDependentOverhead(int length) {
    return length / 2048;
  }

  private static int deflatePut(Deflater deflater, ByteBuffer bytebuf, int flush) {
    if (bytebuf.remaining() == 0) {
      throw new AssertionError("Compressed data exceeded frame size");
    }
    int deflateBytes = deflater.deflate(bytebuf.array(), bytebuf.arrayOffset() + bytebuf.position(),
        bytebuf.remaining(), flush);
    bytebuf.position(bytebuf.position() + deflateBytes);
    return deflateBytes;
  }

  public void endOfMessage(Sink sink) {
    if ((!usingCompression && bytebuf.remaining() == 0)
        || (usingCompression && bytebuf.remaining() <= MARGIN + sufficient)) {
      commitToSink(sink, true);
    }
  }

  public void flush(Sink sink) {
    if (bytebuf.position() == 0) {
      return;
    }
    commitToSink(sink, true);
  }

  /**
   * Writes compression frame to sink. It does not initialize the next frame, so {@link
   * #checkInitFrame()} is necessary if other frames are to follow.
   */
  private void commitToSink(Sink sink, boolean endOfMessage) {
    if (usingCompression) {
      deflater.finish();
      while (!deflater.finished()) {
        deflatePut(deflater, bytebuf, Deflater.NO_FLUSH);
      }
      if (endOfMessage) {
        deflater.end();
        deflater = null;
      }
    }
    int frameFlag = usingCompression
        ? TransportFrameUtil.FLATE_FLAG : TransportFrameUtil.NO_COMPRESS_FLAG;
    // Header = 1b flag | 3b length of GRPC frame
    int header = (frameFlag << 24) | (bytebuf.position() - 4);
    bytebuf.putInt(0, header);
    bytebuf.flip();
    sink.deliverFrame(bytebuf, endOfMessage);
    bytebuf.clear();
  }

  private class OutputStreamAdapter extends OutputStream {
    private Sink sink;
    private final byte[] singleByte = new byte[1];

    @Override
    public void write(int b) {
      singleByte[0] = (byte) b;
      write(singleByte, 0, 1);
    }

    @Override
    public void write(byte[] b, int off, int len) {
      CompressionFramer.this.write(b, off, len, sink);
    }

    public void setSink(Sink sink) {
      this.sink = sink;
    }
  }
}
