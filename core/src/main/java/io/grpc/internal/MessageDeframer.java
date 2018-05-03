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

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Codec;
import io.grpc.Decompressor;
import io.grpc.Status;
import java.io.Closeable;
import java.io.FilterInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.zip.DataFormatException;
import javax.annotation.Nullable;
import javax.annotation.concurrent.NotThreadSafe;

/**
 * Deframer for GRPC frames.
 *
 * <p>This class is not thread-safe. Unless otherwise stated, all calls to public methods should be
 * made in the deframing thread.
 */
@NotThreadSafe
public class MessageDeframer implements Closeable, Deframer {
  private static final int HEADER_LENGTH = 5;
  private static final int COMPRESSED_FLAG_MASK = 1;
  private static final int RESERVED_MASK = 0xFE;
  private static final int MAX_BUFFER_SIZE = 1024 * 1024 * 2;

  /**
   * A listener of deframing events. These methods will be invoked from the deframing thread.
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
     * @param producer single message producer wrapping the message.
     */
    void messagesAvailable(StreamListener.MessageProducer producer);

    /**
     * Called when the deframer closes.
     *
     * @param hasPartialMessage whether the deframer contained an incomplete message at closing.
     */
    void deframerClosed(boolean hasPartialMessage);

    /**
     * Called when a {@link #deframe(ReadableBuffer)} operation failed.
     *
     * @param cause the actual failure
     */
    void deframeFailed(Throwable cause);
  }

  private enum State {
    HEADER, BODY
  }

  private Listener listener;
  private int maxInboundMessageSize;
  private final StatsTraceContext statsTraceCtx;
  private final TransportTracer transportTracer;
  private Decompressor decompressor;
  private GzipInflatingBuffer fullStreamDecompressor;
  private byte[] inflatedBuffer;
  private int inflatedIndex;
  private State state = State.HEADER;
  private int requiredLength = HEADER_LENGTH;
  private boolean compressedFlag;
  private CompositeReadableBuffer nextFrame;
  private CompositeReadableBuffer unprocessed = new CompositeReadableBuffer();
  private long pendingDeliveries;
  private boolean inDelivery = false;
  private int currentMessageSeqNo = -1;
  private int inboundBodyWireSize;

  private boolean closeWhenComplete = false;
  private volatile boolean stopDelivery = false;

  /**
   * Create a deframer.
   *
   * @param listener listener for deframer events.
   * @param decompressor the compression used if a compressed frame is encountered, with
   *  {@code NONE} meaning unsupported
   * @param maxMessageSize the maximum allowed size for received messages.
   */
  public MessageDeframer(
      Listener listener,
      Decompressor decompressor,
      int maxMessageSize,
      StatsTraceContext statsTraceCtx,
      TransportTracer transportTracer) {
    this.listener = checkNotNull(listener, "sink");
    this.decompressor = checkNotNull(decompressor, "decompressor");
    this.maxInboundMessageSize = maxMessageSize;
    this.statsTraceCtx = checkNotNull(statsTraceCtx, "statsTraceCtx");
    this.transportTracer = checkNotNull(transportTracer, "transportTracer");
  }

  void setListener(Listener listener) {
    this.listener = listener;
  }

  @Override
  public void setMaxInboundMessageSize(int messageSize) {
    maxInboundMessageSize = messageSize;
  }

  @Override
  public void setDecompressor(Decompressor decompressor) {
    checkState(fullStreamDecompressor == null, "Already set full stream decompressor");
    this.decompressor = checkNotNull(decompressor, "Can't pass an empty decompressor");
  }

  @Override
  public void setFullStreamDecompressor(GzipInflatingBuffer fullStreamDecompressor) {
    checkState(decompressor == Codec.Identity.NONE, "per-message decompressor already set");
    checkState(this.fullStreamDecompressor == null, "full stream decompressor already set");
    this.fullStreamDecompressor =
        checkNotNull(fullStreamDecompressor, "Can't pass a null full stream decompressor");
    unprocessed = null;
  }

  @Override
  public void request(int numMessages) {
    checkArgument(numMessages > 0, "numMessages must be > 0");
    if (isClosed()) {
      return;
    }
    pendingDeliveries += numMessages;
    deliver();
  }

  @Override
  public void deframe(ReadableBuffer data) {
    checkNotNull(data, "data");
    boolean needToCloseData = true;
    try {
      if (!isClosedOrScheduledToClose()) {
        if (fullStreamDecompressor != null) {
          fullStreamDecompressor.addGzippedBytes(data);
        } else {
          unprocessed.addBuffer(data);
        }
        needToCloseData = false;

        deliver();
      }
    } finally {
      if (needToCloseData) {
        data.close();
      }
    }
  }

  @Override
  public void closeWhenComplete() {
    if (isClosed()) {
      return;
    } else if (isStalled()) {
      close();
    } else {
      closeWhenComplete = true;
    }
  }

  /**
   * Sets a flag to interrupt delivery of any currently queued messages. This may be invoked outside
   * of the deframing thread, and must be followed by a call to {@link #close()} in the deframing
   * thread. Without a subsequent call to {@link #close()}, the deframer may hang waiting for
   * additional messages before noticing that the {@code stopDelivery} flag has been set.
   */
  void stopDelivery() {
    stopDelivery = true;
  }

  @Override
  public void close() {
    if (isClosed()) {
      return;
    }
    boolean hasPartialMessage = nextFrame != null && nextFrame.readableBytes() > 0;
    try {
      if (fullStreamDecompressor != null) {
        hasPartialMessage = hasPartialMessage || fullStreamDecompressor.hasPartialData();
        fullStreamDecompressor.close();
      }
      if (unprocessed != null) {
        unprocessed.close();
      }
      if (nextFrame != null) {
        nextFrame.close();
      }
    } finally {
      fullStreamDecompressor = null;
      unprocessed = null;
      nextFrame = null;
    }
    listener.deframerClosed(hasPartialMessage);
  }

  /**
   * Indicates whether or not this deframer has been closed.
   */
  public boolean isClosed() {
    return unprocessed == null && fullStreamDecompressor == null;
  }

  /** Returns true if this deframer has already been closed or scheduled to close. */
  private boolean isClosedOrScheduledToClose() {
    return isClosed() || closeWhenComplete;
  }

  private boolean isStalled() {
    if (fullStreamDecompressor != null) {
      return fullStreamDecompressor.isStalled();
    } else {
      return unprocessed.readableBytes() == 0;
    }
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
      while (!stopDelivery && pendingDeliveries > 0 && readRequiredBytes()) {
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

      if (stopDelivery) {
        close();
        return;
      }

      /*
       * We are stalled when there are no more bytes to process. This allows delivering errors as
       * soon as the buffered input has been consumed, independent of whether the application
       * has requested another message.  At this point in the function, either all frames have been
       * delivered, or unprocessed is empty.  If there is a partial message, it will be inside next
       * frame and not in unprocessed.  If there is extra data but no pending deliveries, it will
       * be in unprocessed.
       */
      if (closeWhenComplete && isStalled()) {
        close();
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
    int deflatedBytesRead = 0;
    try {
      if (nextFrame == null) {
        nextFrame = new CompositeReadableBuffer();
      }

      // Read until the buffer contains all the required bytes.
      int missingBytes;
      while ((missingBytes = requiredLength - nextFrame.readableBytes()) > 0) {
        if (fullStreamDecompressor != null) {
          try {
            if (inflatedBuffer == null || inflatedIndex == inflatedBuffer.length) {
              inflatedBuffer = new byte[Math.min(missingBytes, MAX_BUFFER_SIZE)];
              inflatedIndex = 0;
            }
            int bytesToRead = Math.min(missingBytes, inflatedBuffer.length - inflatedIndex);
            int n = fullStreamDecompressor.inflateBytes(inflatedBuffer, inflatedIndex, bytesToRead);
            totalBytesRead += fullStreamDecompressor.getAndResetBytesConsumed();
            deflatedBytesRead += fullStreamDecompressor.getAndResetDeflatedBytesConsumed();
            if (n == 0) {
              // No more inflated data is available.
              return false;
            }
            nextFrame.addBuffer(ReadableBuffers.wrap(inflatedBuffer, inflatedIndex, n));
            inflatedIndex += n;
          } catch (IOException e) {
            throw new RuntimeException(e);
          } catch (DataFormatException e) {
            throw new RuntimeException(e);
          }
        } else {
          if (unprocessed.readableBytes() == 0) {
            // No more data is available.
            return false;
          }
          int toRead = Math.min(missingBytes, unprocessed.readableBytes());
          totalBytesRead += toRead;
          nextFrame.addBuffer(unprocessed.readBytes(toRead));
        }
      }
      return true;
    } finally {
      if (totalBytesRead > 0) {
        listener.bytesRead(totalBytesRead);
        if (state == State.BODY) {
          if (fullStreamDecompressor != null) {
            // With compressed streams, totalBytesRead can include gzip header and trailer metadata
            statsTraceCtx.inboundWireSize(deflatedBytesRead);
            inboundBodyWireSize += deflatedBytesRead;
          } else {
            statsTraceCtx.inboundWireSize(totalBytesRead);
            inboundBodyWireSize += totalBytesRead;
          }
        }
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
      throw Status.INTERNAL.withDescription(
          "gRPC frame header malformed: reserved bits not zero")
          .asRuntimeException();
    }
    compressedFlag = (type & COMPRESSED_FLAG_MASK) != 0;

    // Update the required length to include the length of the frame.
    requiredLength = nextFrame.readInt();
    if (requiredLength < 0 || requiredLength > maxInboundMessageSize) {
      throw Status.RESOURCE_EXHAUSTED.withDescription(
          String.format("gRPC message exceeds maximum size %d: %d",
              maxInboundMessageSize, requiredLength))
          .asRuntimeException();
    }

    currentMessageSeqNo++;
    statsTraceCtx.inboundMessage(currentMessageSeqNo);
    transportTracer.reportMessageReceived();
    // Continue reading the frame body.
    state = State.BODY;
  }

  /**
   * Processes the GRPC message body, which depending on frame header flags may be compressed.
   */
  private void processBody() {
    // There is no reliable way to get the uncompressed size per message when it's compressed,
    // because the uncompressed bytes are provided through an InputStream whose total size is
    // unknown until all bytes are read, and we don't know when it happens.
    statsTraceCtx.inboundMessageRead(currentMessageSeqNo, inboundBodyWireSize, -1);
    inboundBodyWireSize = 0;
    InputStream stream = compressedFlag ? getCompressedBody() : getUncompressedBody();
    nextFrame = null;
    listener.messagesAvailable(new SingleMessageProducer(stream));

    // Done with this frame, begin processing the next header.
    state = State.HEADER;
    requiredLength = HEADER_LENGTH;
  }

  private InputStream getUncompressedBody() {
    statsTraceCtx.inboundUncompressedSize(nextFrame.readableBytes());
    return ReadableBuffers.openStream(nextFrame, true);
  }

  private InputStream getCompressedBody() {
    if (decompressor == Codec.Identity.NONE) {
      throw Status.INTERNAL.withDescription(
          "Can't decode compressed gRPC message as compression not configured")
          .asRuntimeException();
    }

    try {
      // Enforce the maxMessageSize limit on the returned stream.
      InputStream unlimitedStream =
          decompressor.decompress(ReadableBuffers.openStream(nextFrame, true));
      return new SizeEnforcingInputStream(
          unlimitedStream, maxInboundMessageSize, statsTraceCtx);
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }

  /**
   * An {@link InputStream} that enforces the {@link #maxMessageSize} limit for compressed frames.
   */
  @VisibleForTesting
  static final class SizeEnforcingInputStream extends FilterInputStream {
    private final int maxMessageSize;
    private final StatsTraceContext statsTraceCtx;
    private long maxCount;
    private long count;
    private long mark = -1;

    SizeEnforcingInputStream(InputStream in, int maxMessageSize, StatsTraceContext statsTraceCtx) {
      super(in);
      this.maxMessageSize = maxMessageSize;
      this.statsTraceCtx = statsTraceCtx;
    }

    @Override
    public int read() throws IOException {
      int result = in.read();
      if (result != -1) {
        count++;
      }
      verifySize();
      reportCount();
      return result;
    }

    @Override
    public int read(byte[] b, int off, int len) throws IOException {
      int result = in.read(b, off, len);
      if (result != -1) {
        count += result;
      }
      verifySize();
      reportCount();
      return result;
    }

    @Override
    public long skip(long n) throws IOException {
      long result = in.skip(n);
      count += result;
      verifySize();
      reportCount();
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

    private void reportCount() {
      if (count > maxCount) {
        statsTraceCtx.inboundUncompressedSize(count - maxCount);
        maxCount = count;
      }
    }

    private void verifySize() {
      if (count > maxMessageSize) {
        throw Status.RESOURCE_EXHAUSTED.withDescription(String.format(
                "Compressed gRPC message exceeds maximum size %d: %d bytes read",
                maxMessageSize, count)).asRuntimeException();
      }
    }
  }

  private static class SingleMessageProducer implements StreamListener.MessageProducer {
    private InputStream message;

    private SingleMessageProducer(InputStream message) {
      this.message = message;
    }

    @Nullable
    @Override
    public InputStream next() {
      InputStream messageToReturn = message;
      message = null;
      return messageToReturn;
    }
  }
}
