/*
 * Copyright 2014, gRPC Authors All rights reserved.
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

import com.google.common.base.Preconditions;
import io.grpc.internal.SerializingExecutor;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.FrameWriter;
import io.grpc.okhttp.internal.framed.Header;
import io.grpc.okhttp.internal.framed.Settings;
import java.io.IOException;
import java.net.Socket;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;
import okio.Buffer;

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
    this.frameWriter = Preconditions.checkNotNull(frameWriter, "frameWriter");
    this.socket = Preconditions.checkNotNull(socket, "socket");
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
      } catch (Exception e) {
        transport.onException(e);
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
