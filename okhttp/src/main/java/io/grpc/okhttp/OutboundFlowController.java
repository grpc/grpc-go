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
import static java.lang.Math.ceil;
import static java.lang.Math.max;
import static java.lang.Math.min;

import com.google.common.base.Preconditions;
import io.grpc.okhttp.internal.framed.FrameWriter;
import java.io.IOException;
import javax.annotation.Nullable;
import okio.Buffer;

/**
 * Simple outbound flow controller that evenly splits the connection window across all existing
 * streams.
 */
class OutboundFlowController {
  private final OkHttpClientTransport transport;
  private final FrameWriter frameWriter;
  private int initialWindowSize;
  private final OutboundFlowState connectionState;

  OutboundFlowController(
      OkHttpClientTransport transport, FrameWriter frameWriter, int initialWindowSize) {
    this.transport = Preconditions.checkNotNull(transport, "transport");
    this.frameWriter = Preconditions.checkNotNull(frameWriter, "frameWriter");
    this.initialWindowSize = initialWindowSize;
    connectionState = new OutboundFlowState(CONNECTION_STREAM_ID, initialWindowSize);
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
        state = new OutboundFlowState(stream, initialWindowSize);
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
    boolean framesAlreadyQueued = state.hasPendingData();
    int size = (int) source.size();

    if (!framesAlreadyQueued && window >= size) {
      // Window size is large enough to send entire data frame
      state.write(source, size, outFinished);
    } else {
      // send partial data
      if (!framesAlreadyQueued && window > 0) {
        state.write(source, window, false);
      }
      // Queue remaining data in the buffer
      state.enqueue(source, (int) source.size(), outFinished);
    }

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
      state = new OutboundFlowState(stream, initialWindowSize);
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
    final Buffer pendingWriteBuffer;
    final int streamId;
    int window;
    int allocatedBytes;
    OkHttpClientStream stream;
    boolean pendingBufferHasEndOfStream = false;

    OutboundFlowState(int streamId, int initialWindowSize) {
      this.streamId = streamId;
      window = initialWindowSize;
      pendingWriteBuffer = new Buffer();
    }

    OutboundFlowState(OkHttpClientStream stream, int initialWindowSize) {
      this(stream.id(), initialWindowSize);
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
      return max(0, min(window, (int) pendingWriteBuffer.size()));
    }

    /**
     * Indicates whether or not there are frames in the pending queue.
     */
    boolean hasPendingData() {
      return pendingWriteBuffer.size() > 0;
    }

    /**
     * Writes up to the number of bytes from the pending queue.
     */
    int writeBytes(int bytes, WriteStatus writeStatus) {
      int bytesAttempted = 0;
      int maxBytes = min(bytes, writableWindow());
      while (hasPendingData() && maxBytes > 0) {
        if (maxBytes >= pendingWriteBuffer.size()) {
          // Window size is large enough to send entire data frame
          bytesAttempted += (int) pendingWriteBuffer.size();
          write(pendingWriteBuffer, (int) pendingWriteBuffer.size(), pendingBufferHasEndOfStream);
        } else {
          bytesAttempted += maxBytes;
          write(pendingWriteBuffer, maxBytes, false);
        }
        writeStatus.incrementNumWrites();
        // Update the threshold.
        maxBytes = min(bytes - bytesAttempted, writableWindow());
      }
      return bytesAttempted;
    }

    /**
     * Writes the frame and decrements the stream and connection window sizes. If the frame is in
     * the pending queue, the written bytes are removed from this branch of the priority tree. If
     * the window size is smaller than the frame, it sends partial frame.
     */
    void write(Buffer buffer, int bytesToSend, boolean endOfStream) {
      int bytesToWrite = bytesToSend;
      // Using a do/while loop because if the buffer is empty we still need to call
      // the writer once to send the empty frame.
      do {
        int frameBytes = min(bytesToWrite, frameWriter.maxDataLength());
        connectionState.incrementStreamWindow(-frameBytes);
        incrementStreamWindow(-frameBytes);
        try {
          // endOfStream is set for the last chunk of data marked as endOfStream
          boolean isEndOfStream = buffer.size() == frameBytes && endOfStream;
          frameWriter.data(isEndOfStream, streamId, buffer, frameBytes);
        } catch (IOException e) {
          throw new RuntimeException(e);
        }
        stream.transportState().onSentBytes(frameBytes);
        bytesToWrite -= frameBytes;
      } while (bytesToWrite > 0);
    }

    void enqueue(Buffer buffer, int size, boolean endOfStream) {
      this.pendingWriteBuffer.write(buffer, size);
      this.pendingBufferHasEndOfStream |= endOfStream;
    }
  }
}