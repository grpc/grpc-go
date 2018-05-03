/*
 * Copyright 2017 The gRPC Authors
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

import static com.google.common.base.Preconditions.checkState;

import java.io.Closeable;
import java.util.zip.CRC32;
import java.util.zip.DataFormatException;
import java.util.zip.Inflater;
import java.util.zip.ZipException;
import javax.annotation.concurrent.NotThreadSafe;

/**
 * Processes gzip streams, delegating to {@link Inflater} to perform on-demand inflation of the
 * deflated blocks. Like {@link java.util.zip.GZIPInputStream}, this handles concatenated gzip
 * streams. Unlike {@link java.util.zip.GZIPInputStream}, this allows for incremental processing of
 * gzip streams, allowing data to be inflated as it arrives over the wire.
 *
 * <p>This also frees the inflate context when the end of a gzip stream is reached without another
 * concatenated stream available to inflate.
 */
@NotThreadSafe
class GzipInflatingBuffer implements Closeable {

  private static final int INFLATE_BUFFER_SIZE = 512;
  private static final int UNSIGNED_SHORT_SIZE = 2;

  private static final int GZIP_MAGIC = 0x8b1f;

  private static final int GZIP_HEADER_MIN_SIZE = 10;
  private static final int GZIP_TRAILER_SIZE = 8;

  private static final int HEADER_CRC_FLAG = 2;
  private static final int HEADER_EXTRA_FLAG = 4;
  private static final int HEADER_NAME_FLAG = 8;
  private static final int HEADER_COMMENT_FLAG = 16;

  /**
   * Reads gzip header and trailer bytes from the inflater's buffer (if bytes beyond the inflate
   * block were given to the inflater) and then from {@code gzippedData}, and handles updating the
   * CRC and the count of gzipped bytes consumed.
   */
  private class GzipMetadataReader {

    /**
     * Returns the next unsigned byte, adding it the CRC and incrementing {@code bytesConsumed}.
     *
     * <p>It is the responsibility of the caller to verify and reset the CRC as needed, as well as
     * caching the current CRC value when necessary before invoking this method.
     */
    private int readUnsignedByte() {
      int bytesRemainingInInflaterInput = inflaterInputEnd - inflaterInputStart;
      int b;
      if (bytesRemainingInInflaterInput > 0) {
        b = inflaterInput[inflaterInputStart] & 0xFF;
        inflaterInputStart += 1;
      } else {
        b = gzippedData.readUnsignedByte();
      }
      crc.update(b);
      bytesConsumed += 1;
      return b;
    }

    /**
     * Skips {@code length} bytes, adding them to the CRC and adding {@code length} to {@code
     * bytesConsumed}.
     *
     * <p>It is the responsibility of the caller to verify and reset the CRC as needed, as well as
     * caching the current CRC value when necessary before invoking this method.
     */
    private void skipBytes(int length) {
      int bytesToSkip = length;
      int bytesRemainingInInflaterInput = inflaterInputEnd - inflaterInputStart;

      if (bytesRemainingInInflaterInput > 0) {
        int bytesToGetFromInflaterInput = Math.min(bytesRemainingInInflaterInput, bytesToSkip);
        crc.update(inflaterInput, inflaterInputStart, bytesToGetFromInflaterInput);
        inflaterInputStart += bytesToGetFromInflaterInput;
        bytesToSkip -= bytesToGetFromInflaterInput;
      }

      if (bytesToSkip > 0) {
        byte[] buf = new byte[512];
        int total = 0;
        while (total < bytesToSkip) {
          int toRead = Math.min(bytesToSkip - total, buf.length);
          gzippedData.readBytes(buf, 0, toRead);
          crc.update(buf, 0, toRead);
          total += toRead;
        }
      }

      bytesConsumed += length;
    }

    private int readableBytes() {
      return (inflaterInputEnd - inflaterInputStart) + gzippedData.readableBytes();
    }

    /** Skip over a zero-terminated byte sequence. Returns true when the zero byte is read. */
    private boolean readBytesUntilZero() {
      while (readableBytes() > 0) {
        if (readUnsignedByte() == 0) {
          return true;
        }
      }
      return false;
    }

