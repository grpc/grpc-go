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

import com.google.common.base.Preconditions;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameWriter;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.Settings;

import io.grpc.internal.SerializingExecutor;

import okio.Buffer;

import java.io.IOException;
import java.net.Socket;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;

class AsyncFrameWriter implements FrameWriter {
  private static final Logger log = Logger.getLogger(OkHttpClientTransport.class.getName());
  private FrameWriter frameWriter;
  private Socket socket;
  // Although writes are thread-safe, we serialize them to prevent consuming many Threads that are
  // just waiting on each other.
  private final SerializingExecutor executor;
  private final OkHttpClientTransport transport;

  public AsyncFrameWriter(OkHttpClientTransport transport, SerializingExecutor executor) {
    this.transport = transport;
    this.executor = executor;
  }

  /**
   * Set the real frameWriter and the corresponding underlying socket, the socket is needed for
   * closing.
   *
   * <p>should only be called by thread of executor.
   */
  void becomeConnected(FrameWriter frameWriter, Socket socket) {
    Preconditions.checkState(this.frameWriter == null,
        "AsyncFrameWriter's setFrameWriter() should only be called once.");
    this.frameWriter = Preconditions.checkNotNull(frameWriter);
    this.socket = Preconditions.checkNotNull(socket);
  }

  @Override
  public void connectionPreface() {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.connectionPreface();
      }
    });
  }

  @Override
  public void ackSettings(final Settings peerSettings) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.ackSettings(peerSettings);
      }
    });
  }

  @Override
  public void pushPromise(final int streamId, final int promisedStreamId,
      final List<Header> requestHeaders) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.pushPromise(streamId, promisedStreamId, requestHeaders);
      }
    });
  }

  @Override
  public void flush() {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.flush();
      }
    });
  }

  @Override
  public void synStream(final boolean outFinished, final boolean inFinished, final int streamId,
      final int associatedStreamId, final List<Header> headerBlock) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.synStream(outFinished, inFinished, streamId, associatedStreamId, headerBlock);
      }
    });
  }

  @Override
  public void synReply(final boolean outFinished, final int streamId,
      final List<Header> headerBlock) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.synReply(outFinished, streamId, headerBlock);
      }
    });
  }

  @Override
  public void headers(final int streamId, final List<Header> headerBlock) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.headers(streamId, headerBlock);
      }
    });
  }

  @Override
  public void rstStream(final int streamId, final ErrorCode errorCode) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.rstStream(streamId, errorCode);
      }
    });
  }

  @Override
  public void data(final boolean outFinished, final int streamId, final Buffer source,
      final int byteCount) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.data(outFinished, streamId, source, byteCount);
      }
    });
  }

  @Override
  public void settings(final Settings okHttpSettings) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.settings(okHttpSettings);
      }
    });
  }

  @Override
  public void ping(final boolean ack, final int payload1, final int payload2) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.ping(ack, payload1, payload2);
      }
    });
  }

  @Override
  public void goAway(final int lastGoodStreamId, final ErrorCode errorCode,
      final byte[] debugData) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.goAway(lastGoodStreamId, errorCode, debugData);
        // Flush it since after goAway, we are likely to close this writer.
        frameWriter.flush();
      }
    });
  }

  @Override
  public void windowUpdate(final int streamId, final long windowSizeIncrement) {
    executor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        frameWriter.windowUpdate(streamId, windowSizeIncrement);
      }
    });
  }

  @Override
  public void close() {
    executor.execute(new Runnable() {
      @Override
      public void run() {
        if (frameWriter != null) {
          try {
            frameWriter.close();
            socket.close();
          } catch (IOException e) {
            log.log(Level.WARNING, "Failed closing connection", e);
          }
        }
      }
    });
  }

  private abstract class WriteRunnable implements Runnable {
    @Override
    public final void run() {
      try {
        if (frameWriter == null) {
          throw new IOException("Unable to perform write due to unavailable frameWriter.");
        }
        doRun();
      } catch (RuntimeException e) {
        transport.onException(e);
        throw e;
      } catch (Exception e) {
        transport.onException(e);
        throw new RuntimeException(e);
      }
    }

    public abstract void doRun() throws IOException;
  }

  @Override
  public int maxDataLength() {
    return frameWriter == null ? 0x4000 /* 16384, the minimum required by the HTTP/2 spec */
        : frameWriter.maxDataLength();
  }
}
