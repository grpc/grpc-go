/*
 * Copyright 2018 The gRPC Authors
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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import io.grpc.internal.SerializingExecutor;
import io.grpc.okhttp.ExceptionHandlingFrameWriter.TransportExceptionHandler;
import java.io.IOException;
import java.net.Socket;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import okio.Buffer;
import okio.Sink;
import okio.Timeout;

/**
 * A sink that asynchronously write / flushes a buffer internally. AsyncSink provides flush
 * coalescing to minimize network packing transmit.
 */
final class AsyncSink implements Sink {

  private final Object lock = new Object();
  private final Buffer buffer = new Buffer();
  private final SerializingExecutor serializingExecutor;
  private final TransportExceptionHandler transportExceptionHandler;

  @GuardedBy("lock")
  private boolean writeEnqueued = false;
  @GuardedBy("lock")
  private boolean flushEnqueued = false;
  private boolean closed = false;
  @Nullable
  private Sink sink;
  @Nullable
  private Socket socket;

  private AsyncSink(SerializingExecutor executor, TransportExceptionHandler exceptionHandler) {
    this.serializingExecutor = checkNotNull(executor, "executor");
    this.transportExceptionHandler = checkNotNull(exceptionHandler, "exceptionHandler");
  }

  static AsyncSink sink(
      SerializingExecutor executor, TransportExceptionHandler exceptionHandler) {
    return new AsyncSink(executor, exceptionHandler);
  }

  /**
   * Sets the actual sink. It is allowed to call write / flush operations on the sink iff calling
   * this method is scheduled in the executor. The socket is needed for closing.
   *
   * <p>should only be called once by thread of executor.
   */
  void becomeConnected(Sink sink, Socket socket) {
    checkState(this.sink == null, "AsyncSink's becomeConnected should only be called once.");
    this.sink = checkNotNull(sink, "sink");
    this.socket = checkNotNull(socket, "socket");
  }

  @Override
  public void write(Buffer source, long byteCount) throws IOException {
    checkNotNull(source, "source");
    if (closed) {
      throw new IOException("closed");
    }
    synchronized (lock) {
      buffer.write(source, byteCount);
      if (writeEnqueued || flushEnqueued || buffer.completeSegmentByteCount() <= 0) {
        return;
      }
      writeEnqueued = true;
    }
    serializingExecutor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        Buffer buf = new Buffer();
        synchronized (lock) {
          buf.write(buffer, buffer.completeSegmentByteCount());
          writeEnqueued = false;
        }
        sink.write(buf, buf.size());
      }
    });
  }

  @Override
  public void flush() throws IOException {
    if (closed) {
      throw new IOException("closed");
    }
    synchronized (lock) {
      if (flushEnqueued) {
        return;
      }
      flushEnqueued = true;
    }
    serializingExecutor.execute(new WriteRunnable() {
      @Override
      public void doRun() throws IOException {
        Buffer buf = new Buffer();
        synchronized (lock) {
          buf.write(buffer, buffer.size());
          flushEnqueued = false;
        }
        sink.write(buf, buf.size());
        sink.flush();
      }
    });
  }

  @Override
  public Timeout timeout() {
    return Timeout.NONE;
  }

  @Override
  public void close() {
    if (closed) {
      return;
    }
    closed = true;
    serializingExecutor.execute(new Runnable() {
      @Override
      public void run() {
        buffer.close();
        try {
          if (sink != null) {
            sink.close();
          }
        } catch (IOException e) {
          transportExceptionHandler.onException(e);
        }
        try {
          if (socket != null) {
            socket.close();
          }
        } catch (IOException e) {
          transportExceptionHandler.onException(e);
        }
      }
    });
  }

  private abstract class WriteRunnable implements Runnable {
    @Override
    public final void run() {
      try {
        if (sink == null) {
          throw new IOException("Unable to perform write due to unavailable sink.");
        }
        doRun();
      } catch (Exception e) {
        transportExceptionHandler.onException(e);
      }
    }

    public abstract void doRun() throws IOException;
  }
}