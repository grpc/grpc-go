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

import com.google.common.util.concurrent.SettableFuture;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameWriter;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.Settings;

import io.grpc.SerializingExecutor;

import okio.Buffer;

import java.io.IOException;
import java.util.List;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Executor;

class AsyncFrameWriter implements FrameWriter {
  private final FrameWriter frameWriter;
  private final Executor executor;
  private final OkHttpClientTransport transport;

  public AsyncFrameWriter(FrameWriter frameWriter, OkHttpClientTransport transport,
      Executor executor) {
    this.frameWriter = frameWriter;
    this.transport = transport;
    // Although writes are thread-safe, we serialize them to prevent consuming many Threads that are
    // just waiting on each other.
    this.executor = new SerializingExecutor(executor);
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
  public void ackSettings(final Settings peerSettings) throws IOException {
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
    // Wait for the frameWriter to close.
    final SettableFuture<?> closeFuture = SettableFuture.create();
    executor.execute(new Runnable() {
      @Override
      public void run() {
        try {
          frameWriter.close();
        } catch (IOException e) {
          closeFuture.setException(e);
        } finally {
          closeFuture.set(null);
        }
      }
    });
    try {
      closeFuture.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new RuntimeException(e);
    } catch (ExecutionException e) {
      throw new RuntimeException(e);
    }
  }

  private abstract class WriteRunnable implements Runnable {
    @Override
    public final void run() {
      try {
        doRun();
      } catch (IOException ex) {
        transport.abort(ex);
        throw new RuntimeException(ex);
      }
    }

    public abstract void doRun() throws IOException;
  }

  @Override
  public int maxDataLength() {
    return frameWriter.maxDataLength();
  }
}
