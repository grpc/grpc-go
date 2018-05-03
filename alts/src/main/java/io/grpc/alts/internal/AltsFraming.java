/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts.internal;

import com.google.common.base.Preconditions;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.security.GeneralSecurityException;

/** Framing and deframing methods and classes used by handshaker. */
public final class AltsFraming {
  // The size of the frame field. Must correspond to the size of int, 4 bytes.
  // Left package-private for testing.
  private static final int FRAME_LENGTH_HEADER_SIZE = 4;
  private static final int FRAME_MESSAGE_TYPE_HEADER_SIZE = 4;
  private static final int MAX_DATA_LENGTH = 1024 * 1024;
  private static final int INITIAL_BUFFER_CAPACITY = 1024 * 64;

  // TODO: Make this the responsibility of the caller.
  private static final int MESSAGE_TYPE = 6;

  private AltsFraming() {}

  static int getFrameLengthHeaderSize() {
    return FRAME_LENGTH_HEADER_SIZE;
  }

  static int getFrameMessageTypeHeaderSize() {
    return FRAME_MESSAGE_TYPE_HEADER_SIZE;
  }

  static int getMaxDataLength() {
    return MAX_DATA_LENGTH;
  }

  static int getFramingOverhead() {
    return FRAME_LENGTH_HEADER_SIZE + FRAME_MESSAGE_TYPE_HEADER_SIZE;
  }

  /**
   * Creates a frame of length dataSize + FRAME_HEADER_SIZE using the input bytes, if dataSize <=
   * input.remaining(). Otherwise, a frame of length input.remaining() + FRAME_HEADER_SIZE is
   * created.
   */
  static ByteBuffer toFrame(ByteBuffer input, int dataSize) throws GeneralSecurityException {
    Preconditions.checkNotNull(input);
    if (dataSize > input.remaining()) {
      dataSize = input.remaining();
    }
    Producer producer = new Producer();
    ByteBuffer inputAlias = input.duplicate();
    inputAlias.limit(input.position() + dataSize);
    producer.readBytes(inputAlias);
    producer.flush();
    input.position(inputAlias.position());
    ByteBuffer output = producer.getRawFrame();
    return output;
  }

  /**
   * A helper class to write a frame.
   *
   * <p>This class guarantees that one of the following is true:
   *
   * <ul>
   *   <li>readBytes will read from the input
   *   <li>writeBytes will write to the output
   * </ul>
   *
   * <p>Sample usage:
   *
   * <pre>{@code
   * Producer producer = new Producer();
   * ByteBuffer inputBuffer = readBytesFromMyStream();
   * ByteBuffer outputBuffer = writeBytesToMyStream();
   * while (inputBuffer.hasRemaining() || outputBuffer.hasRemaining()) {
   *   producer.readBytes(inputBuffer);
   *   producer.writeBytes(outputBuffer);
   * }
   * }</pre>
   *
   * <p>Alternatively, this class guarantees that one of the following is true:
   *
   * <ul>
   *   <li>readBytes will read from the input
   *   <li>{@code isComplete()} returns true and {@code getByteBuffer()} returns the contents of a
   *       processed frame.
   * </ul>
   *
   * <p>Sample usage:
   *
   * <pre>{@code
   * Producer producer = new Producer();
   * while (!producer.isComplete()) {
   *   ByteBuffer inputBuffer = readBytesFromMyStream();
   *   producer.readBytes(inputBuffer);
   * }
   * producer.flush();
   * ByteBuffer outputBuffer = producer.getRawFrame();
   * }</pre>
   */
  static final class Producer {
    private ByteBuffer buffer;
    private boolean isComplete;

    Producer(int maxFrameSize) {
      buffer = ByteBuffer.allocate(maxFrameSize);
      reset();
      Preconditions.checkArgument(maxFrameSize > getFramePrefixLength() + getFrameSuffixLength());
    }

