package com.google.net.stubby.newtransport;

import static com.google.net.stubby.GrpcFramingUtil.FRAME_LENGTH;
import static com.google.net.stubby.GrpcFramingUtil.FRAME_TYPE_LENGTH;
import static com.google.net.stubby.GrpcFramingUtil.FRAME_TYPE_MASK;
import static com.google.net.stubby.GrpcFramingUtil.PAYLOAD_FRAME;
import static com.google.net.stubby.GrpcFramingUtil.STATUS_FRAME;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Transport;

import java.io.Closeable;
import java.util.concurrent.Executor;

/**
 * Deframer for GRPC frames. Delegates deframing/decompression of the GRPC compression frame to a
 * {@link Decompressor}.
 */
public class GrpcDeframer implements Closeable {
  public interface Sink extends MessageDeframer2.Sink {
    void statusRead(Status status);
  }

  private enum State {
    HEADER, BODY
  }

  private static final int HEADER_LENGTH = FRAME_TYPE_LENGTH + FRAME_LENGTH;
  private final Decompressor decompressor;
  private final Executor executor;
  private final Runnable deliveryTask;
  private State state = State.HEADER;
  private int requiredLength = HEADER_LENGTH;
  private int frameType;
  private boolean statusNotified;
  private boolean endOfStream;
  private boolean deliveryOutstanding;
  private Sink sink;
  private CompositeBuffer nextFrame;

  /**
   * Constructs the deframer.
   *
   * @param decompressor the object used for de-framing GRPC compression frames.
   * @param sink the sink for fully read GRPC messages.
   * @param executor the executor to be used for delivery. All calls to
   *        {@link #deframe(Buffer, boolean)} must be made in the context of this executor. This
   *        executor must not allow concurrent access to this class, so it must be either a single
   *        thread or have sequential processing of events.
   */
  public GrpcDeframer(Decompressor decompressor, Sink sink, Executor executor) {
    this.decompressor = Preconditions.checkNotNull(decompressor, "decompressor");
    this.sink = Preconditions.checkNotNull(sink, "sink");
    this.executor = Preconditions.checkNotNull(executor, "executor");
    deliveryTask = new Runnable() {
      @Override
      public void run() {
        deliveryOutstanding = false;
        deliver();
      }
    };
  }

  /**
   * Adds the given data to this deframer and attempts delivery to the sink.
   */
  public void deframe(Buffer data, boolean endOfStream) {
    Preconditions.checkNotNull(data, "data");

    // Add the data to the decompression buffer.
    decompressor.decompress(data);

    // Indicate that all of the data for this stream has been received.
    this.endOfStream = endOfStream;

    // Deliver the next message if not already delivering.
    deliver();
  }

  @Override
  public void close() {
    decompressor.close();
    if (nextFrame != null) {
      nextFrame.close();
    }
  }

  /**
   * If there is no outstanding delivery, attempts to read and deliver as many messages to the
   * sink as possible. Only one outstanding delivery is allowed at a time.
   */
  private void deliver() {
    if (deliveryOutstanding) {
      // Only allow one outstanding delivery at a time.
      return;
    }

    // Process the uncompressed bytes.
    while (readRequiredBytes()) {
      if (statusNotified) {
        throw new IllegalStateException("Inbound data after receiving status frame");
      }

      switch (state) {
        case HEADER:
          processHeader();
          break;
        case BODY:
          // Read the body and deliver the message to the sink.
          deliveryOutstanding = true;
          ListenableFuture<Void> processingFuture = processBody();
          if (processingFuture != null) {
            // A sink was returned for the completion of processing the delivered
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

    // If reached the end of stream without reading a status frame, fabricate one
    // and deliver to the target.
    if (!statusNotified && endOfStream) {
      notifyStatus(Status.OK);
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
      Buffer buffer = decompressor.readBytes(missingBytes);
      if (buffer == null) {
        // No more data is available.
        break;
      }
      // Add it to the composite buffer for the next frame.
      nextFrame.addBuffer(buffer);
    }

    // Return whether or not all of the required bytes are now in the frame.
    return nextFrame.readableBytes() == requiredLength;
  }

  /**
   * Processes the GRPC compression header which is composed of the compression flag and the outer
   * frame length.
   */
  private void processHeader() {
    // Peek, but do not read the header.
    frameType = nextFrame.readUnsignedByte() & FRAME_TYPE_MASK;

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
    ListenableFuture<Void> future = null;
    switch (frameType) {
      case PAYLOAD_FRAME:
        future = processMessage();
        break;
      case STATUS_FRAME:
        processStatus();
        break;
      default:
        throw new AssertionError("Invalid frameType: " + frameType);
    }

    // Done with this frame, begin processing the next header.
    state = State.HEADER;
    requiredLength = HEADER_LENGTH;
    return future;
  }

  /**
   * Processes the payload of a message frame.
   */
  private ListenableFuture<Void> processMessage() {
    try {
      return sink.messageRead(Buffers.openStream(nextFrame, true), nextFrame.readableBytes());
    } finally {
      // Don't close the frame, since the sink is now responsible for the life-cycle.
      nextFrame = null;
    }
  }

  /**
   * Processes the payload of a status frame.
   */
  private void processStatus() {
    try {
      int statusCode = nextFrame.readUnsignedShort();
      Transport.Code code = Transport.Code.valueOf(statusCode);
      notifyStatus(code != null ? new Status(code)
          : new Status(Transport.Code.UNKNOWN, "Unknown status code " + statusCode));
    } finally {
      nextFrame.close();
      nextFrame = null;
    }
  }

  /**
   * Delivers the status notification to the sink.
   */
  private void notifyStatus(Status status) {
    statusNotified = true;
    sink.statusRead(status);
    sink.endOfStream();
  }
}
