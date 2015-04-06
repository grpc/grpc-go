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

import com.google.common.base.Preconditions;

import io.grpc.Status;

import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;
import java.util.zip.GZIPInputStream;

import javax.annotation.concurrent.NotThreadSafe;

/**
 * Deframer for GRPC frames.
 *
 * <p>This class is not thread-safe. All calls to public methods should be made in the transport
 * thread.
 */
@NotThreadSafe
public class MessageDeframer implements Closeable {
  private static final int HEADER_LENGTH = 5;
  private static final int COMPRESSED_FLAG_MASK = 1;
  private static final int RESERVED_MASK = 0xFE;

  public enum Compression {
    NONE, GZIP
  }

  /**
   * A listener of deframing events.
   */
  public interface Listener {

    /**
     * Called when the given number of bytes has been read from the input source of the deframer.
     *
     * @param numBytes the number of bytes read from the deframer's input source.
     */
    void bytesRead(int numBytes);

    /**
     * Called to deliver the next complete message.
     *
     * @param is stream containing the message.
     */
    void messageRead(InputStream is);

    /**
     * Called when end-of-stream has not yet been reached but there are no complete messages
     * remaining to be delivered.
     */
    void deliveryStalled();

    /**
     * Called when the stream is complete and all messages have been successfully delivered.
     */
    void endOfStream();
  }

  private enum State {
    HEADER, BODY
  }

  private final Listener listener;
  private final Compression compression;
  private State state = State.HEADER;
  private int requiredLength = HEADER_LENGTH;
  private boolean compressedFlag;
  private boolean endOfStream;
  private CompositeReadableBuffer nextFrame;
  private CompositeReadableBuffer unprocessed = new CompositeReadableBuffer();
  private long pendingDeliveries;
  private boolean deliveryStalled = true;

  /**
   * Creates a deframer. Compression will not be supported.
   *
   * @param listener listener for deframer events.
   */
  public MessageDeframer(Listener listener) {
    this(listener, Compression.NONE);
  }

  /**
   * Create a deframer.
   *
   * @param listener listener for deframer events.
   * @param compression the compression used if a compressed frame is encountered, with {@code NONE}
   *        meaning unsupported
   */
  public MessageDeframer(Listener listener, Compression compression) {
    this.listener = Preconditions.checkNotNull(listener, "sink");
    this.compression = Preconditions.checkNotNull(compression, "compression");
  }

  /**
   * Requests up to the given number of messages from the call to be delivered to
   * {@link Listener#messageRead(InputStream)}. No additional messages will be delivered.
   *
   * <p>If {@link #close()} has been called, this method will have no effect.
   *
   * @param numMessages the requested number of messages to be delivered to the listener.
   */
  public void request(int numMessages) {
    Preconditions.checkArgument(numMessages > 0, "numMessages must be > 0");
    if (isClosed()) {
      return;
    }
    pendingDeliveries += numMessages;
    deliver();
  }

  /**
   * Adds the given data to this deframer and attempts delivery to the sink.
   *
   * @param data the raw data read from the remote endpoint. Must be non-null.
   * @param endOfStream if {@code true}, indicates that {@code data} is the end of the stream from
   *        the remote endpoint.
   * @throws IllegalStateException if {@link #close()} has been called previously or if
   *         {@link #deframe(ReadableBuffer, boolean)} has previously been called with
   *         {@code endOfStream=true}.
   */
  public void deframe(ReadableBuffer data, boolean endOfStream) {
    Preconditions.checkNotNull(data, "data");
    boolean needToCloseData = true;
    try {
      checkNotClosed();
      Preconditions.checkState(!this.endOfStream, "Past end of stream");

      needToCloseData = false;
      unprocessed.addBuffer(data);

      // Indicate that all of the data for this stream has been received.
      this.endOfStream = endOfStream;
      deliver();
    } finally {
      if (needToCloseData) {
        data.close();
      }
    }
  }

  /**
   * Indicates whether delivery is currently stalled, pending receipt of more data.
   */
  public boolean isStalled() {
    return deliveryStalled;
  }

  /**
   * Closes this deframer and frees any resources. After this method is called, additional
   * calls will have no effect.
   */
  @Override
  public void close() {
    try {
      if (unprocessed != null) {
        unprocessed.close();
      }
      if (nextFrame != null) {
        nextFrame.close();
      }
    } finally {
      unprocessed = null;
      nextFrame = null;
    }
  }

