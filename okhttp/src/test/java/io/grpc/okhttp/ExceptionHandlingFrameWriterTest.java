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

import static com.google.common.base.Charsets.UTF_8;
import static com.google.common.truth.Truth.assertThat;
import static io.grpc.okhttp.ExceptionHandlingFrameWriter.getLogLevel;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.grpc.okhttp.ExceptionHandlingFrameWriter.TransportExceptionHandler;
import io.grpc.okhttp.OkHttpFrameLogger.Direction;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.FrameWriter;
import io.grpc.okhttp.internal.framed.Header;
import io.grpc.okhttp.internal.framed.Settings;
import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.logging.Handler;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;
import okio.Buffer;
import okio.ByteString;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class ExceptionHandlingFrameWriterTest {

  private static final Logger logger = Logger.getLogger(OkHttpClientTransport.class.getName());
  private final FrameWriter mockedFrameWriter = mock(FrameWriter.class);
  private final TransportExceptionHandler transportExceptionHandler =
      mock(TransportExceptionHandler.class);
  private final ExceptionHandlingFrameWriter exceptionHandlingFrameWriter =
      new ExceptionHandlingFrameWriter(transportExceptionHandler, mockedFrameWriter,
          new OkHttpFrameLogger(Level.FINE, logger));

  @Test
  public void exception() throws IOException {
    IOException exception = new IOException("some exception");
    doThrow(exception).when(mockedFrameWriter)
        .synReply(false, 100, new ArrayList<Header>());

    exceptionHandlingFrameWriter.synReply(false, 100, new ArrayList<Header>());

    verify(transportExceptionHandler).onException(exception);
    verify(mockedFrameWriter).synReply(false, 100, new ArrayList<Header>());
  }

  @Test
  public void unknownException() {
    assertThat(getLogLevel(new Exception())).isEqualTo(Level.INFO);
  }

  @Test
  public void quiet() {
    assertThat(getLogLevel(new IOException("Socket closed"))).isEqualTo(Level.FINE);
  }

  @Test
  public void nonquiet() {
    assertThat(getLogLevel(new IOException("foo"))).isEqualTo(Level.INFO);
  }

  @Test
  public void nullMessage() {
    IOException e = new IOException();
    assertThat(e.getMessage()).isNull();
    assertThat(getLogLevel(e)).isEqualTo(Level.INFO);
  }

  private static Buffer createMessageFrame(String message) {
    return createMessageFrame(message.getBytes(UTF_8));
  }

  private static Buffer createMessageFrame(byte[] message) {
    Buffer buffer = new Buffer();
    buffer.writeByte(0 /* UNCOMPRESSED */);
    buffer.writeInt(message.length);
    buffer.write(message);
    return buffer;
  }

  /**
   * Test logging is functioning correctly for client sent Http/2 frames. Not intended to test
   * actual frame content being logged.
   */
  @Test
  public void testFrameLogger() {
    final List<LogRecord> logs = new ArrayList<>();
    Handler handler = new Handler() {
      @Override
      public void publish(LogRecord record) {
        logs.add(record);
      }

      @Override
      public void flush() {
      }

      @Override
      public void close() throws SecurityException {
      }
    };
    logger.addHandler(handler);
    logger.setLevel(Level.ALL);

    exceptionHandlingFrameWriter.headers(100, new ArrayList<Header>());
    assertThat(logs).hasSize(1);
    LogRecord log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " HEADERS: streamId=" + 100);
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    final String message = "Hello Server";
    Buffer buffer = createMessageFrame(message);
    exceptionHandlingFrameWriter.data(false, 100, buffer, (int) buffer.size());
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " DATA: streamId=" + 100);
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    // At most 64 bytes of data frame will be logged.
    exceptionHandlingFrameWriter
        .data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    String data = log.getMessage();
    assertThat(data).endsWith("...");
    assertThat(data.substring(data.indexOf("bytes="), data.indexOf("..."))).hasLength(64 * 2 + 6);

    exceptionHandlingFrameWriter.ackSettings(new Settings());
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " SETTINGS: ack=true");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    exceptionHandlingFrameWriter.settings(new Settings());
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " SETTINGS: ack=false");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    exceptionHandlingFrameWriter.ping(false, 0, 0);
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " PING: ack=false");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    exceptionHandlingFrameWriter.pushPromise(100, 100, new ArrayList<Header>());
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " PUSH_PROMISE");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    exceptionHandlingFrameWriter.rstStream(100, ErrorCode.CANCEL);
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " RST_STREAM");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    exceptionHandlingFrameWriter.goAway(100, ErrorCode.CANCEL, ByteString.EMPTY.toByteArray());
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " GO_AWAY");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    exceptionHandlingFrameWriter.windowUpdate(100, 32);
    assertThat(logs).hasSize(1);
    log = logs.remove(0);
    assertThat(log.getMessage()).startsWith(Direction.OUTBOUND + " WINDOW_UPDATE");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    logger.removeHandler(handler);
  }
}