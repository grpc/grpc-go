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

import static com.google.net.stubby.GrpcFramingUtil.FRAME_LENGTH;
import static com.google.net.stubby.GrpcFramingUtil.FRAME_TYPE_LENGTH;
import static com.google.net.stubby.GrpcFramingUtil.FRAME_TYPE_MASK;
import static com.google.net.stubby.GrpcFramingUtil.PAYLOAD_FRAME;
import static com.google.net.stubby.GrpcFramingUtil.STATUS_FRAME;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.Status;

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
  private final DeframerListener listener;
  private State state = State.HEADER;
  private int requiredLength = HEADER_LENGTH;
  private int frameType;
  private boolean statusNotified;
  private boolean endOfStream;
  private SettableFuture<?> deliveryOutstanding;
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
   * @param listener a listener to deframing events
   */
  public GrpcDeframer(Decompressor decompressor, Sink sink, Executor executor,
      DeframerListener listener) {
    this.decompressor = Preconditions.checkNotNull(decompressor, "decompressor");
    this.sink = Preconditions.checkNotNull(sink, "sink");
    this.executor = Preconditions.checkNotNull(executor, "executor");
    this.listener = Preconditions.checkNotNull(listener, "listener");
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

    // Add the data to the decompression buffer.
    decompressor.decompress(data);

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
    decompressor.close();
    if (nextFrame != null) {
      nextFrame.close();
    }
  }

  /**
   * Reads and delivers as many messages to the sink as possible. May only be called when a delivery
   * is known not to be outstanding.
   */
  private ListenableFuture<?> deliver() {
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
      // If reached the end of stream without reading a status frame, fabricate one
      // and deliver to the target.
      if (!statusNotified) {
        notifyStatus(Status.OK);
      }
    }
    // All available messages processed.
    if (deliveryOutstanding != null) {
      SettableFuture<?> previousOutstanding = deliveryOutstanding;
      deliveryOutstanding = null;
      previousOutstanding.set(null);
    }
    return null;
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
   * Attempts to read the required bytes into nextFrame.
   *
   * @returns {@code true} if all of the required bytes have been read.
   */
  private boolean readRequiredBytes() {
    int totalBytesRead = 0;
    try {
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
        totalBytesRead += buffer.readableBytes();
        // Add it to the composite buffer for the next frame.
        nextFrame.addBuffer(buffer);
      }

      // Return whether or not all of the required bytes are now in the frame.
      return nextFrame.readableBytes() == requiredLength;
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
      notifyStatus(Status.fromCodeValue(nextFrame.readUnsignedShort()));
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
