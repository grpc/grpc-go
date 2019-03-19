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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.fail;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.CALLS_REAL_METHODS;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.verify;

import com.google.common.base.Charsets;
import io.grpc.internal.SerializingExecutor;
import io.grpc.okhttp.ExceptionHandlingFrameWriter.TransportExceptionHandler;
import java.io.IOException;
import java.net.Socket;
import java.util.Queue;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.Executor;
import okio.Buffer;
import okio.Sink;
import okio.Timeout;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.InOrder;

/** Tests for {@link AsyncSink}. */
@RunWith(JUnit4.class)
public class AsyncSinkTest {

  private final Socket socket = mock(Socket.class);
  private final Sink mockedSink = mock(VoidSink.class, CALLS_REAL_METHODS);
  private final QueueingExecutor queueingExecutor = new QueueingExecutor();
  private final TransportExceptionHandler exceptionHandler = mock(TransportExceptionHandler.class);
  private final AsyncSink sink =
      AsyncSink.sink(new SerializingExecutor(queueingExecutor), exceptionHandler);

  @Test
  public void noCoalesceRequired() throws IOException {
    Buffer buffer = new Buffer();
    sink.becomeConnected(mockedSink, socket);
    sink.write(buffer.writeUtf8("hello"), buffer.size());
    sink.flush();
    queueingExecutor.runAll();

    InOrder inOrder = inOrder(mockedSink);
    inOrder.verify(mockedSink).write(any(Buffer.class), anyLong());
    inOrder.verify(mockedSink).flush();
  }

  @Test
  public void flushCoalescing_shouldNotMergeTwoDistinctFlushes() throws IOException {
    byte[] firstData = "a string".getBytes(Charsets.UTF_8);
    byte[] secondData = "a longer string".getBytes(Charsets.UTF_8);

    sink.becomeConnected(mockedSink, socket);
    Buffer buffer = new Buffer();
    sink.write(buffer.write(firstData), buffer.size());
    sink.flush();
    queueingExecutor.runAll();

    sink.write(buffer.write(secondData), buffer.size());
    sink.flush();
    queueingExecutor.runAll();

    InOrder inOrder = inOrder(mockedSink);
    inOrder.verify(mockedSink).write(any(Buffer.class), anyLong());
    inOrder.verify(mockedSink).flush();
    inOrder.verify(mockedSink).write(any(Buffer.class), anyLong());
    inOrder.verify(mockedSink).flush();
  }

  @Test
  public void flushCoalescing_shouldMergeTwoQueuedFlushesAndWrites() throws IOException {
    byte[] firstData = "a string".getBytes(Charsets.UTF_8);
    byte[] secondData = "a longer string".getBytes(Charsets.UTF_8);
    Buffer buffer = new Buffer().write(firstData);
    sink.becomeConnected(mockedSink, socket);
    sink.write(buffer, buffer.size());
    sink.flush();
    buffer = new Buffer().write(secondData);
    sink.write(buffer, buffer.size());
    sink.flush();

    queueingExecutor.runAll();

    InOrder inOrder = inOrder(mockedSink);
    inOrder.verify(mockedSink)
        .write(any(Buffer.class), eq((long) firstData.length + secondData.length));
    inOrder.verify(mockedSink).flush();
  }

  @Test
  public void flushCoalescing_shouldMergeWrites() throws IOException {
    byte[] firstData = "a string".getBytes(Charsets.UTF_8);
    byte[] secondData = "a longer string".getBytes(Charsets.UTF_8);
    Buffer buffer = new Buffer();
    sink.becomeConnected(mockedSink, socket);
    sink.write(buffer.write(firstData), buffer.size());
    sink.write(buffer.write(secondData), buffer.size());
    sink.flush();
    queueingExecutor.runAll();

    InOrder inOrder = inOrder(mockedSink);
    inOrder.verify(mockedSink)
        .write(any(Buffer.class), eq((long) firstData.length + secondData.length));
    inOrder.verify(mockedSink).flush();
  }

  @Test
  public void write_shouldCachePreviousException() throws IOException {
    Exception ioException = new IOException("some exception");
    doThrow(ioException)
        .when(mockedSink).write(any(Buffer.class), anyLong());
    Buffer buffer = new Buffer();
    buffer.writeUtf8("any message");
    sink.becomeConnected(mockedSink, socket);
    sink.write(buffer, buffer.size());
    sink.flush();
    queueingExecutor.runAll();
    sink.write(buffer, buffer.size());
    queueingExecutor.runAll();

    verify(exceptionHandler, timeout(1000)).onException(ioException);
  }

  @Test
  public void close_writeShouldThrowException() {
    sink.close();
    queueingExecutor.runAll();
    try {
      sink.write(new Buffer(), 0);
      fail("should throw ioException");
    } catch (IOException e) {
      assertThat(e).hasMessageThat().contains("closed");
    }
  }