    Producer() {
      this(INITIAL_BUFFER_CAPACITY);
    }

    /** The length of the frame prefix data, including the message length/type fields. */
    int getFramePrefixLength() {
      int result = FRAME_LENGTH_HEADER_SIZE + FRAME_MESSAGE_TYPE_HEADER_SIZE;
      return result;
    }

    int getFrameSuffixLength() {
      return 0;
    }

    /**
     * Reads bytes from input, parsing them into a frame. Returns false if and only if more data is
     * needed. To obtain a full frame this method must be called repeatedly until it returns true.
     */
    boolean readBytes(ByteBuffer input) throws GeneralSecurityException {
      Preconditions.checkNotNull(input);
      if (isComplete) {
        return true;
      }
      copy(buffer, input);
      if (!buffer.hasRemaining()) {
        flush();
      }
      return isComplete;
    }

    /**
     * Completes the current frame, signaling that no further data is available to be passed to
     * readBytes and that the client requires writeBytes to start returning data. isComplete() is
     * guaranteed to return true after this call.
     */
    void flush() throws GeneralSecurityException {
      if (isComplete) {
        return;
      }
      // Get the length of the complete frame.
      int frameLength = buffer.position() + getFrameSuffixLength();

      // Set the limit and move to the start.
      buffer.flip();

      // Advance the limit to allow a crypto suffix.
      buffer.limit(buffer.limit() + getFrameSuffixLength());

      // Write the data length and the message type.
      int dataLength = frameLength - FRAME_LENGTH_HEADER_SIZE;
      buffer.order(ByteOrder.LITTLE_ENDIAN);
      buffer.putInt(dataLength);
      buffer.putInt(MESSAGE_TYPE);

      // Move the position back to 0, the frame is ready.
      buffer.position(0);
      isComplete = true;
    }

    /** Resets the state, preparing to construct a new frame. Must be called between frames. */
    private void reset() {
      buffer.clear();

      // Save some space for framing, we'll fill that in later.
      buffer.position(getFramePrefixLength());
      buffer.limit(buffer.limit() - getFrameSuffixLength());

      isComplete = false;
    }

    /**
     * Returns a ByteBuffer containing a complete raw frame, if it's available. Should only be
     * called when isComplete() returns true, otherwise null is returned. The returned object
     * aliases the internal buffer, that is, it shares memory with the internal buffer. No further
     * operations are permitted on this object until the caller has processed the data it needs from
     * the returned byte buffer.
     */
    ByteBuffer getRawFrame() {
      if (!isComplete) {
        return null;
      }
      ByteBuffer result = buffer.duplicate();
      reset();
      return result;
    }
  }

  /**
   * A helper class to read a frame.
   *
   * <p>This class guarantees that one of the following is true:
   *
   * <ul>
   *   <li>readBytes will read from the input
   *   <li>writeBytes will write to the output
   * </ul>
   *
   * <p>Sample usage:
   *
   * <pre>{@code
   * Parser parser = new Parser();
   * ByteBuffer inputBuffer = readBytesFromMyStream();
   * ByteBuffer outputBuffer = writeBytesToMyStream();
   * while (inputBuffer.hasRemaining() || outputBuffer.hasRemaining()) {
   *   parser.readBytes(inputBuffer);
   *   parser.writeBytes(outputBuffer); }
   * }</pre>
   *
   * <p>Alternatively, this class guarantees that one of the following is true:
   *
   * <ul>
   *   <li>readBytes will read from the input
   *   <li>{@code isComplete()} returns true and {@code getByteBuffer()} returns the contents of a
   *       processed frame.
   * </ul>
   *
   * <p>Sample usage:
   *
   * <pre>{@code
   * Parser parser = new Parser();
   * while (!parser.isComplete()) {
   *   ByteBuffer inputBuffer = readBytesFromMyStream();
   *   parser.readBytes(inputBuffer);
   * }
   * ByteBuffer outputBuffer = parser.getRawFrame();
   * }</pre>
   */
  public static final class Parser {
    private ByteBuffer buffer = ByteBuffer.allocate(INITIAL_BUFFER_CAPACITY);
    private boolean isComplete = false;

