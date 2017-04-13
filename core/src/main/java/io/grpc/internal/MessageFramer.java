/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static java.lang.Math.min;

import com.google.common.io.ByteStreams;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.Drainable;
import io.grpc.KnownLength;
import io.grpc.Status;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.util.ArrayList;
import java.util.List;
import javax.annotation.Nullable;

/**
 * Encodes gRPC messages to be delivered via the transport layer which implements {@link
 * MessageFramer.Sink}.
 */
public class MessageFramer implements Framer {

  private static final int NO_MAX_OUTBOUND_MESSAGE_SIZE = -1;

  /**
   * Sink implemented by the transport layer to receive frames and forward them to their
   * destination.
   */
  public interface Sink {
    /**
     * Delivers a frame via the transport.
     *
     * @param frame a non-empty buffer to deliver or {@code null} if the framer is being
     *              closed and there is no data to deliver.
     * @param endOfStream whether the frame is the last one for the GRPC stream
     * @param flush {@code true} if more data may not be arriving soon
     */
    void deliverFrame(@Nullable WritableBuffer frame, boolean endOfStream, boolean flush);
  }

  private static final int HEADER_LENGTH = 5;
  private static final byte UNCOMPRESSED = 0;
  private static final byte COMPRESSED = 1;

  private final Sink sink;
  // effectively final.  Can only be set once.
  private int maxOutboundMessageSize = NO_MAX_OUTBOUND_MESSAGE_SIZE;
  private WritableBuffer buffer;
  private Compressor compressor = Codec.Identity.NONE;
  private boolean messageCompression = true;
  private final OutputStreamAdapter outputStreamAdapter = new OutputStreamAdapter();
  private final byte[] headerScratch = new byte[HEADER_LENGTH];
  private final WritableBufferAllocator bufferAllocator;
  private final StatsTraceContext statsTraceCtx;
  private boolean closed;

  /**
   * Creates a {@code MessageFramer}.
   *
   * @param sink the sink used to deliver frames to the transport
   * @param bufferAllocator allocates buffers that the transport can commit to the wire.
   */
  public MessageFramer(Sink sink, WritableBufferAllocator bufferAllocator,
      StatsTraceContext statsTraceCtx) {
    this.sink = checkNotNull(sink, "sink");
    this.bufferAllocator = checkNotNull(bufferAllocator, "bufferAllocator");
    this.statsTraceCtx = checkNotNull(statsTraceCtx, "statsTraceCtx");
  }

  @Override
  public MessageFramer setCompressor(Compressor compressor) {
    this.compressor = checkNotNull(compressor, "Can't pass an empty compressor");
    return this;
  }

  @Override
  public MessageFramer setMessageCompression(boolean enable) {
    messageCompression = enable;
    return this;
  }

  @Override
  public void setMaxOutboundMessageSize(int maxSize) {
    checkState(maxOutboundMessageSize == NO_MAX_OUTBOUND_MESSAGE_SIZE, "max size already set");
    maxOutboundMessageSize = maxSize;
  }

  /**
   * Writes out a payload message.
   *
   * @param message contains the message to be written out. It will be completely consumed.
   */
  @Override
  public void writePayload(InputStream message) {
    verifyNotClosed();
    statsTraceCtx.outboundMessage();
    boolean compressed = messageCompression && compressor != Codec.Identity.NONE;
    int written = -1;
    int messageLength = -2;
    try {
      messageLength = getKnownLength(message);
      if (messageLength != 0 && compressed) {
        written = writeCompressed(message, messageLength);
      } else {
        written = writeUncompressed(message, messageLength);
      }
    } catch (IOException e) {
      // This should not be possible, since sink#deliverFrame doesn't throw.
      throw Status.INTERNAL
          .withDescription("Failed to frame message")
          .withCause(e)
          .asRuntimeException();
    } catch (RuntimeException e) {
      throw Status.INTERNAL
          .withDescription("Failed to frame message")
          .withCause(e)
          .asRuntimeException();
    }

    if (messageLength != -1 && written != messageLength) {
      String err = String.format("Message length inaccurate %s != %s", written, messageLength);
      throw Status.INTERNAL.withDescription(err).asRuntimeException();
    }
    statsTraceCtx.outboundUncompressedSize(written);
  }

