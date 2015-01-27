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

package io.grpc.transport.okhttp;

import static io.grpc.transport.okhttp.Utils.CONNECTION_STREAM_ID;
import static io.grpc.transport.okhttp.Utils.DEFAULT_WINDOW_SIZE;
import static io.grpc.transport.okhttp.Utils.MAX_FRAME_SIZE;
import static java.lang.Math.ceil;
import static java.lang.Math.max;
import static java.lang.Math.min;

import com.google.common.base.Preconditions;

import com.squareup.okhttp.internal.spdy.FrameWriter;

import okio.Buffer;

import java.io.IOException;
import java.util.ArrayDeque;
import java.util.Queue;

/**
 * Simple outbound flow controller that evenly splits the connection window across all existing
 * streams.
 */
class OutboundFlowController {
  private static final OkHttpClientStream[] EMPTY_STREAM_ARRAY = new OkHttpClientStream[0];
  private final OkHttpClientTransport transport;
  private final FrameWriter frameWriter;
  private int initialWindowSize = DEFAULT_WINDOW_SIZE;
  private final OutboundFlowState connectionState = new OutboundFlowState(CONNECTION_STREAM_ID);

  OutboundFlowController(OkHttpClientTransport transport, FrameWriter frameWriter) {
    this.transport = Preconditions.checkNotNull(transport, "transport");
    this.frameWriter = Preconditions.checkNotNull(frameWriter, "frameWriter");
  }

  synchronized void initialOutboundWindowSize(int newWindowSize) {
    if (newWindowSize < 0) {
      throw new IllegalArgumentException("Invalid initial window size: " + newWindowSize);
    }

    int delta = newWindowSize - initialWindowSize;
    initialWindowSize = newWindowSize;
    for (OkHttpClientStream stream : getActiveStreams()) {
      // Verify that the maximum value is not exceeded by this change.
      OutboundFlowState state = state(stream);
      state.incrementStreamWindow(delta);
    }

    if (delta > 0) {
      // The window size increased, send any pending frames for all streams.
      writeStreams();
    }
  }

  synchronized void windowUpdate(int streamId, int delta) {
    if (streamId == CONNECTION_STREAM_ID) {
      // Update the connection window and write any pending frames for all streams.
      connectionState.incrementStreamWindow(delta);
      writeStreams();
    } else {
      // Update the stream window and write any pending frames for the stream.
      OutboundFlowState state = stateOrFail(streamId);
      state.incrementStreamWindow(delta);

      WriteStatus writeStatus = new WriteStatus();
      state.writeBytes(state.writableWindow(), writeStatus);
      if (writeStatus.hasWritten()) {
        flush();
      }
    }
  }

  synchronized void data(boolean outFinished, int streamId, Buffer source) {
    Preconditions.checkNotNull(source, "source");
    if (streamId <= 0) {
      throw new IllegalArgumentException("streamId must be > 0");
    }

    OutboundFlowState state = stateOrFail(streamId);
    int window = state.writableWindow();
    boolean framesAlreadyQueued = state.hasFrame();

    OutboundFlowState.Frame frame = state.newFrame(source, outFinished);
    if (!framesAlreadyQueued && window >= frame.size()) {
      // Window size is large enough to send entire data frame
      frame.write();
      flush();
      return;
    }

    // Enqueue the frame to be written when the window size permits.
    frame.enqueue();

    if (framesAlreadyQueued || window <= 0) {
      // Stream already has frames pending or is stalled, don't send anything now.
      return;
    }

    // Create and send a partial frame up to the window size.
    frame.split(window).write();
    flush();
  }

  private void flush() {
    try {
      frameWriter.flush();
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }

  private OutboundFlowState state(OkHttpClientStream stream) {
    OutboundFlowState state = (OutboundFlowState) stream.getOutboundFlowState();
    if (state == null) {
      state = new OutboundFlowState(stream.id());
      stream.setOutboundFlowState(state);
    }
    return state;
  }

  private OutboundFlowState state(int streamId) {
    OkHttpClientStream stream = transport.getStreams().get(streamId);
    return stream != null ? state(stream) : null;
  }

  private OutboundFlowState stateOrFail(int streamId) {
    OutboundFlowState state = state(streamId);
    if (state == null) {
      throw new RuntimeException("Missing flow control window for stream: " + streamId);
    }
    return state;
  }

  /**
   * Gets all active streams as an array.
   */
  private OkHttpClientStream[] getActiveStreams() {
    return transport.getStreams().values().toArray(EMPTY_STREAM_ARRAY);
  }

  /**
   * Writes as much data for all the streams as possible given the current flow control windows.
   */
  private void writeStreams() {
    OkHttpClientStream[] streams = getActiveStreams();
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
    for (OkHttpClientStream stream : getActiveStreams()) {
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
  private final class WriteStatus {
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

    OutboundFlowState(int streamId) {
      this.streamId = streamId;
      pendingWriteQueue = new ArrayDeque<Frame>(2);
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
     * Returns the the head of the pending queue, or {@code null} if empty.
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
          int frameBytes = min(bytesToWrite, MAX_FRAME_SIZE);
          if (frameBytes == bytesToWrite) {
            // All the bytes fit into a single HTTP/2 frame, just send it all.
            connectionState.incrementStreamWindow(-bytesToWrite);
            incrementStreamWindow(-bytesToWrite);
            try {
              frameWriter.data(endStream, streamId, data, bytesToWrite);
            } catch (IOException e) {
              throw new RuntimeException(e);
            }
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