  @Test
  public void write_shouldThrowIfAlreadyClosed() throws IOException {
    Exception ioException = new IOException("some exception");
    doThrow(ioException)
        .when(mockedSink).write(any(Buffer.class), anyLong());
    Buffer buffer = new Buffer();
    buffer.writeUtf8("any message");
    sink.becomeConnected(mockedSink, socket);
    sink.write(buffer, buffer.size());
    sink.close();
    queueingExecutor.runAll();
    try {
      sink.write(buffer, buffer.size());
      queueingExecutor.runAll();
      fail("should throw ioException");
    } catch (IOException e) {
      assertThat(e).hasMessageThat().contains("closed");
    }
  }

  @Test
  public void close_flushShouldThrowException() throws IOException {
    sink.becomeConnected(mockedSink, socket);
    sink.close();
    queueingExecutor.runAll();
    try {
      sink.flush();
      queueingExecutor.runAll();
      fail("should fail");
    } catch (IOException e) {
      assertThat(e).hasMessageThat().contains("closed");
    }
  }

  @Test
  public void flush_shouldThrowIfAlreadyClosed() throws IOException {
    Buffer buffer = new Buffer();
    buffer.writeUtf8("any message");
    sink.becomeConnected(mockedSink, socket);
    sink.write(buffer, buffer.size());
    sink.close();
    queueingExecutor.runAll();
    try {
      sink.flush();
      queueingExecutor.runAll();
      fail("should fail");
    } catch (IOException e) {
      assertThat(e).hasMessageThat().contains("closed");
    }
  }

  @Test
  public void write_callSinkIfBufferIsLargerThanSegmentSize() throws IOException {
    Buffer buffer = new Buffer();
    sink.becomeConnected(mockedSink, socket);
    // OkHttp is using 8192 as segment size.
    int payloadSize = 8192 * 2 - 1;
    int padding = 10;
    buffer.write(new byte[payloadSize]);

    int completeSegmentBytes = (int) buffer.completeSegmentByteCount();
    assertThat(completeSegmentBytes).isLessThan(payloadSize);

    // first trying to send of all complete segments, but not the padding
    sink.write(buffer, completeSegmentBytes + padding);
    queueingExecutor.runAll();
    verify(mockedSink).write(any(Buffer.class), eq((long) completeSegmentBytes));
    verify(mockedSink, never()).flush();
    assertThat(buffer.size()).isEqualTo((long) (payloadSize - completeSegmentBytes - padding));

    // writing smaller than completed segment, shouldn't trigger write to Sink.
    reset(mockedSink);
    sink.write(buffer, buffer.size());
    queueingExecutor.runAll();
    verify(mockedSink, never()).write(any(Buffer.class), anyLong());
    verify(mockedSink, never()).flush();
    assertThat(buffer.exhausted()).isTrue();

    // flush should write everything.
    sink.flush();
    queueingExecutor.runAll();
    verify(mockedSink).write(any(Buffer.class), eq((long) payloadSize - completeSegmentBytes));
    verify(mockedSink).flush();
  }

  @Test
  public void writeAndFlush_beforeConnected() throws IOException {
    Buffer buffer = new Buffer();
    sink.write(buffer.writeUtf8("hello"), buffer.size());
    sink.flush();
    queueingExecutor.runAll();

    verify(mockedSink, never()).write(any(Buffer.class), anyLong());
    verify(mockedSink, never()).flush();

    ArgumentCaptor<Throwable> captor = ArgumentCaptor.forClass(Throwable.class);

    verify(exceptionHandler).onException(captor.capture());

    Throwable t = captor.getValue();
    assertThat(t).isInstanceOf(IOException.class);
    assertThat(t).hasMessageThat().contains("unavailable sink");
  }

  @Test
  public void close_multipleCloseShouldNotThrow() throws IOException {
    sink.becomeConnected(mockedSink, socket);

    sink.close();
    queueingExecutor.runAll();

    verify(exceptionHandler, never()).onException(any(Throwable.class));

    sink.close();
    queueingExecutor.runAll();

    verify(exceptionHandler, never()).onException(any(Throwable.class));
  }

  /**
   * Executor queues incoming runnables instead of running it. Runnables can be invoked via {@link
   * QueueingExecutor#runAll} in serial order.
   */
  private static class QueueingExecutor implements Executor {

    private final Queue<Runnable> runnables = new ConcurrentLinkedQueue<>();

    @Override
    public void execute(Runnable command) {
      runnables.add(command);
    }

    public void runAll() {
      Runnable r;
      while ((r = runnables.poll()) != null) {
        r.run();
      }
    }
  }

  /** Test sink to mimic real Sink behavior since write has a side effect. */
  private static class VoidSink implements Sink {

    @Override
    public void write(Buffer source, long byteCount) throws IOException {
      // removes byteCount bytes from source.
      source.read(new byte[(int) byteCount], 0, (int) byteCount);
    }

    @Override
    public void flush() throws IOException {
      // do nothing
    }

    @Override
    public Timeout timeout() {
      return Timeout.NONE;
    }

    @Override
    public void close() throws IOException {
      // do nothing
    }
  }
}
