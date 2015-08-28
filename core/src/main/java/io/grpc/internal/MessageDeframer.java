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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.Preconditions;

import io.grpc.Codec;
import io.grpc.Decompressor;
import io.grpc.Status;

import java.io.Closeable;
import java.io.FilterInputStream;
import java.io.IOException;
import java.io.InputStream;

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

  /**
   * A listener of deframing events.
   */
  public interface Listener {

    /**
     * Called when the given number of bytes has been read from the input source of the deframer.
     * This is typically used to indicate to the underlying transport that more data can be
     * accepted.
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
  private final int maxMessageSize;
  private Decompressor decompressor;
  private State state = State.HEADER;
  private int requiredLength = HEADER_LENGTH;
  private boolean compressedFlag;
  private boolean endOfStream;
  private CompositeReadableBuffer nextFrame;
  private CompositeReadableBuffer unprocessed = new CompositeReadableBuffer();
  private long pendingDeliveries;
  private boolean deliveryStalled = true;
  private boolean inDelivery = false;

  /**
   * Create a deframer.
   *
   * @param listener listener for deframer events.
   * @param decompressor the compression used if a compressed frame is encountered, with
   *  {@code NONE} meaning unsupported
   * @param maxMessageSize the maximum allowed size for received messages.
   */
  public MessageDeframer(Listener listener, Decompressor decompressor, int maxMessageSize) {
    this.listener = Preconditions.checkNotNull(listener, "sink");
    this.decompressor = Preconditions.checkNotNull(decompressor, "decompressor");
    this.maxMessageSize = maxMessageSize;
  }

  /**
   * Sets the decompressor available to use.  The message encoding for the stream comes later in
   * time, and thus will not be available at the time of construction.  This should only be set
   * once, since the compression codec cannot change after the headers have been sent.
   *
   * @param decompressor the decompressing wrapper.
   */
  public void setDecompressor(Decompressor decompressor) {
    this.decompressor = checkNotNull(decompressor, "Can't pass an empty decompressor");
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
   * Adds the given data to this deframer and attempts delivery to the listener.
   *
   * @param data the raw data read from the remote endpoint. Must be non-null.
   * @param endOfStream if {@code true}, indicates that {@code data} is the end of the stream from
   *        the remote endpoint.  End of stream should not be used in the event of a transport
   *        error, such as a stream reset.
   * @throws IllegalStateException if {@link #close()} has been called previously or if
   *         this method has previously been called with {@code endOfStream=true}.
   */
  public void deframe(ReadableBuffer data, boolean endOfStream) {
    Preconditions.checkNotNull(data, "data");
    boolean needToCloseData = true;
    try {
      checkNotClosed();
      Preconditions.checkState(!this.endOfStream, "Past end of stream");

      unprocessed.addBuffer(data);
      needToCloseData = false;

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
   * Indicates whether delivery is currently stalled, pending receipt of more data.  This means
   * that no additional data can be delivered to the application.
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
   * Reads and delivers as many messages to the listener as possible.
   */
  private void deliver() {
    // We can have reentrancy here when using a direct executor, triggered by calls to
    // request more messages. This is safe as we simply loop until pendingDelivers = 0
    if (inDelivery) {
      return;
    }
    inDelivery = true;
    try {
      // Process the uncompressed bytes.
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

      /*
       * We are stalled when there are no more bytes to process. This allows delivering errors as
       * soon as the buffered input has been consumed, independent of whether the application
       * has requested another message.  At this point in the function, either all frames have been
       * delivered, or unprocessed is empty.  If there is a partial message, it will be inside next
       * frame and not in unprocessed.  If there is extra data but no pending deliveries, it will
       * be in unprocessed.
       */
      boolean stalled = unprocessed.readableBytes() == 0;

      if (endOfStream && stalled) {
        boolean havePartialMessage = nextFrame != null && nextFrame.readableBytes() > 0;
        if (!havePartialMessage) {
          listener.endOfStream();
          deliveryStalled = false;
          return;
        } else {
          // We've received the entire stream and have data available but we don't have
          // enough to read the next frame ... this is bad.
          throw Status.INTERNAL.withDescription("Encountered end-of-stream mid-frame")
              .asRuntimeException();
        }
      }

      // If we're transitioning to the stalled state, notify the listener.
      boolean previouslyStalled = deliveryStalled;
      deliveryStalled = stalled;
      if (stalled && !previouslyStalled) {
        listener.deliveryStalled();
      }
    } finally {
      inDelivery = false;
    }
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
    if (requiredLength < 0 || requiredLength > maxMessageSize) {
      throw Status.INTERNAL.withDescription(String.format("Frame size %d exceeds maximum: %d, ",
              requiredLength, maxMessageSize)).asRuntimeException();
    }

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
    if (decompressor == Codec.Identity.NONE) {
      throw Status.INTERNAL.withDescription(
          "Can't decode compressed frame as compression not configured.").asRuntimeException();
    }

    try {
      // Enforce the maxMessageSize limit on the returned stream.
      return new SizeEnforcingInputStream(decompressor.decompress(
              ReadableBuffers.openStream(nextFrame, true)));
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }

  /**
   * An {@link InputStream} that enforces the {@link #maxMessageSize} limit for compressed frames.
   */
  private final class SizeEnforcingInputStream extends FilterInputStream {
    private long count;
    private long mark = -1;

    public SizeEnforcingInputStream(InputStream in) {
      super(in);
    }

    @Override
    public int read() throws IOException {
      int result = in.read();
      if (result != -1) {
        count++;
      }
      verifySize();
      return result;
    }

    @Override
    public int read(byte[] b, int off, int len) throws IOException {
      int result = in.read(b, off, len);
      if (result != -1) {
        count += result;
      }
      verifySize();
      return result;
    }

    @Override
    public long skip(long n) throws IOException {
      long result = in.skip(n);
      count += result;
      verifySize();
      return result;
    }

    @Override
    public synchronized void mark(int readlimit) {
      in.mark(readlimit);
      mark = count;
      // it's okay to mark even if mark isn't supported, as reset won't work
    }

    @Override
    public synchronized void reset() throws IOException {
      if (!in.markSupported()) {
        throw new IOException("Mark not supported");
      }
      if (mark == -1) {
        throw new IOException("Mark not set");
      }

      in.reset();
      count = mark;
    }

    private void verifySize() {
      if (count > maxMessageSize) {
        throw Status.INTERNAL.withDescription(String.format(
                "Compressed frame exceeds maximum frame size: %d. Bytes read: %d",
                maxMessageSize, count)).asRuntimeException();
      }
    }
  }
}