  /**
   * Indicates whether or not this deframer has been closed.
   */
  public boolean isClosed() {
    return unprocessed == null;
  }

  /**
   * Throws if this deframer has already been closed.
   */
  private void checkNotClosed() {
    Preconditions.checkState(!isClosed(), "MessageDeframer is already closed");
  }

  /**
   * Reads and delivers as many messages to the sink as possible.
   */
  private void deliver() {
    // Process the uncompressed bytes.
    boolean stalled = false;
    while (pendingDeliveries > 0 && readRequiredBytes()) {
      switch (state) {
        case HEADER:
          processHeader();
          break;
        case BODY:
          // Read the body and deliver the message.
          processBody();

          // Since we've delivered a message, decrement the number of pending
          // deliveries remaining.
          pendingDeliveries--;
          break;
        default:
          throw new AssertionError("Invalid state: " + state);
      }
    }
    // We are stalled when there are no more bytes to process. This allows delivering errors as soon
    // as the buffered input has been consumed, independent of whether the application has requested
    // another message.
    stalled = !isDataAvailable();

    if (endOfStream) {
      if (!isDataAvailable()) {
        listener.endOfStream();
      } else if (stalled) {
        // We've received the entire stream and have data available but we don't have
        // enough to read the next frame ... this is bad.
        throw Status.INTERNAL.withDescription("Encountered end-of-stream mid-frame")
            .asRuntimeException();
      }
    }

    // Never indicate that we're stalled if we've received all the data for the stream.
    stalled &= !endOfStream;

    // If we're transitioning to the stalled state, notify the listener.
    boolean previouslyStalled = deliveryStalled;
    deliveryStalled = stalled;
    if (stalled && !previouslyStalled) {
      listener.deliveryStalled();
    }
  }

  private boolean isDataAvailable() {
    return unprocessed.readableBytes() > 0 || (nextFrame != null && nextFrame.readableBytes() > 0);
  }

  /**
   * Attempts to read the required bytes into nextFrame.
   *
   * @return {@code true} if all of the required bytes have been read.
   */
  private boolean readRequiredBytes() {
    int totalBytesRead = 0;
    try {
      if (nextFrame == null) {
        nextFrame = new CompositeReadableBuffer();
      }

      // Read until the buffer contains all the required bytes.
      int missingBytes;
      while ((missingBytes = requiredLength - nextFrame.readableBytes()) > 0) {
        if (unprocessed.readableBytes() == 0) {
          // No more data is available.
          return false;
        }
        int toRead = Math.min(missingBytes, unprocessed.readableBytes());
        totalBytesRead += toRead;
        nextFrame.addBuffer(unprocessed.readBytes(toRead));
      }
      return true;
    } finally {
      if (totalBytesRead > 0) {
        listener.bytesRead(totalBytesRead);
      }
    }
  }

  /**
   * Processes the GRPC compression header which is composed of the compression flag and the outer
   * frame length.
   */
  private void processHeader() {
    int type = nextFrame.readUnsignedByte();
    if ((type & RESERVED_MASK) != 0) {
      throw Status.INTERNAL.withDescription("Frame header malformed: reserved bits not zero")
          .asRuntimeException();
    }
    compressedFlag = (type & COMPRESSED_FLAG_MASK) != 0;

    // Update the required length to include the length of the frame.
    requiredLength = nextFrame.readInt();

    // Continue reading the frame body.
    state = State.BODY;
  }

  /**
   * Processes the body of the GRPC compression frame. A single compression frame may contain
   * several GRPC messages within it.
   */
  private void processBody() {
    InputStream stream = compressedFlag ? getCompressedBody() : getUncompressedBody();
    nextFrame = null;
    listener.messageRead(stream);

    // Done with this frame, begin processing the next header.
    state = State.HEADER;
    requiredLength = HEADER_LENGTH;
  }

  private InputStream getUncompressedBody() {
    return ReadableBuffers.openStream(nextFrame, true);
  }

  private InputStream getCompressedBody() {
    if (compression == Compression.NONE) {
      throw Status.INTERNAL.withDescription(
          "Can't decode compressed frame as compression not configured.").asRuntimeException();
    }

    if (compression != Compression.GZIP) {
      throw new AssertionError("Unknown compression type");
    }

    try {
      return new GZIPInputStream(ReadableBuffers.openStream(nextFrame, true));
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }
}