  private int writeUncompressed(InputStream message, int messageLength) throws IOException {
    if (messageLength != -1) {
      statsTraceCtx.outboundWireSize(messageLength);
      return writeKnownLengthUncompressed(message, messageLength);
    }
    BufferChainOutputStream bufferChain = new BufferChainOutputStream();
    int written = writeToOutputStream(message, bufferChain);
    if (maxOutboundMessageSize >= 0 && written > maxOutboundMessageSize) {
      throw Status.RESOURCE_EXHAUSTED
          .withDescription(
              String.format("message too large %d > %d", written , maxOutboundMessageSize))
          .asRuntimeException();
    }
    writeBufferChain(bufferChain, false);
    return written;
  }

  private int writeCompressed(InputStream message, int unusedMessageLength) throws IOException {
    BufferChainOutputStream bufferChain = new BufferChainOutputStream();

    OutputStream compressingStream = compressor.compress(bufferChain);
    int written;
    try {
      written = writeToOutputStream(message, compressingStream);
    } finally {
      compressingStream.close();
    }
    if (maxOutboundMessageSize >= 0 && written > maxOutboundMessageSize) {
      throw Status.RESOURCE_EXHAUSTED
          .withDescription(
              String.format("message too large %d > %d", written , maxOutboundMessageSize))
          .asRuntimeException();
    }

    writeBufferChain(bufferChain, true);
    return written;
  }

  private int getKnownLength(InputStream inputStream) throws IOException {
    if (inputStream instanceof KnownLength || inputStream instanceof ByteArrayInputStream) {
      return inputStream.available();
    }
    return -1;
  }

  /**
   * Write an unserialized message with a known length, uncompressed.
   */
  private int writeKnownLengthUncompressed(InputStream message, int messageLength)
      throws IOException {
    if (maxOutboundMessageSize >= 0 && messageLength > maxOutboundMessageSize) {
      throw Status.RESOURCE_EXHAUSTED
          .withDescription(
              String.format("message too large %d > %d", messageLength , maxOutboundMessageSize))
          .asRuntimeException();
    }
    ByteBuffer header = ByteBuffer.wrap(headerScratch);
    header.put(UNCOMPRESSED);
    header.putInt(messageLength);
    // Allocate the initial buffer chunk based on frame header + payload length.
    // Note that the allocator may allocate a buffer larger or smaller than this length
    if (buffer == null) {
      buffer = bufferAllocator.allocate(header.position() + messageLength);
    }
    writeRaw(headerScratch, 0, header.position());
    return writeToOutputStream(message, outputStreamAdapter);
  }

  /**
   * Write a message that has been serialized to a sequence of buffers.
   */
  private void writeBufferChain(BufferChainOutputStream bufferChain, boolean compressed) {
    ByteBuffer header = ByteBuffer.wrap(headerScratch);
    header.put(compressed ? COMPRESSED : UNCOMPRESSED);
    int messageLength = bufferChain.readableBytes();
    header.putInt(messageLength);
    WritableBuffer writeableHeader = bufferAllocator.allocate(HEADER_LENGTH);
    writeableHeader.write(headerScratch, 0, header.position());
    if (messageLength == 0) {
      // the payload had 0 length so make the header the current buffer.
      buffer = writeableHeader;
      return;
    }
    // Note that we are always delivering a small message to the transport here which
    // may incur transport framing overhead as it may be sent separately to the contents
    // of the GRPC frame.
    sink.deliverFrame(writeableHeader, false, false);
    // Commit all except the last buffer to the sink
    List<WritableBuffer> bufferList = bufferChain.bufferList;
    for (int i = 0; i < bufferList.size() - 1; i++) {
      sink.deliverFrame(bufferList.get(i), false, false);
    }
    // Assign the current buffer to the last in the chain so it can be used
    // for future writes or written with end-of-stream=true on close.
    buffer = bufferList.get(bufferList.size() - 1);
    statsTraceCtx.outboundWireSize(messageLength);
  }

  private static int writeToOutputStream(InputStream message, OutputStream outputStream)
      throws IOException {
    if (message instanceof Drainable) {
      return ((Drainable) message).drainTo(outputStream);
    } else {
      // This makes an unnecessary copy of the bytes when bytebuf supports array(). However, we
      // expect performance-critical code to support flushTo().
      long written = ByteStreams.copy(message, outputStream);
      checkArgument(written <= Integer.MAX_VALUE, "Message size overflow: %s", written);
      return (int) written;
    }
  }

