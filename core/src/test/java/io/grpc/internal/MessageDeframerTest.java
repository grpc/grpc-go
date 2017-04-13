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

package io.grpc.internal;

import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.base.Charsets;
import com.google.common.io.ByteStreams;
import com.google.common.primitives.Bytes;
import io.grpc.Codec;
import io.grpc.StatusRuntimeException;
import io.grpc.StreamTracer;
import io.grpc.internal.MessageDeframer.Listener;
import io.grpc.internal.MessageDeframer.SizeEnforcingInputStream;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.List;
import java.util.zip.GZIPOutputStream;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Tests for {@link MessageDeframer}.
 */
@RunWith(JUnit4.class)
public class MessageDeframerTest {
  @Rule public final ExpectedException thrown = ExpectedException.none();

  private Listener listener = mock(Listener.class);
  private StreamTracer tracer = mock(StreamTracer.class);
  private StatsTraceContext statsTraceCtx = new StatsTraceContext(new StreamTracer[]{tracer});
  private ArgumentCaptor<Long> wireSizeCaptor = ArgumentCaptor.forClass(Long.class);
  private ArgumentCaptor<Long> uncompressedSizeCaptor = ArgumentCaptor.forClass(Long.class);

  private MessageDeframer deframer = new MessageDeframer(listener, Codec.Identity.NONE,
      DEFAULT_MAX_MESSAGE_SIZE, statsTraceCtx, "test");

  private ArgumentCaptor<InputStream> messages = ArgumentCaptor.forClass(InputStream.class);

