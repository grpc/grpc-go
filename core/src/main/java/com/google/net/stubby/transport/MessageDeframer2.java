package com.google.net.stubby.transport;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.Status;

import java.io.ByteArrayInputStream;
import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.Executor;
import java.util.zip.GZIPInputStream;

import javax.annotation.concurrent.NotThreadSafe;

/**
 * Deframer for GRPC frames.
 *
 * <p>This class is not thread-safe. All calls to this class must be made in the context of the
 * executor provided during creation. That executor must not allow concurrent execution of tasks.
 */
@NotThreadSafe
public class MessageDeframer2 implements Closeable {
  private static final int HEADER_LENGTH = 5;
  private static final int COMPRESSED_FLAG_MASK = 1;
  private static final int RESERVED_MASK = 0xFE;

  public enum Compression {
    NONE, GZIP;
  }

  public interface Sink {
    public ListenableFuture<Void> messageRead(InputStream is, int length);
    public void endOfStream();
  }

  private enum State {
    HEADER, BODY
  }

  private final Sink sink;
  private final Executor executor;
  private final Compression compression;
  private State state = State.HEADER;
  private int requiredLength = HEADER_LENGTH;
  private boolean compressedFlag;
  private boolean endOfStream;
  private SettableFuture<?> deliveryOutstanding;
  private CompositeBuffer nextFrame;
  private CompositeBuffer unprocessed = new CompositeBuffer();

  /**
   * Create a deframer. All calls to this class must be made in the context of the provided
   * executor, which also must not allow concurrent processing of Runnables. Compression will not
   * be supported.
   *
   * @param sink callback for fully read GRPC messages
   * @param executor used for internal event processing
   */
  public MessageDeframer2(Sink sink, Executor executor) {
    this(sink, executor, Compression.NONE);
  }

  /**
   * Create a deframer. All calls to this class must be made in the context of the provided
   * executor, which also must not allow concurrent processing of Runnables.
   *
   * @param sink callback for fully read GRPC messages
   * @param executor used for internal event processing
   * @param compression the compression used if a compressed frame is encountered, with NONE meaning
   *     unsupported
   */
  public MessageDeframer2(Sink sink, Executor executor, Compression compression) {
    this.sink = Preconditions.checkNotNull(sink, "sink");
    this.executor = Preconditions.checkNotNull(executor, "executor");
    this.compression = Preconditions.checkNotNull(compression, "compression");
  }

  /**
   * Adds the given data to this deframer and attempts delivery to the sink.
   *
   * <p>If returned future is not {@code null}, then it completes when no more deliveries are
   * occuring. Delivering completes if all available deframing input is consumed or if delivery
   * resulted in an exception, in which case this method may throw the exception or the returned
   * future will fail with the throwable. The future is guaranteed to complete within the executor
   * provided during construction.
   */
  public ListenableFuture<?> deframe(Buffer data, boolean endOfStream) {
    Preconditions.checkNotNull(data, "data");
    Preconditions.checkState(this.endOfStream == false, "Past end of stream");
    unprocessed.addBuffer(data);

    // Indicate that all of the data for this stream has been received.
    this.endOfStream = endOfStream;

    if (deliveryOutstanding != null) {
      // Only allow one outstanding delivery at a time.
      return null;
    }
    return deliver();
  }

  @Override
  public void close() {
    unprocessed.close();
    if (nextFrame != null) {
      nextFrame.close();
    }
  }

  /**
   * Consider {@code future} to be a message currently being processed. Messages will not be
   * delivered until the future completes. The returned future behaves as if it was returned by
   * {@link #deframe(Buffer, boolean)}.
   *
   * @throws IllegalStateException if a message is already being processed
   */
  public ListenableFuture<?> delayProcessing(ListenableFuture<?> future) {
    Preconditions.checkState(deliveryOutstanding == null, "Only one delay allowed concurrently");
    if (future == null) {
      return null;
    }
    return delayProcessingInternal(future);
  }

