package com.google.net.stubby.newtransport;

import com.google.common.base.Preconditions;
import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

import java.io.ByteArrayInputStream;
import java.io.Closeable;
import java.io.IOException;
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

  private enum State {
    HEADER, BODY
  }

  /**
   * Create a deframer for use on the server-side. All calls to this class must be made in the
   * context of the provided executor, which also must not allow concurrent processing of Runnables.
   *
   * @param listener callback for fully read GRPC messages
   * @param executor used for internal event processing
   */
  public static MessageDeframer2 createOnServer(StreamListener listener, Executor executor) {
    return createOnServer(listener, executor, Compression.NONE);
  }

  /**
   * Create a deframer for use on the server-side. All calls to this class must be made in the
   * context of the provided executor, which also must not allow concurrent processing of Runnables.
   *
   * @param listener callback for fully read GRPC messages
   * @param executor used for internal event processing
   * @param compression the compression used if a compressed frame is encountered, with NONE meaning
   *     unsupported
   */
  public static MessageDeframer2 createOnServer(StreamListener listener, Executor executor,
      Compression compression) {
    return new MessageDeframer2(listener, executor, false, compression);
  }

  /**
   * Create a deframer for use on the client-side. All calls to this class must be made in the
   * context of the provided executor, which also must not allow concurrent processing of Runnables.
   *
   * @param listener callback for fully read GRPC messages
   * @param executor used for internal event processing
   */
  public static MessageDeframer2 createOnClient(StreamListener listener, Executor executor) {
    return createOnClient(listener, executor, Compression.NONE);
  }

  /**
   * Create a deframer for use on the client-side. All calls to this class must be made in the
   * context of the provided executor, which also must not allow concurrent processing of Runnables.
   *
   * @param listener callback for fully read GRPC messages
   * @param executor used for internal event processing
   * @param compression the compression used if a compressed frame is encountered, with NONE meaning
   *     unsupported
   */
  public static MessageDeframer2 createOnClient(StreamListener listener, Executor executor,
      Compression compression) {
    return new MessageDeframer2(listener, executor, true, compression);
  }

  private final StreamListener listener;
  private final Executor executor;
  private final boolean client;
  private final Compression compression;
  private final Runnable deliveryTask = new Runnable() {
        @Override
        public void run() {
          deliveryOutstanding = false;
          deliver();
        }
      };
  private State state = State.HEADER;
  private int requiredLength = HEADER_LENGTH;
  private boolean compressedFlag;
  private boolean endOfStream;
  private boolean deliveryOutstanding;
  private CompositeBuffer nextFrame;
  private CompositeBuffer unprocessed = new CompositeBuffer();

  private MessageDeframer2(StreamListener listener, Executor executor, boolean client,
      Compression compression) {
    this.listener = Preconditions.checkNotNull(listener, "listener");
    this.executor = Preconditions.checkNotNull(executor, "executor");
    this.client = client;
    this.compression = Preconditions.checkNotNull(compression, "compression");
  }

  /**
   * Adds the given data to this deframer and attempts delivery to the listener.
   */
  public void deframe(Buffer data, boolean endOfStream) {
    Preconditions.checkNotNull(data, "data");
    Preconditions.checkState(this.endOfStream == false, "Past end of stream");
    unprocessed.addBuffer(data);

    // Indicate that all of the data for this stream has been received.
    this.endOfStream = endOfStream;

    // Deliver the next message if not already delivering.
    deliver();
  }

  @Override
  public void close() {
    unprocessed.close();
    if (nextFrame != null) {
      nextFrame.close();
    }
  }

  /**
   * If there is no outstanding delivery, attempts to read and deliver as many messages to the
   * listener as possible. Only one outstanding delivery is allowed at a time.
   */
  private void deliver() {
    if (deliveryOutstanding) {
      // Only allow one outstanding delivery at a time.
      return;
    }

    // Process the uncompressed bytes.
    while (readRequiredBytes()) {
      switch (state) {
        case HEADER:
          processHeader();
          break;
        case BODY:
          // Read the body and deliver the message to the listener.
          deliveryOutstanding = true;
          ListenableFuture<Void> processingFuture = processBody();
          if (processingFuture != null) {
            // A listener was returned for the completion of processing the delivered
            // message. Once it's done, try to deliver the next message.
            processingFuture.addListener(deliveryTask, executor);
            return;
          }

          // No future was returned, so assume processing is complete for the delivery.
          deliveryOutstanding = false;
          break;
        default:
          throw new AssertionError("Invalid state: " + state);
      }
    }

    if (endOfStream) {
      if (nextFrame.readableBytes() != 0) {
        // TODO(user): Investigate how this should be propagated, so that stream is aborted and
        // application is properly notified of abortion.
        throw new RuntimeException("Encountered end-of-stream mid-frame");
      }
      if (!client) {
        // If on the server-side, we need to notify application of half-close.
        listener.closed(Status.OK, new Metadata.Trailers());
      }
    }
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
      throw new RuntimeException("Frame header malformed: reserved bits not zero");
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
  private ListenableFuture<Void> processBody() {
    ListenableFuture<Void> future;
    if (compressedFlag) {
      if (compression == Compression.NONE) {
        throw new IllegalStateException("Can't decode compressed frame with NONE compression");
      } else if (compression == Compression.GZIP) {
        // Fully drain frame.
        byte[] bytes;
        try {
          bytes = ByteStreams.toByteArray(
              new GZIPInputStream(Buffers.openStream(nextFrame, false)));
        } catch (IOException ex) {
          throw new RuntimeException(ex);
        }
        future = listener.messageRead(new ByteArrayInputStream(bytes), bytes.length);
      } else {
        throw new AssertionError("Unknown compression type");
      }
    } else {
      // Don't close the frame, since the listener is now responsible for the life-cycle.
      future = listener.messageRead(Buffers.openStream(nextFrame, true), nextFrame.readableBytes());
      nextFrame = null;
    }

    // Done with this frame, begin processing the next header.
    state = State.HEADER;
    requiredLength = HEADER_LENGTH;
    return future;
  }
}
