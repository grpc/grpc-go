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

package io.grpc.okhttp;

import static io.grpc.okhttp.Utils.CONNECTION_STREAM_ID;
import static io.grpc.okhttp.Utils.DEFAULT_WINDOW_SIZE;
import static java.lang.Math.ceil;
import static java.lang.Math.max;
import static java.lang.Math.min;

import com.google.common.base.Preconditions;
import io.grpc.okhttp.internal.framed.FrameWriter;
import java.io.IOException;
import java.util.ArrayDeque;
import java.util.Queue;
import javax.annotation.Nullable;
import okio.Buffer;

/**
 * Simple outbound flow controller that evenly splits the connection window across all existing
 * streams.
 */
class OutboundFlowController {
  private final OkHttpClientTransport transport;
  private final FrameWriter frameWriter;
  private int initialWindowSize = DEFAULT_WINDOW_SIZE;
  private final OutboundFlowState connectionState = new OutboundFlowState(CONNECTION_STREAM_ID);

  OutboundFlowController(OkHttpClientTransport transport, FrameWriter frameWriter) {
    this.transport = Preconditions.checkNotNull(transport, "transport");
    this.frameWriter = Preconditions.checkNotNull(frameWriter, "frameWriter");
  }

  /**
   * Adjusts outbound window size requested by peer. When window size is increased, it does not send
   * any pending frames. If this method returns {@code true}, the caller should call {@link
   * #writeStreams()} after settings ack.
   *
   * <p>Must be called with holding transport lock.
   *
   * @return true, if new window size is increased, false otherwise.
   */
  boolean initialOutboundWindowSize(int newWindowSize) {
    if (newWindowSize < 0) {
      throw new IllegalArgumentException("Invalid initial window size: " + newWindowSize);
    }

    int delta = newWindowSize - initialWindowSize;
    initialWindowSize = newWindowSize;
    for (OkHttpClientStream stream : transport.getActiveStreams()) {
      OutboundFlowState state = (OutboundFlowState) stream.getOutboundFlowState();
      if (state == null) {
        // Create the OutboundFlowState with the new window size.
        state = new OutboundFlowState(stream);
        stream.setOutboundFlowState(state);
      } else {
        state.incrementStreamWindow(delta);
      }
    }

    return delta > 0;
  }

  /**
   * Update the outbound window for given stream, or for the connection if stream is null. Returns
   * the new value of the window size.
   *
   * <p>Must be called with holding transport lock.
   */
  int windowUpdate(@Nullable OkHttpClientStream stream, int delta) {
    final int updatedWindow;
    if (stream == null) {
      // Update the connection window and write any pending frames for all streams.
      updatedWindow = connectionState.incrementStreamWindow(delta);
      writeStreams();
    } else {
      // Update the stream window and write any pending frames for the stream.
      OutboundFlowState state = state(stream);
      updatedWindow = state.incrementStreamWindow(delta);

      WriteStatus writeStatus = new WriteStatus();
      state.writeBytes(state.writableWindow(), writeStatus);
      if (writeStatus.hasWritten()) {
        flush();
      }
    }
    return updatedWindow;
  }

  /**
   * Must be called with holding transport lock.
   */
  void data(boolean outFinished, int streamId, Buffer source, boolean flush) {
    Preconditions.checkNotNull(source, "source");

    OkHttpClientStream stream = transport.getStream(streamId);
    if (stream == null) {
      // This is possible for a stream that has received end-of-stream from server (but hasn't sent
      // end-of-stream), and was removed from the transport stream map.
      // In such case, we just throw away the data.
      return;
    }

    OutboundFlowState state = state(stream);
    int window = state.writableWindow();
    boolean framesAlreadyQueued = state.hasFrame();

    OutboundFlowState.Frame frame = state.newFrame(source, outFinished);
    if (!framesAlreadyQueued && window >= frame.size()) {
      // Window size is large enough to send entire data frame
      frame.write();
      if (flush) {
        flush();
      }
      return;
    }

    // Enqueue the frame to be written when the window size permits.
    frame.enqueue();

    if (framesAlreadyQueued || window <= 0) {
      // Stream already has frames pending or is stalled, don't send anything now.
      if (flush) {
        flush();
      }
      return;
    }

    // Create and send a partial frame up to the window size.
    frame.split(window).write();
    if (flush) {
      flush();
    }
  }