    /** Reads unsigned short in Little-Endian byte order. */
    private int readUnsignedShort() {
      return readUnsignedByte() | (readUnsignedByte() << 8);
    }

    /** Reads unsigned integer in Little-Endian byte order. */
    private long readUnsignedInt() {
      long s = readUnsignedShort();
      return ((long) readUnsignedShort() << 16) | s;
    }
  }

  private enum State {
    HEADER,
    HEADER_EXTRA_LEN,
    HEADER_EXTRA,
    HEADER_NAME,
    HEADER_COMMENT,
    HEADER_CRC,
    INITIALIZE_INFLATER,
    INFLATING,
    INFLATER_NEEDS_INPUT,
    TRAILER
  }

  /**
   * This buffer holds all input gzipped data, consisting of blocks of deflated data and the
   * surrounding gzip headers and trailers. All access to the Gzip headers and trailers must be made
   * via {@link GzipMetadataReader}.
   */
  private final CompositeReadableBuffer gzippedData = new CompositeReadableBuffer();

  private final CRC32 crc = new CRC32();

  private final GzipMetadataReader gzipMetadataReader = new GzipMetadataReader();
  private final byte[] inflaterInput = new byte[INFLATE_BUFFER_SIZE];
  private int inflaterInputStart;
  private int inflaterInputEnd;
  private Inflater inflater;
  private State state = State.HEADER;
  private boolean closed = false;

  /** Extra state variables for parsing gzip header flags. */
  private int gzipHeaderFlag;
  private int headerExtraToRead;

  /* Number of inflated bytes per gzip stream, used to validate the gzip trailer. */
  private long expectedGzipTrailerIsize;

  /**
   * Tracks gzipped bytes (including gzip metadata and deflated blocks) consumed during {@link
   * #inflateBytes} calls.
   */
  private int bytesConsumed = 0;

  /** Tracks deflated bytes (excluding gzip metadata) consumed by the inflater. */
  private int deflatedBytesConsumed = 0;

  private boolean isStalled = true;

  /**
   * Returns true when more bytes must be added via {@link #addGzippedBytes} to enable additional
   * calls to {@link #inflateBytes} to make progress.
   */
  boolean isStalled() {
    checkState(!closed, "GzipInflatingBuffer is closed");
    return isStalled;
  }

  /**
   * Returns true when there is gzippedData that has not been input to the inflater or the inflater
   * has not consumed all of its input, or all data has been consumed but we are at not at the
   * boundary between gzip streams.
   */
  boolean hasPartialData() {
    checkState(!closed, "GzipInflatingBuffer is closed");
    return gzipMetadataReader.readableBytes() != 0 || state != State.HEADER;
  }

  /**
   * Adds more gzipped data, which will be consumed only when needed to fulfill requests made via
   * {@link #inflateBytes}.
   */
  void addGzippedBytes(ReadableBuffer buffer) {
    checkState(!closed, "GzipInflatingBuffer is closed");
    gzippedData.addBuffer(buffer);
    isStalled = false;
  }

  @Override
  public void close() {
    if (!closed) {
      closed = true;
      gzippedData.close();
      if (inflater != null) {
        inflater.end();
        inflater = null;
      }
    }
  }

  /**
   * Reports bytes consumed by calls to {@link #inflateBytes} since the last invocation of this
   * method, then resets the count to zero.
   */
  int getAndResetBytesConsumed() {
    int savedBytesConsumed = bytesConsumed;
    bytesConsumed = 0;
    return savedBytesConsumed;
  }

  /**
   * Reports bytes consumed by the inflater since the last invocation of this method, then resets
   * the count to zero.
   */
  int getAndResetDeflatedBytesConsumed() {
    int savedDeflatedBytesConsumed = deflatedBytesConsumed;
    deflatedBytesConsumed = 0;
    return savedDeflatedBytesConsumed;
  }

