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

package io.grpc.transport;

import static java.lang.Math.min;

import com.google.common.base.Preconditions;
import com.google.common.io.ByteStreams;

import io.grpc.DeferredInputStream;
import io.grpc.KnownLength;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.util.ArrayList;
import java.util.List;
import java.util.zip.GZIPOutputStream;

/**
 * Encodes gRPC messages to be delivered via the transport layer which implements {@link
 * MessageFramer.Sink}.
 */
public class MessageFramer {
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
    void deliverFrame(WritableBuffer frame, boolean endOfStream, boolean flush);
  }

  private static final int HEADER_LENGTH = 5;
  private static final byte UNCOMPRESSED = 0;
  private static final byte COMPRESSED = 1;

  public enum Compression {
    NONE, GZIP;
  }

  private final Sink sink;
  private WritableBuffer buffer;
  private final Compression compression;
  private final OutputStreamAdapter outputStreamAdapter = new OutputStreamAdapter();
  private final byte[] headerScratch = new byte[HEADER_LENGTH];
  private final WritableBufferAllocator bufferAllocator;
  private boolean closed;

  /**
   * Creates a {@code MessageFramer} without compression.
   *
   * @param sink the sink used to deliver frames to the transport
   * @param bufferAllocator allocates buffers that the transport can commit to the wire.
   */
  public MessageFramer(Sink sink, WritableBufferAllocator bufferAllocator) {
    this(sink, bufferAllocator, Compression.NONE);
  }

  /**
   * Creates a {@code MessageFramer}.
   *
   * @param sink the sink used to deliver frames to the transport
   * @param bufferAllocator allocates buffers that the transport can commit to the wire.
   * @param compression the compression type
   */
  public MessageFramer(Sink sink, WritableBufferAllocator bufferAllocator,
                       Compression compression) {
    this.sink = Preconditions.checkNotNull(sink, "sink");
    this.bufferAllocator = bufferAllocator;
    this.compression = Preconditions.checkNotNull(compression, "compression");
  }

  /**
   * Writes out a payload message.
   *
   * @param message contains the message to be written out. It will be completely consumed.
   */
  public void writePayload(InputStream message) {
    verifyNotClosed();
    try {
      switch (compression) {
        case NONE:
          int messageLength = getKnownLength(message);
          if (messageLength != -1) {
            writeKnownLength(message, messageLength, false);
          } else {
            BufferChainOutputStream bufferChain = new BufferChainOutputStream();
            writeToOutputStream(message, bufferChain);
            writeBufferChain(bufferChain, false);
          }
          break;
        case GZIP:
          BufferChainOutputStream bufferChain = new BufferChainOutputStream();
          gzipCompressTo(message, bufferChain);
          writeBufferChain(bufferChain, true);
          break;
        default:
          throw new AssertionError("Unknown compression type");
      }
    } catch (IOException ex) {
      throw new RuntimeException(ex);
    }
  }

  private int getKnownLength(InputStream inputStream) throws IOException {
    if (inputStream instanceof KnownLength || inputStream instanceof ByteArrayInputStream) {
      return inputStream.available();
    }
    return -1;
  }

  private static void gzipCompressTo(InputStream in, OutputStream out)
      throws IOException {
    int messageLength = -1;
    if (in instanceof KnownLength) {
      messageLength = in.available();
    }
    GZIPOutputStream compressingStream = new GZIPOutputStream(out);
    try {
      long written = writeToOutputStream(in, compressingStream);
      if (messageLength != -1 && messageLength != written) {
        throw new RuntimeException("Message length was inaccurate");
      }
    } finally {
      compressingStream.close();
    }
  }

  /**
   * Write an unserialized message with a known length.
   */
  private void writeKnownLength(InputStream message, int messageLength, boolean compressed)
      throws IOException {
    ByteBuffer header = ByteBuffer.wrap(headerScratch);
    header.put(compressed ? COMPRESSED : UNCOMPRESSED);
    header.putInt(messageLength);
    // Allocate the initial buffer chunk based on frame header + payload length.
    // Note that the allocator may allocate a buffer larger or smaller than this length
    if (buffer == null) {
      buffer = bufferAllocator.allocate(header.position() + messageLength);
    }
    writeRaw(headerScratch, 0, header.position());
    long written = writeToOutputStream(message, outputStreamAdapter);
    if (messageLength != written) {
      throw new RuntimeException("Message length was inaccurate");
    }
  }

  /**
   * Write a message that has been serialized to a sequence of buffers.
   */
  private void writeBufferChain(BufferChainOutputStream bufferChain, boolean compressed)
      throws IOException {
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
    buffer =  bufferList.get(bufferList.size() - 1);
  }

  @SuppressWarnings("rawtypes")
  private static long writeToOutputStream(InputStream message, OutputStream outputStream)
      throws IOException {
    if (message instanceof DeferredInputStream) {
      return ((DeferredInputStream) message).flushTo(outputStream);
    } else {
      // This makes an unnecessary copy of the bytes when bytebuf supports array(). However, we
      // expect performance-critical code to support flushTo().
      return ByteStreams.copy(message, outputStream);
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
  public void flush() {
    if (buffer != null && buffer.readableBytes() > 0) {
      commitToSink(false, true);
    }
  }

  /**
   * Indicates whether or not this framer has been closed via a call to either
   * {@link #close()} or {@link #dispose()}.
   */
  public boolean isClosed() {
    return closed;
  }

  /**
   * Flushes and closes the framer and releases any buffers. After the framer is closed or
   * disposed, additional calls to this method will have no affect.
   */
  public void close() {
    if (!isClosed()) {
      closed = true;
      // With the current code we don't expect readableBytes > 0 to be possible here, added
      // defensively to prevent buffer leak issues if the framer code changes later.
      if (buffer != null && buffer.readableBytes() == 0) {
        buffer.release();
        buffer = null;
      }
      commitToSink(true, true);
    }
  }

  /**
   * Closes the framer and releases any buffers, but does not flush. After the framer is
   * closed or disposed, additional calls to this method will have no affect.
   */
  public void dispose() {
    closed = true;
    if (buffer != null) {
      buffer.release();
      buffer = null;
    }
  }

  private void commitToSink(boolean endOfStream, boolean flush) {
    sink.deliverFrame(buffer, endOfStream, flush);
    buffer = null;
  }

  private void verifyNotClosed() {
    if (isClosed()) {
      throw new IllegalStateException("Framer already closed");
    }
  }

  /** OutputStream whose write()s are passed to the framer. */
  private class OutputStreamAdapter extends OutputStream {
    private final byte[] singleByte = new byte[1];

    @Override
    public void write(int b) {
      singleByte[0] = (byte) b;
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
  private class BufferChainOutputStream extends OutputStream {

    private final byte[] singleByte = new byte[1];
    private List<WritableBuffer> bufferList;
    private WritableBuffer current;

    private BufferChainOutputStream() {
      bufferList = new ArrayList<WritableBuffer>();
    }

    @Override
    public void write(int b) throws IOException {
      singleByte[0] = (byte) b;
      write(singleByte, 0, 1);
    }

    @Override
    public void write(byte[] b, int off, int len) throws IOException {
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
          current = bufferAllocator.allocate(current.readableBytes() * 2);
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