  void flush() {
    try {
      frameWriter.flush();
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }

  private OutboundFlowState state(OkHttpClientStream stream) {
    OutboundFlowState state = (OutboundFlowState) stream.getOutboundFlowState();
    if (state == null) {
      state = new OutboundFlowState(stream);
      stream.setOutboundFlowState(state);
    }
    return state;
  }

  /**
   * Writes as much data for all the streams as possible given the current flow control windows.
   *
   * <p>Must be called with holding transport lock.
   */
  void writeStreams() {
    OkHttpClientStream[] streams = transport.getActiveStreams();
    int connectionWindow = connectionState.window();
    for (int numStreams = streams.length; numStreams > 0 && connectionWindow > 0;) {
      int nextNumStreams = 0;
      int windowSlice = (int) ceil(connectionWindow / (float) numStreams);
      for (int index = 0; index < numStreams && connectionWindow > 0; ++index) {
        OkHttpClientStream stream = streams[index];
        OutboundFlowState state = state(stream);

        int bytesForStream = min(connectionWindow, min(state.unallocatedBytes(), windowSlice));
        if (bytesForStream > 0) {
          state.allocateBytes(bytesForStream);
          connectionWindow -= bytesForStream;
        }

        if (state.unallocatedBytes() > 0) {
          // There is more data to process for this stream. Add it to the next
          // pass.
          streams[nextNumStreams++] = stream;
        }
      }
      numStreams = nextNumStreams;
    }

    // Now take one last pass through all of the streams and write any allocated bytes.
    WriteStatus writeStatus = new WriteStatus();
    for (OkHttpClientStream stream : transport.getActiveStreams()) {
      OutboundFlowState state = state(stream);
      state.writeBytes(state.allocatedBytes(), writeStatus);
      state.clearAllocatedBytes();
    }

    if (writeStatus.hasWritten()) {
      flush();
    }
  }

  /**
   * Simple status that keeps track of the number of writes performed.
   */
  private static final class WriteStatus {
    int numWrites;

    void incrementNumWrites() {
      numWrites++;
    }

    boolean hasWritten() {
      return numWrites > 0;
    }
  }

  /**
   * The outbound flow control state for a single stream.
   */
  private final class OutboundFlowState {
    final Queue<Frame> pendingWriteQueue;
    final int streamId;
    int queuedBytes;
    int window = initialWindowSize;
    int allocatedBytes;
    OkHttpClientStream stream;

    OutboundFlowState(int streamId) {
      this.streamId = streamId;
      pendingWriteQueue = new ArrayDeque<Frame>(2);
    }

    OutboundFlowState(OkHttpClientStream stream) {
      this(stream.id());
      this.stream = stream;
    }

    int window() {
      return window;
    }

    void allocateBytes(int bytes) {
      allocatedBytes += bytes;
    }

    int allocatedBytes() {
      return allocatedBytes;
    }

    int unallocatedBytes() {
      return streamableBytes() - allocatedBytes;
    }

    void clearAllocatedBytes() {
      allocatedBytes = 0;
    }

    /**
     * Increments the flow control window for this stream by the given delta and returns the new
     * value.
     */
    int incrementStreamWindow(int delta) {
      if (delta > 0 && Integer.MAX_VALUE - delta < window) {
        throw new IllegalArgumentException("Window size overflow for stream: " + streamId);
      }
      window += delta;

      return window;
    }

    /**
     * Returns the maximum writable window (minimum of the stream and connection windows).
     */
    int writableWindow() {
      return min(window, connectionState.window());
    }

    int streamableBytes() {
      return max(0, min(window, queuedBytes));
    }

    /**
     * Creates a new frame with the given values but does not add it to the pending queue.
     */
    Frame newFrame(Buffer data, boolean endStream) {
      return new Frame(data, endStream);
    }

    /**
     * Indicates whether or not there are frames in the pending queue.
     */
    boolean hasFrame() {
      return !pendingWriteQueue.isEmpty();
    }

    /**
     * Returns the head of the pending queue, or {@code null} if empty.
     */
    private Frame peek() {
      return pendingWriteQueue.peek();
    }

    /**
     * Writes up to the number of bytes from the pending queue.
     */
    int writeBytes(int bytes, WriteStatus writeStatus) {
      int bytesAttempted = 0;
      int maxBytes = min(bytes, writableWindow());
      while (hasFrame()) {
        Frame pendingWrite = peek();
        if (maxBytes >= pendingWrite.size()) {
          // Window size is large enough to send entire data frame
          writeStatus.incrementNumWrites();
          bytesAttempted += pendingWrite.size();
          pendingWrite.write();
        } else if (maxBytes <= 0) {
          // No data from the current frame can be written - we're done.
          // We purposely check this after first testing the size of the
          // pending frame to properly handle zero-length frame.
          break;
        } else {
          // We can send a partial frame
          Frame partialFrame = pendingWrite.split(maxBytes);
          writeStatus.incrementNumWrites();
          bytesAttempted += partialFrame.size();
          partialFrame.write();
        }

        // Update the threshold.
        maxBytes = min(bytes - bytesAttempted, writableWindow());
      }
      return bytesAttempted;
    }

    /**
     * A wrapper class around the content of a data frame.
     */
    private final class Frame {
      final Buffer data;
      final boolean endStream;
      boolean enqueued;

      Frame(Buffer data, boolean endStream) {
        this.data = data;
        this.endStream = endStream;
      }

      /**
       * Gets the total size (in bytes) of this frame including the data and padding.
       */
      int size() {
        return (int) data.size();
      }

      void enqueue() {
        if (!enqueued) {
          enqueued = true;
          pendingWriteQueue.offer(this);

          // Increment the number of pending bytes for this stream.
          queuedBytes += size();
        }
      }

      /**
       * Writes the frame and decrements the stream and connection window sizes. If the frame is in
       * the pending queue, the written bytes are removed from this branch of the priority tree.
       */
      void write() {
        // Using a do/while loop because if the buffer is empty we still need to call
        // the writer once to send the empty frame.
        do {
          int bytesToWrite = size();
          int frameBytes = min(bytesToWrite, frameWriter.maxDataLength());
          if (frameBytes == bytesToWrite) {
            // All the bytes fit into a single HTTP/2 frame, just send it all.
            connectionState.incrementStreamWindow(-bytesToWrite);
            incrementStreamWindow(-bytesToWrite);
            try {
              frameWriter.data(endStream, streamId, data, bytesToWrite);
            } catch (IOException e) {
              throw new RuntimeException(e);
            }
            stream.transportState().onSentBytes(bytesToWrite);

            if (enqueued) {
              // It's enqueued - remove it from the head of the pending write queue.
              queuedBytes -= bytesToWrite;
              pendingWriteQueue.remove(this);
            }
            return;
          }

          // Split a chunk that will fit into a single HTTP/2 frame and write it.
          Frame frame = split(frameBytes);
          frame.write();
        } while (size() > 0);
      }

      /**
       * Creates a new frame that is a view of this frame's data. The {@code maxBytes} are first
       * split from the data buffer. If not all the requested bytes are available, the remaining
       * bytes are then split from the padding (if available).
       *
       * @param maxBytes the maximum number of bytes that is allowed in the created frame.
       * @return the partial frame.
       */
      Frame split(int maxBytes) {
        // The requested maxBytes should always be less than the size of this frame.
        assert maxBytes < size() : "Attempting to split a frame for the full size.";

        // Get the portion of the data buffer to be split. Limit to the readable bytes.
        int dataSplit = min(maxBytes, (int) data.size());

        Buffer splitSlice = new Buffer();
        splitSlice.write(data, dataSplit);

        Frame frame = new Frame(splitSlice, false);

        if (enqueued) {
          queuedBytes -= dataSplit;
        }
        return frame;
      }
    }
  }
}