  /**
   * Attempts to inflate {@code length} bytes of data into {@code b}.
   *
   * <p>Any gzipped bytes consumed by this method will be added to the counter returned by {@link
   * #getAndResetBytesConsumed()}. This method may consume gzipped bytes without writing any data to
   * {@code b}, and may also write data to {@code b} without consuming additional gzipped bytes (if
   * the inflater on an earlier call consumed the bytes necessary to produce output).
   *
   * @param b the destination array to receive the bytes.
   * @param offset the starting offset in the destination array.
   * @param length the number of bytes to be copied.
   * @throws IndexOutOfBoundsException if {@code b} is too small to hold the requested bytes.
   */
  int inflateBytes(byte[] b, int offset, int length) throws DataFormatException, ZipException {
    checkState(!closed, "GzipInflatingBuffer is closed");

    int bytesRead = 0;
    int missingBytes;
    boolean madeProgress = true;
    while (madeProgress && (missingBytes = length - bytesRead) > 0) {
      switch (state) {
        case HEADER:
          madeProgress = processHeader();
          break;
        case HEADER_EXTRA_LEN:
          madeProgress = processHeaderExtraLen();
          break;
        case HEADER_EXTRA:
          madeProgress = processHeaderExtra();
          break;
        case HEADER_NAME:
          madeProgress = processHeaderName();
          break;
        case HEADER_COMMENT:
          madeProgress = processHeaderComment();
          break;
        case HEADER_CRC:
          madeProgress = processHeaderCrc();
          break;
        case INITIALIZE_INFLATER:
          madeProgress = initializeInflater();
          break;
        case INFLATING:
          bytesRead += inflate(b, offset + bytesRead, missingBytes);
          if (state == State.TRAILER) {
            // Eagerly process trailer, if available, to validate CRC.
            madeProgress = processTrailer();
          } else {
            // Continue in INFLATING until we have the required bytes or we transition to
            // INFLATER_NEEDS_INPUT
            madeProgress = true;
          }
          break;
        case INFLATER_NEEDS_INPUT:
          madeProgress = fill();
          break;
        case TRAILER:
          madeProgress = processTrailer();
          break;
        default:
          throw new AssertionError("Invalid state: " + state);
      }
    }
    // If we finished a gzip block, check if we have enough bytes to read another header
    isStalled =
        !madeProgress
            || (state == State.HEADER && gzipMetadataReader.readableBytes() < GZIP_HEADER_MIN_SIZE);

    return bytesRead;
  }

  private boolean processHeader() throws ZipException {
    if (gzipMetadataReader.readableBytes() < GZIP_HEADER_MIN_SIZE) {
      return false;
    }
    if (gzipMetadataReader.readUnsignedShort() != GZIP_MAGIC) {
      throw new ZipException("Not in GZIP format");
    }
    if (gzipMetadataReader.readUnsignedByte() != 8) {
      throw new ZipException("Unsupported compression method");
    }
    gzipHeaderFlag = gzipMetadataReader.readUnsignedByte();
    gzipMetadataReader.skipBytes(6 /* remaining header bytes */);
    state = State.HEADER_EXTRA_LEN;
    return true;
  }

  private boolean processHeaderExtraLen() {
    if ((gzipHeaderFlag & HEADER_EXTRA_FLAG) != HEADER_EXTRA_FLAG) {
      state = State.HEADER_NAME;
      return true;
    }
    if (gzipMetadataReader.readableBytes() < UNSIGNED_SHORT_SIZE) {
      return false;
    }
    headerExtraToRead = gzipMetadataReader.readUnsignedShort();
    state = State.HEADER_EXTRA;
    return true;
  }

  private boolean processHeaderExtra() {
    if (gzipMetadataReader.readableBytes() < headerExtraToRead) {
      return false;
    }
    gzipMetadataReader.skipBytes(headerExtraToRead);
    state = State.HEADER_NAME;
    return true;
  }

  private boolean processHeaderName() {
    if ((gzipHeaderFlag & HEADER_NAME_FLAG) != HEADER_NAME_FLAG) {
      state = State.HEADER_COMMENT;
      return true;
    }
    if (!gzipMetadataReader.readBytesUntilZero()) {
      return false;
    }
    state = State.HEADER_COMMENT;
    return true;
  }