    public Parser() {
      Preconditions.checkArgument(
          INITIAL_BUFFER_CAPACITY > getFramePrefixLength() + getFrameSuffixLength());
    }

    /**
     * Reads bytes from input, parsing them into a frame. Returns false if and only if more data is
     * needed. To obtain a full frame this method must be called repeatedly until it returns true.
     */
    public boolean readBytes(ByteBuffer input) throws GeneralSecurityException {
      Preconditions.checkNotNull(input);

      if (isComplete) {
        return true;
      }

      // Read enough bytes to determine the length
      while (buffer.position() < FRAME_LENGTH_HEADER_SIZE && input.hasRemaining()) {
        buffer.put(input.get());
      }

      // If we have enough bytes to determine the length, read the length and ensure that our
      // internal buffer is large enough.
      if (buffer.position() == FRAME_LENGTH_HEADER_SIZE && input.hasRemaining()) {
        ByteBuffer bufferAlias = buffer.duplicate();
        bufferAlias.flip();
        bufferAlias.order(ByteOrder.LITTLE_ENDIAN);
        int dataLength = bufferAlias.getInt();
        if (dataLength < FRAME_MESSAGE_TYPE_HEADER_SIZE || dataLength > MAX_DATA_LENGTH) {
          throw new IllegalArgumentException("Invalid frame length " + dataLength);
        }
        // Maybe resize the buffer
        int frameLength = dataLength + FRAME_LENGTH_HEADER_SIZE;
        if (buffer.capacity() < frameLength) {
          buffer = ByteBuffer.allocate(frameLength);
          buffer.order(ByteOrder.LITTLE_ENDIAN);
          buffer.putInt(dataLength);
        }
        buffer.limit(frameLength);
      }

      // TODO: Similarly extract and check message type.

      // Read the remaining data into the internal buffer.
      copy(buffer, input);
      if (!buffer.hasRemaining()) {
        buffer.flip();
        isComplete = true;
      }
      return isComplete;
    }

    /** The length of the frame prefix data, including the message length/type fields. */
    int getFramePrefixLength() {
      int result = FRAME_LENGTH_HEADER_SIZE + FRAME_MESSAGE_TYPE_HEADER_SIZE;
      return result;
    }

    int getFrameSuffixLength() {
      return 0;
    }

    /** Returns true if we've parsed a complete frame. */
    public boolean isComplete() {
      return isComplete;
    }

    /** Resets the state, preparing to parse a new frame. Must be called between frames. */
    private void reset() {
      buffer.clear();
      isComplete = false;
    }

    /**
     * Returns a ByteBuffer containing a complete raw frame, if it's available. Should only be
     * called when isComplete() returns true, otherwise null is returned. The returned object
     * aliases the internal buffer, that is, it shares memory with the internal buffer. No further
     * operations are permitted on this object until the caller has processed the data it needs from
     * the returned byte buffer.
     */
    public ByteBuffer getRawFrame() {
      if (!isComplete) {
        return null;
      }
      ByteBuffer result = buffer.duplicate();
      reset();
      return result;
    }
  }

  /**
   * Copy as much as possible to dst from src. Unlike {@link ByteBuffer#put(ByteBuffer)}, this stops
   * early if there is no room left in dst.
   */
  private static void copy(ByteBuffer dst, ByteBuffer src) {
    if (dst.hasRemaining() && src.hasRemaining()) {
      // Avoid an allocation if possible.
      if (dst.remaining() >= src.remaining()) {
        dst.put(src);
      } else {
        int count = Math.min(dst.remaining(), src.remaining());
        ByteBuffer slice = src.slice();
        slice.limit(count);
        dst.put(slice);
        src.position(src.position() + count);
      }
    }
  }
}
