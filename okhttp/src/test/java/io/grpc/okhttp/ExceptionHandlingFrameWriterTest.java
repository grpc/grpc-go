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
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.startsWith;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.okhttp.ExceptionHandlingFrameWriter.TransportExceptionHandler;
import io.grpc.okhttp.OkHttpFrameLogger.Direction;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.FrameWriter;
import io.grpc.okhttp.internal.framed.Header;
import io.grpc.okhttp.internal.framed.Settings;
import java.io.IOException;
import java.util.ArrayList;
import java.util.logging.Level;
import java.util.logging.Logger;
import okio.Buffer;
import okio.ByteString;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

@RunWith(JUnit4.class)
public class ExceptionHandlingFrameWriterTest {

  private final FrameWriter mockedFrameWriter = mock(FrameWriter.class);
  private final TransportExceptionHandler transportExceptionHandler =
      mock(TransportExceptionHandler.class);
  private final Logger log = mock(Logger.class);
  private final ExceptionHandlingFrameWriter exceptionHandlingFrameWriter =
      new ExceptionHandlingFrameWriter(transportExceptionHandler, mockedFrameWriter,
          new OkHttpFrameLogger(Level.FINE, log));

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
    when(log.isLoggable(any(Level.class))).thenReturn(true);

    exceptionHandlingFrameWriter.headers(100, new ArrayList<Header>());
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " HEADERS: streamId=" + 100));

    final String message = "Hello Server";
    Buffer buffer = createMessageFrame(message);
    exceptionHandlingFrameWriter.data(false, 100, buffer, (int) buffer.size());
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " DATA: streamId=" + 100));

    // At most 64 bytes of data frame will be logged.
    exceptionHandlingFrameWriter
        .data(false, 3, createMessageFrame(new String(new char[1000])), 1000);
    ArgumentCaptor<String> logCaptor = ArgumentCaptor.forClass(String.class);
    verify(log, atLeastOnce()).log(any(Level.class), logCaptor.capture());
    String data = logCaptor.getValue();
    assertThat(data).endsWith("...");
    assertThat(data.substring(data.indexOf("bytes="), data.indexOf("..."))).hasLength(64 * 2 + 6);

    exceptionHandlingFrameWriter.ackSettings(new Settings());
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " SETTINGS: ack=true"));

    exceptionHandlingFrameWriter.settings(new Settings());
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " SETTINGS: ack=false"));

    exceptionHandlingFrameWriter.ping(false, 0, 0);
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " PING: ack=false"));

    exceptionHandlingFrameWriter.pushPromise(100, 100, new ArrayList<Header>());
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " PUSH_PROMISE"));

    exceptionHandlingFrameWriter.rstStream(100, ErrorCode.CANCEL);
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " RST_STREAM"));

    exceptionHandlingFrameWriter.goAway(100, ErrorCode.CANCEL, ByteString.EMPTY.toByteArray());
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " GO_AWAY"));

    exceptionHandlingFrameWriter.windowUpdate(100, 32);
    verify(log).log(any(Level.class), startsWith(Direction.OUTBOUND + " WINDOW_UPDATE"));

  }
}