  private void writeRaw(byte[] b, int off, int len) {
    while (len > 0) {
      if (buffer != null && buffer.writableBytes() == 0) {
        commitToSink(false, false);
      }
      if (buffer == null) {
        // Request a buffer allocation using the message length as a hint.
        buffer = bufferAllocator.allocate(len);
      }
      int toWrite = min(len, buffer.writableBytes());
      buffer.write(b, off, toWrite);
      off += toWrite;
      len -= toWrite;
    }
  }

  /**
   * Flushes any buffered data in the framer to the sink.
   */
  @Override
  public void flush() {
    if (buffer != null && buffer.readableBytes() > 0) {
      commitToSink(false, true);
    }
  }

  /**
   * Indicates whether or not this framer has been closed via a call to either
   * {@link #close()} or {@link #dispose()}.
   */
  @Override
  public boolean isClosed() {
    return closed;
  }

  /**
   * Flushes and closes the framer and releases any buffers. After the framer is closed or
   * disposed, additional calls to this method will have no affect.
   */
  @Override
  public void close() {
    if (!isClosed()) {
      closed = true;
      // With the current code we don't expect readableBytes > 0 to be possible here, added
      // defensively to prevent buffer leak issues if the framer code changes later.
      if (buffer != null && buffer.readableBytes() == 0) {
        releaseBuffer();
      }
      commitToSink(true, true);
    }
  }

  /**
   * Closes the framer and releases any buffers, but does not flush. After the framer is
   * closed or disposed, additional calls to this method will have no affect.
   */
  @Override
  public void dispose() {
    closed = true;
    releaseBuffer();
  }

  private void releaseBuffer() {
    if (buffer != null) {
      buffer.release();
      buffer = null;
    }
  }

  private void commitToSink(boolean endOfStream, boolean flush) {
    WritableBuffer buf = buffer;
    buffer = null;
    sink.deliverFrame(buf, endOfStream, flush);
  }

  private void verifyNotClosed() {
    if (isClosed()) {
      throw new IllegalStateException("Framer already closed");
    }
  }

  /** OutputStream whose write()s are passed to the framer. */
  private class OutputStreamAdapter extends OutputStream {
    /**
     * This is slow, don't call it.  If you care about write overhead, use a BufferedOutputStream.
     * Better yet, you can use your own single byte buffer and call
     * {@link #write(byte[], int, int)}.
     */
    @Override
    public void write(int b) {
      byte[] singleByte = new byte[]{(byte)b};
      write(singleByte, 0, 1);
    }

    @Override
    public void write(byte[] b, int off, int len) {
      writeRaw(b, off, len);
    }
  }

  /**
   * Produce a collection of {@link WritableBuffer} instances from the data written to an
   * {@link OutputStream}.
   */
  private final class BufferChainOutputStream extends OutputStream {
    private final List<WritableBuffer> bufferList = new ArrayList<WritableBuffer>();
    private WritableBuffer current;

    /**
     * This is slow, don't call it.  If you care about write overhead, use a BufferedOutputStream.
     * Better yet, you can use your own single byte buffer and call
     * {@link #write(byte[], int, int)}.
     */
    @Override
    public void write(int b) throws IOException {
      if (current != null && current.writableBytes() > 0) {
        current.write((byte)b);
        return;
      }
      byte[] singleByte = new byte[]{(byte)b};
      write(singleByte, 0, 1);
    }

    @Override
    public void write(byte[] b, int off, int len) {
      if (current == null) {
        // Request len bytes initially from the allocator, it may give us more.
        current = bufferAllocator.allocate(len);
        bufferList.add(current);
      }
      while (len > 0) {
        int canWrite = Math.min(len, current.writableBytes());
        if (canWrite == 0) {
          // Assume message is twice as large as previous assumption if were still not done,
          // the allocator may allocate more or less than this amount.
          int needed = Math.max(len, current.readableBytes() * 2);
          current = bufferAllocator.allocate(needed);
          bufferList.add(current);
        } else {
          current.write(b, off, canWrite);
          off += canWrite;
          len -= canWrite;
        }
      }
    }

    private int readableBytes() {
      int readable = 0;
      for (WritableBuffer writableBuffer : bufferList) {
        readable += writableBuffer.readableBytes();
      }
      return readable;
    }
  }
}