  private boolean processHeaderComment() {
    if ((gzipHeaderFlag & HEADER_COMMENT_FLAG) != HEADER_COMMENT_FLAG) {
      state = State.HEADER_CRC;
      return true;
    }
    if (!gzipMetadataReader.readBytesUntilZero()) {
      return false;
    }
    state = State.HEADER_CRC;
    return true;
  }

  private boolean processHeaderCrc() throws ZipException {
    if ((gzipHeaderFlag & HEADER_CRC_FLAG) != HEADER_CRC_FLAG) {
      state = State.INITIALIZE_INFLATER;
      return true;
    }
    if (gzipMetadataReader.readableBytes() < UNSIGNED_SHORT_SIZE) {
      return false;
    }
    int desiredCrc16 = (int) crc.getValue() & 0xffff;
    if (desiredCrc16 != gzipMetadataReader.readUnsignedShort()) {
      throw new ZipException("Corrupt GZIP header");
    }
    state = State.INITIALIZE_INFLATER;
    return true;
  }

  private boolean initializeInflater() {
    if (inflater == null) {
      inflater = new Inflater(true);
    } else {
      inflater.reset();
    }
    crc.reset();
    int bytesRemainingInInflaterInput = inflaterInputEnd - inflaterInputStart;
    if (bytesRemainingInInflaterInput > 0) {
      inflater.setInput(inflaterInput, inflaterInputStart, bytesRemainingInInflaterInput);
      state = State.INFLATING;
    } else {
      state = State.INFLATER_NEEDS_INPUT;
    }
    return true;
  }

  private int inflate(byte[] b, int off, int len) throws DataFormatException, ZipException {
    checkState(inflater != null, "inflater is null");

    try {
      int inflaterTotalIn = inflater.getTotalIn();
      int n = inflater.inflate(b, off, len);
      int bytesConsumedDelta = inflater.getTotalIn() - inflaterTotalIn;
      bytesConsumed += bytesConsumedDelta;
      deflatedBytesConsumed += bytesConsumedDelta;
      inflaterInputStart += bytesConsumedDelta;
      crc.update(b, off, n);

      if (inflater.finished()) {
        // Save bytes written to check against the trailer ISIZE
        expectedGzipTrailerIsize = (inflater.getBytesWritten() & 0xffffffffL);

        state = State.TRAILER;
      } else if (inflater.needsInput()) {
        state = State.INFLATER_NEEDS_INPUT;
      }

      return n;
    } catch (DataFormatException e) {
      // Wrap the exception so tests can check for a specific prefix
      throw new DataFormatException("Inflater data format exception: " + e.getMessage());
    }
  }

  private boolean fill() {
    checkState(inflater != null, "inflater is null");
    checkState(inflaterInputStart == inflaterInputEnd, "inflaterInput has unconsumed bytes");
    int bytesToAdd = Math.min(gzippedData.readableBytes(), INFLATE_BUFFER_SIZE);
    if (bytesToAdd == 0) {
      return false;
    }
    inflaterInputStart = 0;
    inflaterInputEnd = bytesToAdd;
    gzippedData.readBytes(inflaterInput, inflaterInputStart, bytesToAdd);
    inflater.setInput(inflaterInput, inflaterInputStart, bytesToAdd);
    state = State.INFLATING;
    return true;
  }

  private boolean processTrailer() throws ZipException {
    if (inflater != null
        && gzipMetadataReader.readableBytes() <= GZIP_HEADER_MIN_SIZE + GZIP_TRAILER_SIZE) {
      // We don't have enough bytes to begin inflating a concatenated gzip stream, drop context
      inflater.end();
      inflater = null;
    }
    if (gzipMetadataReader.readableBytes() < GZIP_TRAILER_SIZE) {
      return false;
    }
    if (crc.getValue() != gzipMetadataReader.readUnsignedInt()
        || expectedGzipTrailerIsize != gzipMetadataReader.readUnsignedInt()) {
      throw new ZipException("Corrupt GZIP trailer");
    }
    crc.reset();
    state = State.HEADER;
    return true;
  }
}
