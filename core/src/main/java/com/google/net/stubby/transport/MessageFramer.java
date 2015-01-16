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

package com.google.net.stubby.transport;

import com.google.common.base.Preconditions;
import com.google.common.io.ByteStreams;
import com.google.net.stubby.DeferredInputStream;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.util.zip.GZIPOutputStream;

/**
 * Encodes gRPC messages to be delivered via the transport layer which implements {@link
 * MessageFramer.Sink}.
 */
public class MessageFramer {
  /**
   * Sink implemented by the transport layer to receive frames and forward them to their destination
   */
  public interface Sink<T> {
    /**
     * Deliver a frame via the transport.
     *
     * @param frame the contents of the frame to deliver
     * @param endOfStream whether the frame is the last one for the GRPC stream
     */
    public void deliverFrame(T frame, boolean endOfStream);
  }

  private static final int HEADER_LENGTH = 5;
  private static final byte UNCOMPRESSED = 0;
  private static final byte COMPRESSED = 1;

  public enum Compression {
    NONE, GZIP;
  }

  private final Sink<ByteBuffer> sink;
  private ByteBuffer bytebuf;
  private final Compression compression;
  private final OutputStreamAdapter outputStreamAdapter = new OutputStreamAdapter();
  private final byte[] headerScratch = new byte[HEADER_LENGTH];

  public MessageFramer(Sink<ByteBuffer> sink, int maxFrameSize) {
    this(sink, maxFrameSize, Compression.NONE);
  }

  public MessageFramer(Sink<ByteBuffer> sink, int maxFrameSize, Compression compression) {
    this.sink = Preconditions.checkNotNull(sink, "sink");
    this.bytebuf = ByteBuffer.allocate(maxFrameSize);
    this.compression = Preconditions.checkNotNull(compression, "compression");
  }

  /**
   * Write out a Payload message. {@code message} will be completely consumed.
   * {@code message.available()} must return the number of remaining bytes to be read.
   */
  public void writePayload(InputStream message, int messageLength) {
    try {
      if (compression == Compression.NONE) {
        writeFrame(message, messageLength, false);
      } else if (compression != Compression.GZIP) {
        throw new AssertionError("Unknown compression type");
      } else {
        // compression == GZIP
        DirectAccessByteArrayOutputStream out = new DirectAccessByteArrayOutputStream();
        gzipCompressTo(message, messageLength, out);
        InputStream compressedMessage =
            new DeferredByteArrayInputStream(out.getBuf(), 0, out.getCount());
        writeFrame(compressedMessage, out.getCount(), true);
      }
    } catch (IOException ex) {
      throw new RuntimeException(ex);
    }
  }

  private void gzipCompressTo(InputStream in, int messageLength, OutputStream out)
      throws IOException {
    GZIPOutputStream compressingStream = new GZIPOutputStream(out);
    try {
      long written = writeToOutputStream(in, compressingStream);
      if (messageLength != written) {
        throw new RuntimeException("Message length was inaccurate");
      }
    } finally {
      compressingStream.close();
    }
  }

  private void writeFrame(InputStream message, int messageLength, boolean compressed)
      throws IOException {
    verifyNotClosed();
    ByteBuffer header = ByteBuffer.wrap(headerScratch);
    header.put(compressed ? COMPRESSED : UNCOMPRESSED);
    header.putInt(messageLength);
    writeRaw(headerScratch, 0, header.position());
    long written = writeToOutputStream(message, outputStreamAdapter);
    if (messageLength != written) {
      throw new RuntimeException("Message length was inaccurate");
    }
  }

  @SuppressWarnings("rawtypes")
  private static long writeToOutputStream(InputStream message, OutputStream outputStream)
      throws IOException {
    if (message instanceof DeferredInputStream) {
      return ((DeferredInputStream) message).flushTo(outputStream);
    } else if (message instanceof DeferredByteArrayInputStream) {
      return ((DeferredByteArrayInputStream) message).flushTo(outputStream);
    } else {
      // This makes an unnecessary copy of the bytes when bytebuf supports array(). However, we
      // expect performance-critical code to support flushTo().
      return ByteStreams.copy(message, outputStream);
    }
  }

  private void writeRaw(byte[] b, int off, int len) {
    while (len > 0) {
      if (bytebuf.remaining() == 0) {
        commitToSink(false);
      }
      int toWrite = Math.min(len, bytebuf.remaining());
      bytebuf.put(b, off, toWrite);
      off += toWrite;
      len -= toWrite;
    }
  }

  /**
   * Flush any buffered data in the framer to the sink.
   */
  public void flush() {
    if (bytebuf.position() == 0) {
      return;
    }
    commitToSink(false);
  }

  /**
   * Indicates whether or not this framer has been closed via a call to either
   * {@link #close()} or {@link #dispose()}.
   */
  public boolean isClosed() {
    return bytebuf == null;
  }

  /**
   * Flushes and closes the framer and releases any buffers. After the framer is closed or
   * disposed, additional calls to this method will have no affect.
   */
  public void close() {
    if (!isClosed()) {
      commitToSink(true);
      dispose();
    }
  }

  /**
   * Closes the framer and releases any buffers, but does not flush. After the framer is
   * closed or disposed, additional calls to this method will have no affect.
   */
  public void dispose() {
    // TODO(user): Returning buffer to a pool would go here
    bytebuf = null;
  }

  private void commitToSink(boolean endOfStream) {
    bytebuf.flip();
    sink.deliverFrame(bytebuf, endOfStream);
    bytebuf.clear();
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
   * Implements the same general contract of DeferredInputStream, although is unable to extend it.
   */
  private static class DeferredByteArrayInputStream extends ByteArrayInputStream {
    public DeferredByteArrayInputStream(byte[] buf, int offset, int length) {
      super(buf, offset, length);
    }

    public int flushTo(OutputStream os) throws IOException {
      os.write(buf, pos, count - pos);
      return count - pos;
    }
  }

  private static class DirectAccessByteArrayOutputStream extends ByteArrayOutputStream {
    public byte[] getBuf() {
      return buf;
    }

    public int getCount() {
      return count;
    }
  }
}