  @Test
  public void simplePayload() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 2, 3, 14}), false);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(new byte[]{3, 14}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, 2, 2);
  }

  @Test
  public void smallCombinedPayloads() {
    deframer.request(2);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false);
    verify(listener, times(2)).messageRead(messages.capture());
    List<InputStream> streams = messages.getAllValues();
    assertEquals(2, streams.size());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(streams.get(0)));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    assertEquals(Bytes.asList(new byte[] {14, 15}), bytes(streams.get(1)));
    verifyNoMoreInteractions(listener);
    checkStats(2, 3, 3);
  }

  @Test
  public void endOfStreamWithPayloadShouldNotifyEndOfStream() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener).endOfStream();
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, 1, 1);
  }

  @Test
  public void endOfStreamShouldNotifyEndOfStream() {
    deframer.deframe(buffer(new byte[0]), true);
    verify(listener).endOfStream();
    verifyNoMoreInteractions(listener);
    checkStats(0, 0, 0);
  }

  @Test
  public void payloadSplitBetweenBuffers() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 7, 3, 14, 1, 5, 9}), false);
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    deframer.deframe(buffer(new byte[] {2, 6}), false);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(new byte[] {3, 14, 1, 5, 9, 2, 6}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    assertTrue(deframer.isStalled());
    verifyNoMoreInteractions(listener);
    checkStats(1, 7, 7);
  }

  @Test
  public void frameHeaderSplitBetweenBuffers() {
    deframer.request(1);

    deframer.deframe(buffer(new byte[] {0, 0}), false);
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    deframer.deframe(buffer(new byte[] {0, 0, 1, 3}), false);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    assertTrue(deframer.isStalled());
    verifyNoMoreInteractions(listener);
    checkStats(1, 1, 1);
  }

  @Test
  public void emptyPayload() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 0}), false);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, 0, 0);
  }

  @Test
  public void largerFrameSize() {
    deframer.request(1);
    deframer.deframe(ReadableBuffers.wrap(
        Bytes.concat(new byte[] {0, 0, 0, 3, (byte) 232}, new byte[1000])), false);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(new byte[1000]), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, 1000, 1000);
  }

  @Test
  public void endOfStreamCallbackShouldWaitForMessageDelivery() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    verifyNoMoreInteractions(listener);

    deframer.request(1);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener).endOfStream();
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, 1, 1);
  }

  @Test
  public void compressed() {
    deframer = new MessageDeframer(listener, new Codec.Gzip(), DEFAULT_MAX_MESSAGE_SIZE,
        statsTraceCtx, "test");
    deframer.request(1);

    byte[] payload = compress(new byte[1000]);
    assertTrue(payload.length < 100);
    byte[] header = new byte[] {1, 0, 0, 0, (byte) payload.length};
    deframer.deframe(buffer(Bytes.concat(header, payload)), false);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(new byte[1000]), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, payload.length, 1000);
  }

  @Test
  public void deliverIsReentrantSafe() {
    doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        deframer.request(1);
        return null;
      }
    }).when(listener).messageRead(Matchers.<InputStream>any());
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    verifyNoMoreInteractions(listener);

    deframer.request(1);
    verify(listener).messageRead(messages.capture());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener).endOfStream();
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void sizeEnforcingInputStream_readByteBelowLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 4, statsTraceCtx, "test");

    while (stream.read() != -1) {}

    stream.close();
    // SizeEnforcingInputStream only reports uncompressed bytes
    checkStats(0, 0, 3);
  }

  @Test
  public void sizeEnforcingInputStream_readByteAtLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 3, statsTraceCtx, "test");

    while (stream.read() != -1) {}

    stream.close();
    // SizeEnforcingInputStream only reports uncompressed bytes
    checkStats(0, 0, 3);
  }

  @Test
  public void sizeEnforcingInputStream_readByteAboveLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 2, statsTraceCtx, "test");

    try {
      thrown.expect(StatusRuntimeException.class);
      thrown.expectMessage("RESOURCE_EXHAUSTED: test: Compressed frame exceeds");

      while (stream.read() != -1) {}
    } finally {
      stream.close();
    }
  }

  @Test
  public void sizeEnforcingInputStream_readBelowLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 4, statsTraceCtx, "test");
    byte[] buf = new byte[10];

    int read = stream.read(buf, 0, buf.length);

    assertEquals(3, read);
    stream.close();
    // SizeEnforcingInputStream only reports uncompressed bytes
    checkStats(0, 0, 3);
  }

  @Test
  public void sizeEnforcingInputStream_readAtLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 3, statsTraceCtx, "test");
    byte[] buf = new byte[10];

    int read = stream.read(buf, 0, buf.length);

    assertEquals(3, read);
    stream.close();
    // SizeEnforcingInputStream only reports uncompressed bytes
    checkStats(0, 0, 3);
  }

  @Test
  public void sizeEnforcingInputStream_readAboveLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 2, statsTraceCtx, "test");
    byte[] buf = new byte[10];

    try {
      thrown.expect(StatusRuntimeException.class);
      thrown.expectMessage("RESOURCE_EXHAUSTED: test: Compressed frame exceeds");

      stream.read(buf, 0, buf.length);
    } finally {
      stream.close();
    }
  }

  @Test
  public void sizeEnforcingInputStream_skipBelowLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 4, statsTraceCtx, "test");

    long skipped = stream.skip(4);

    assertEquals(3, skipped);

    stream.close();
    // SizeEnforcingInputStream only reports uncompressed bytes
    checkStats(0, 0, 3);
  }

  @Test
  public void sizeEnforcingInputStream_skipAtLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 3, statsTraceCtx, "test");

    long skipped = stream.skip(4);

    assertEquals(3, skipped);
    stream.close();
    // SizeEnforcingInputStream only reports uncompressed bytes
    checkStats(0, 0, 3);
  }

  @Test
  public void sizeEnforcingInputStream_skipAboveLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 2, statsTraceCtx, "test");

    try {
      thrown.expect(StatusRuntimeException.class);
      thrown.expectMessage("RESOURCE_EXHAUSTED: test: Compressed frame exceeds");

      stream.skip(4);
    } finally {
      stream.close();
    }
  }

  @Test
  public void sizeEnforcingInputStream_markReset() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 3, statsTraceCtx, "test");
    // stream currently looks like: |foo
    stream.skip(1); // f|oo
    stream.mark(10); // any large number will work.
    stream.skip(2); // foo|
    stream.reset(); // f|oo
    long skipped = stream.skip(2); // foo|

    assertEquals(2, skipped);
    stream.close();
    // SizeEnforcingInputStream only reports uncompressed bytes
    checkStats(0, 0, 3);
  }

  private void checkStats(
      int messagesReceived, long wireBytesReceived, long uncompressedBytesReceived) {
    long actualWireSize = 0;
    long actualUncompressedSize = 0;

    verify(tracer, times(messagesReceived)).inboundMessage();
    verify(tracer, atLeast(0)).inboundWireSize(wireSizeCaptor.capture());
    for (Long portion : wireSizeCaptor.getAllValues()) {
      actualWireSize += portion;
    }

    verify(tracer, atLeast(0)).inboundUncompressedSize(uncompressedSizeCaptor.capture());
    for (Long portion : uncompressedSizeCaptor.getAllValues()) {
      actualUncompressedSize += portion;
    }

    verifyNoMoreInteractions(tracer);
    assertEquals(wireBytesReceived, actualWireSize);
    assertEquals(uncompressedBytesReceived, actualUncompressedSize);
  }

  private static List<Byte> bytes(ArgumentCaptor<InputStream> captor) {
    return bytes(captor.getValue());
  }

  private static List<Byte> bytes(InputStream in) {
    try {
      return Bytes.asList(ByteStreams.toByteArray(in));
    } catch (IOException ex) {
      throw new AssertionError(ex);
    }
  }

  private static ReadableBuffer buffer(byte[] bytes) {
    return ReadableBuffers.wrap(bytes);
  }

  private static byte[] compress(byte[] bytes) {
    try {
      ByteArrayOutputStream baos = new ByteArrayOutputStream();
      GZIPOutputStream zip = new GZIPOutputStream(baos);
      zip.write(bytes);
      zip.close();
      return baos.toByteArray();
    } catch (IOException ex) {
      throw new RuntimeException(ex);
    }
  }
}