  /**
   * May only be called when a delivery is known not to be outstanding. If deliveryOutstanding is
   * non-null, then it will be re-used and this method will return {@code null}.
   */
  private ListenableFuture<?> delayProcessingInternal(ListenableFuture<?> future) {
    Preconditions.checkNotNull(future, "future");
    // Return a separate future so that our callback is guaranteed to complete before any
    // listeners on the returned future.
    ListenableFuture<?> returnFuture = null;
    if (deliveryOutstanding == null) {
      returnFuture = deliveryOutstanding = SettableFuture.create();
    }
    Futures.addCallback(future, new FutureCallback<Object>() {
      @Override
      public void onFailure(Throwable t) {
        SettableFuture<?> previousOutstanding = deliveryOutstanding;
        deliveryOutstanding = null;
        previousOutstanding.setException(t);
      }

      @Override
      public void onSuccess(Object result) {
        try {
          deliver();
        } catch (Throwable t) {
          if (deliveryOutstanding == null) {
            throw Throwables.propagate(t);
          } else {
            onFailure(t);
          }
        }
      }
    }, executor);
    return returnFuture;
  }

  /**
   * Reads and delivers as many messages to the sink as possible. May only be called when a delivery
   * is known not to be outstanding.
   */
  private ListenableFuture<?> deliver() {
    // Process the uncompressed bytes.
    while (readRequiredBytes()) {
      switch (state) {
        case HEADER:
          processHeader();
          break;
        case BODY:
          // Read the body and deliver the message to the sink.
          ListenableFuture<?> processingFuture = processBody();
          if (processingFuture != null) {
            // A future was returned for the completion of processing the delivered
            // message. Once it's done, try to deliver the next message.
            return delayProcessingInternal(processingFuture);
          }

          break;
        default:
          throw new AssertionError("Invalid state: " + state);
      }
    }

    if (endOfStream) {
      if (nextFrame.readableBytes() != 0) {
        throw Status.INTERNAL
            .withDescription("Encountered end-of-stream mid-frame")
            .asRuntimeException();
      }
      sink.endOfStream();
    }
    // All available messagesed processed.
    if (deliveryOutstanding != null) {
      SettableFuture<?> previousOutstanding = deliveryOutstanding;
      deliveryOutstanding = null;
      previousOutstanding.set(null);
    }
    return null;
  }

  /**
   * Attempts to read the required bytes into nextFrame.
   *
   * @returns {@code true} if all of the required bytes have been read.
   */
  private boolean readRequiredBytes() {
    if (nextFrame == null) {
      nextFrame = new CompositeBuffer();
    }

    // Read until the buffer contains all the required bytes.
    int missingBytes;
    while ((missingBytes = requiredLength - nextFrame.readableBytes()) > 0) {
      if (unprocessed.readableBytes() == 0) {
        // No more data is available.
        return false;
      }
      int toRead = Math.min(missingBytes, unprocessed.readableBytes());
      nextFrame.addBuffer(unprocessed.readBytes(toRead));
    }
    return true;
  }

  /**
   * Processes the GRPC compression header which is composed of the compression flag and the outer
   * frame length.
   */
  private void processHeader() {
    int type = nextFrame.readUnsignedByte();
    if ((type & RESERVED_MASK) != 0) {
      throw Status.INTERNAL
          .withDescription("Frame header malformed: reserved bits not zero")
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
  private ListenableFuture<?> processBody() {
    ListenableFuture<?> future;
    if (compressedFlag) {
      if (compression == Compression.NONE) {
        throw Status.INTERNAL
            .withDescription("Can't decode compressed frame as compression not configured.")
            .asRuntimeException();
      } else if (compression == Compression.GZIP) {
        // Fully drain frame.
        byte[] bytes;
        try {
          bytes = ByteStreams.toByteArray(
              new GZIPInputStream(Buffers.openStream(nextFrame, false)));
        } catch (IOException ex) {
          throw new RuntimeException(ex);
        }
        future = sink.messageRead(new ByteArrayInputStream(bytes), bytes.length);
      } else {
        throw new AssertionError("Unknown compression type");
      }
    } else {
      // Don't close the frame, since the sink is now responsible for the life-cycle.
      future = sink.messageRead(Buffers.openStream(nextFrame, true), nextFrame.readableBytes());
      nextFrame = null;
    }

    // Done with this frame, begin processing the next header.
    state = State.HEADER;
    requiredLength = HEADER_LENGTH;
    return future;
  }
}
