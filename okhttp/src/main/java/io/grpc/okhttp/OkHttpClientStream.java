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

package io.grpc.okhttp;

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.Header;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.Http2ClientStream;
import io.grpc.internal.WritableBuffer;

import okio.Buffer;

import java.util.LinkedList;
import java.util.List;
import java.util.Queue;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * Client stream for the okhttp transport.
 */
class OkHttpClientStream extends Http2ClientStream {

  private static final int WINDOW_UPDATE_THRESHOLD = Utils.DEFAULT_WINDOW_SIZE / 2;

  private static final Buffer EMPTY_BUFFER = new Buffer();

  private final MethodType type;

  @GuardedBy("lock")
  private int window = Utils.DEFAULT_WINDOW_SIZE;
  @GuardedBy("lock")
  private int processedWindow = Utils.DEFAULT_WINDOW_SIZE;
  private final AsyncFrameWriter frameWriter;
  private final OutboundFlowController outboundFlow;
  private final OkHttpClientTransport transport;
  private final Object lock;
  private Object outboundFlowState;
  private volatile Integer id;
  private List<Header> requestHeaders;
  @GuardedBy("lock")
  private Queue<PendingData> pendingData = new LinkedList<PendingData>();
  @GuardedBy("lock")
  private boolean cancelSent = false;

  OkHttpClientStream(ClientStreamListener listener,
      AsyncFrameWriter frameWriter,
      OkHttpClientTransport transport,
      OutboundFlowController outboundFlow,
      MethodType type,
      Object lock,
      List<Header> requestHeaders,
      int maxMessageSize) {
    super(new OkHttpWritableBufferAllocator(), listener, maxMessageSize);
    this.frameWriter = frameWriter;
    this.transport = transport;
    this.outboundFlow = outboundFlow;
    this.type = type;
    this.lock = lock;
    this.requestHeaders = requestHeaders;
  }

  /**
   * Returns the type of this stream.
   */
  public MethodType getType() {
    return type;
  }

  @Override
  public void request(final int numMessages) {
    synchronized (lock) {
      requestMessagesFromDeframer(numMessages);
    }
  }

  @Override
  @Nullable
  public Integer id() {
    return id;
  }

  @GuardedBy("lock")
  public void start(Integer id) {
    checkNotNull(id, "id");
    checkState(this.id == null, "the stream has been started with id %s", this.id);
    this.id = id;
    frameWriter.synStream(false, false, id, 0, requestHeaders);
    requestHeaders = null;

    if (pendingData != null) {
      while (!pendingData.isEmpty()) {
        PendingData data = pendingData.poll();
        outboundFlow.data(data.endOfStream, id, data.buffer, data.flush);
      }
      pendingData = null;
    }
  }

  /**
   * Notification that this stream was allocated for the connection. This means the stream has
   * passed through any delay caused by MAX_CONCURRENT_STREAMS.
   */
  public void allocated() {
    // Now that the stream has actually been initialized, call the listener's onReady callback if
    // appropriate.
    onStreamAllocated();
  }

  void onStreamSentBytes(int numBytes) {
    onSentBytes(numBytes);
  }

  /**
   * Must be called with holding the transport lock.
   */
  @GuardedBy("lock")
  public void transportHeadersReceived(List<Header> headers, boolean endOfStream) {
    if (endOfStream) {
      transportTrailersReceived(Utils.convertTrailers(headers));
    } else {
      transportHeadersReceived(Utils.convertHeaders(headers));
    }
  }

  /**
   * Must be called with holding the transport lock.
   */
  @GuardedBy("lock")
  public void transportDataReceived(okio.Buffer frame, boolean endOfStream) {
    long length = frame.size();
    window -= length;
    if (window < 0) {
      frameWriter.rstStream(id(), ErrorCode.FLOW_CONTROL_ERROR);
      transport.finishStream(id(), Status.INTERNAL.withDescription(
          "Received data size exceeded our receiving window size"), null);
      return;
    }
    super.transportDataReceived(new OkHttpReadableBuffer(frame), endOfStream);
  }

  @Override
  protected void sendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
    Buffer buffer;
    if (frame == null) {
      buffer = EMPTY_BUFFER;
    } else {
      buffer = ((OkHttpWritableBuffer) frame).buffer();
      int size = (int) buffer.size();
      if (size > 0) {
        onSendingBytes(size);
      }
    }

    synchronized (lock) {
      if (cancelSent) {
        return;
      }
      if (pendingData != null) {
        // Stream is pending start, queue the data.
        pendingData.add(new PendingData(buffer, endOfStream, flush));
      } else {
        checkState(id() != null, "streamId should be set");
        // If buffer > frameWriter.maxDataLength() the flow-controller will ensure that it is
        // properly chunked.
        outboundFlow.data(endOfStream, id(), buffer, flush);
      }
    }
  }

  @Override
  protected void returnProcessedBytes(int processedBytes) {
    synchronized (lock) {
      processedWindow -= processedBytes;
      if (processedWindow <= WINDOW_UPDATE_THRESHOLD) {
        int delta = Utils.DEFAULT_WINDOW_SIZE - processedWindow;
        window += delta;
        processedWindow += delta;
        frameWriter.windowUpdate(id(), delta);
      }
    }
  }

  @Override
  protected void sendCancel(Status reason) {
    synchronized (lock) {
      if (cancelSent) {
        return;
      }
      cancelSent = true;
      if (pendingData != null) {
        // stream is pending.
        transport.removePendingStream(this);
        // release holding data, so they can be GCed or returned to pool earlier.
        requestHeaders = null;
        for (PendingData data : pendingData) {
          data.buffer.clear();
        }
        pendingData = null;
        transportReportStatus(reason, true, new Metadata());
      } else {
        transport.finishStream(id(), reason, ErrorCode.CANCEL);
      }
    }
  }

  @Override
  public void remoteEndClosed() {
    super.remoteEndClosed();
    if (canSend()) {
      // If server's end-of-stream is received before client sends end-of-stream, we just send a
      // reset to server to fully close the server side stream.
      frameWriter.rstStream(id(), ErrorCode.CANCEL);
    }
    transport.finishStream(id(), null, null);
  }

  void setOutboundFlowState(Object outboundFlowState) {
    this.outboundFlowState = outboundFlowState;
  }

  Object getOutboundFlowState() {
    return outboundFlowState;
  }

  private static class PendingData {
    Buffer buffer;
    boolean endOfStream;
    boolean flush;

    PendingData(Buffer buffer, boolean endOfStream, boolean flush) {
      this.buffer = buffer;
      this.endOfStream = endOfStream;
      this.flush = flush;
    }
  }
}
