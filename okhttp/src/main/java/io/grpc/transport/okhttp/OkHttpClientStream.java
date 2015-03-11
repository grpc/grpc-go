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

import com.google.common.base.Preconditions;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.Header;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.transport.ClientStreamListener;
import io.grpc.transport.Http2ClientStream;
import io.grpc.transport.WritableBuffer;
import okio.Buffer;

import java.util.List;

import javax.annotation.concurrent.GuardedBy;

/**
 * Client stream for the okhttp transport.
 */
class OkHttpClientStream extends Http2ClientStream {

  private static int WINDOW_UPDATE_THRESHOLD =
      OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE / 2;

  /**
   * Construct a new client stream.
   */
  static OkHttpClientStream newStream(ClientStreamListener listener,
                                      AsyncFrameWriter frameWriter,
                                      OkHttpClientTransport transport,
                                      OutboundFlowController outboundFlow) {
    return new OkHttpClientStream(listener, frameWriter, transport, outboundFlow);
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

  private OkHttpClientStream(ClientStreamListener listener,
                             AsyncFrameWriter frameWriter,
                             OkHttpClientTransport transport,
                             OutboundFlowController outboundFlow) {
    super(new OkHttpWritableBufferAllocator(), listener);
    this.frameWriter = frameWriter;
    this.transport = transport;
    this.outboundFlow = outboundFlow;
  }

  @Override
  public void request(final int numMessages) {
    synchronized (lock) {
      requestMessagesFromDeframer(numMessages);
    }
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
   * We synchronized on "lock" for delivering frames and updating window size, so that
   * the future listeners (executed by synchronizedExecutor) will not be executed in the same time.
   */
  public void transportDataReceived(okio.Buffer frame, boolean endOfStream) {
    synchronized (lock) {
      long length = frame.size();
      window -= length;
      super.transportDataReceived(new OkHttpReadableBuffer(frame), endOfStream);
    }
  }

  @Override
  protected void sendFrame(WritableBuffer frame, boolean endOfStream) {
    Preconditions.checkState(id() != 0, "streamId should be set");
    Buffer buffer = ((OkHttpWritableBuffer) frame).buffer();
    // Write the data to the remote endpoint.
    // Per http2 SPEC, the max data length should be larger than 64K, while our frame size is
    // only 4K.
    Preconditions.checkState(buffer.size() < frameWriter.maxDataLength());
    outboundFlow.data(endOfStream, id(), buffer);
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
    if (transport.finishStream(id(), Status.CANCELLED)) {
      frameWriter.rstStream(id(), ErrorCode.CANCEL);
      transport.stopIfNecessary();
    }
  }

  @Override
  public void remoteEndClosed() {
    super.remoteEndClosed();
    if (transport.finishStream(id(), null)) {
      transport.stopIfNecessary();
    }
  }

  void setOutboundFlowState(Object outboundFlowState) {
    this.outboundFlowState = outboundFlowState;
  }

  Object getOutboundFlowState() {
    return outboundFlowState;
  }
}
