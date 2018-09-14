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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static java.lang.Math.min;

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
     * @param numMessages the number of messages that this series of frames represents
     */
    void deliverFrame(
        @Nullable WritableBuffer frame,
        boolean endOfStream,
        boolean flush,
        int numMessages);
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
  // transportTracer is nullable until it is integrated with client transports
  private boolean closed;

  // Tracing and stats-related states
  private int messagesBuffered;
  private int currentMessageSeqNo = -1;
  private long currentMessageWireSize;

  /**
   * Creates a {@code MessageFramer}.
   *
   * @param sink the sink used to deliver frames to the transport
   * @param bufferAllocator allocates buffers that the transport can commit to the wire.
   */
  public MessageFramer(
      Sink sink, WritableBufferAllocator bufferAllocator, StatsTraceContext statsTraceCtx) {
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
    messagesBuffered++;
    currentMessageSeqNo++;
    currentMessageWireSize = 0;
    statsTraceCtx.outboundMessage(currentMessageSeqNo);
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
    statsTraceCtx.outboundWireSize(currentMessageWireSize);
    statsTraceCtx.outboundMessageSent(currentMessageSeqNo, currentMessageWireSize, written);
  }

  private int writeUncompressed(InputStream message, int messageLength) throws IOException {
    if (messageLength != -1) {
      currentMessageWireSize = messageLength;
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
    // The final message may not be completely written because we do not flush the last buffer.
    // Do not report the last message as sent.
    sink.deliverFrame(writeableHeader, false, false, messagesBuffered - 1);
    messagesBuffered = 1;
    // Commit all except the last buffer to the sink
    List<WritableBuffer> bufferList = bufferChain.bufferList;
    for (int i = 0; i < bufferList.size() - 1; i++) {
      sink.deliverFrame(bufferList.get(i), false, false, 0);
    }
    // Assign the current buffer to the last in the chain so it can be used
    // for future writes or written with end-of-stream=true on close.
    buffer = bufferList.get(bufferList.size() - 1);
    currentMessageWireSize = messageLength;
  }

  private static int writeToOutputStream(InputStream message, OutputStream outputStream)
      throws IOException {
    if (message instanceof Drainable) {
      return ((Drainable) message).drainTo(outputStream);
    } else {
      // This makes an unnecessary copy of the bytes when bytebuf supports array(). However, we
      // expect performance-critical code to support flushTo().
      long written = IoUtils.copy(message, outputStream);
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
    sink.deliverFrame(buf, endOfStream, flush, messagesBuffered);
    messagesBuffered = 0;
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
    private final List<WritableBuffer> bufferList = new ArrayList<>();
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
