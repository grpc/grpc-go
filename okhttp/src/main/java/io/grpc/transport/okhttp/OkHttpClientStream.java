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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.Header;

import io.grpc.Metadata;
import io.grpc.MethodType;
import io.grpc.Status;
import io.grpc.transport.ClientStreamListener;
import io.grpc.transport.Http2ClientStream;
import io.grpc.transport.WritableBuffer;

import okio.Buffer;

import java.util.List;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * Client stream for the okhttp transport.
 */
class OkHttpClientStream extends Http2ClientStream {

  private static int WINDOW_UPDATE_THRESHOLD =
      OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE / 2;

  private static final Buffer EMPTY_BUFFER = new Buffer();

  private final MethodType type;

  /**
   * Construct a new client stream.
   */
  static OkHttpClientStream newStream(ClientStreamListener listener,
                                      AsyncFrameWriter frameWriter,
                                      OkHttpClientTransport transport,
                                      OutboundFlowController outboundFlow,
                                      MethodType type) {
    return new OkHttpClientStream(listener, frameWriter, transport, outboundFlow, type);
  }

  @GuardedBy("lock")
  private int window = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE;
  @GuardedBy("lock")
  private int processedWindow = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE;
  private final AsyncFrameWriter frameWriter;
  private final OutboundFlowController outboundFlow;
  private final OkHttpClientTransport transport;
  private final Object lock = new Object();
  private Object outboundFlowState;
  private volatile Integer id;

  private OkHttpClientStream(ClientStreamListener listener,
                             AsyncFrameWriter frameWriter,
                             OkHttpClientTransport transport,
                             OutboundFlowController outboundFlow,
                             MethodType type) {
    super(new OkHttpWritableBufferAllocator(), listener);
    this.frameWriter = frameWriter;
    this.transport = transport;
    this.outboundFlow = outboundFlow;
    this.type = type;
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

  /**
   * Set the internal ID for this stream.
   */
  public void id(Integer id) {
    checkNotNull(id, "id");
    checkState(this.id == null, "Can only set id once");
    this.id = id;
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

  public void transportHeadersReceived(List<Header> headers, boolean endOfStream) {
    synchronized (lock) {
      if (endOfStream) {
        transportTrailersReceived(Utils.convertTrailers(headers));
      } else {
        transportHeadersReceived(Utils.convertHeaders(headers));
      }
    }
  }

  /**
   * We synchronized on "lock" for delivering frames and updating window size, because
   * the {@link #request(int)} call can be called in other thread for delivering frames.
   */
  public void transportDataReceived(okio.Buffer frame, boolean endOfStream) {
    synchronized (lock) {
      long length = frame.size();
      window -= length;
      if (window < 0) {
        frameWriter.rstStream(id(), ErrorCode.FLOW_CONTROL_ERROR);
        Status status = Status.INTERNAL.withDescription(
            "Received data size exceeded our receiving window size");
        transport.finishStream(id(), status, null);
        return;
      }
      super.transportDataReceived(new OkHttpReadableBuffer(frame), endOfStream);
    }
  }

  @Override
  protected void sendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
    checkState(id() != 0, "streamId should be set");
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
    // If buffer > frameWriter.maxDataLength() the flow-controller will ensure that it is
    // properly chunked.
    outboundFlow.data(endOfStream, id(), buffer, flush);
  }

  @Override
  protected void returnProcessedBytes(int processedBytes) {
    synchronized (lock) {
      processedWindow -= processedBytes;
      if (processedWindow <= WINDOW_UPDATE_THRESHOLD) {
        int delta = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE - processedWindow;
        window += delta;
        processedWindow += delta;
        frameWriter.windowUpdate(id(), delta);
      }
    }
  }

  @Override
  public void transportReportStatus(Status newStatus, boolean stopDelivery,
      Metadata.Trailers trailers) {
    synchronized (lock) {
      super.transportReportStatus(newStatus, stopDelivery, trailers);
    }
  }

  @Override
  protected void sendCancel() {
    transport.finishStream(id(), Status.CANCELLED, ErrorCode.CANCEL);
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
}
