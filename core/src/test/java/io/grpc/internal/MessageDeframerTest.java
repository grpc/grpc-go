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

package io.grpc.internal;

import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.anyInt;
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
import io.grpc.internal.testing.TestStreamTracer.TestBaseStreamTracer;
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
  private TestBaseStreamTracer tracer = new TestBaseStreamTracer();
  private StatsTraceContext statsTraceCtx = new StatsTraceContext(new StreamTracer[]{tracer});

  private MessageDeframer deframer = new MessageDeframer(listener, Codec.Identity.NONE,
      DEFAULT_MAX_MESSAGE_SIZE, statsTraceCtx, "test");

  private ArgumentCaptor<StreamListener.MessageProducer> producer =
      ArgumentCaptor.forClass(StreamListener.MessageProducer.class);

  @Test
  public void simplePayload() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 2, 3, 14}));
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(Bytes.asList(new byte[] {3, 14}), bytes(producer.getValue().next()));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(2, 2);
  }

  @Test
  public void smallCombinedPayloads() {
    deframer.request(2);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}));
    verify(listener, times(2)).messagesAvailable(producer.capture());
    List<StreamListener.MessageProducer> streams = producer.getAllValues();
    assertEquals(2, streams.size());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(streams.get(0).next()));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    assertEquals(Bytes.asList(new byte[] {14, 15}), bytes(streams.get(1).next()));
    verifyNoMoreInteractions(listener);
    checkStats(1, 1, 2, 2);
  }

  @Test
  public void endOfStreamWithPayloadShouldNotifyEndOfStream() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}));
    deframer.closeWhenComplete();
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(producer.getValue().next()));
    verify(listener).deframerClosed(false);
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, 1);
  }

  @Test
  public void endOfStreamShouldNotifyEndOfStream() {
    deframer.deframe(buffer(new byte[0]));
    deframer.closeWhenComplete();
    verify(listener).deframerClosed(false);
    verifyNoMoreInteractions(listener);
    checkStats();
  }

  @Test
  public void endOfStreamWithPartialMessageShouldNotifyDeframerClosedWithPartialMessage() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[1]));
    deframer.closeWhenComplete();
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verify(listener).deframerClosed(true);
    verifyNoMoreInteractions(listener);
    checkStats();
  }

  @Test
  public void payloadSplitBetweenBuffers() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 7, 3, 14, 1, 5, 9}));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    deframer.deframe(buffer(new byte[] {2, 6}));
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(
        Bytes.asList(new byte[] {3, 14, 1, 5, 9, 2, 6}), bytes(producer.getValue().next()));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(7, 7);
  }

  @Test
  public void frameHeaderSplitBetweenBuffers() {
    deframer.request(1);

    deframer.deframe(buffer(new byte[] {0, 0}));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    deframer.deframe(buffer(new byte[] {0, 0, 1, 3}));
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(producer.getValue().next()));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, 1);
  }

  @Test
  public void emptyPayload() {
    deframer.request(1);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 0}));
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(Bytes.asList(), bytes(producer.getValue().next()));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(0, 0);
  }

  @Test
  public void largerFrameSize() {
    deframer.request(1);
    deframer.deframe(ReadableBuffers.wrap(
        Bytes.concat(new byte[] {0, 0, 0, 3, (byte) 232}, new byte[1000])));
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(Bytes.asList(new byte[1000]), bytes(producer.getValue().next()));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1000, 1000);
  }

  @Test
  public void endOfStreamCallbackShouldWaitForMessageDelivery() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}));
    deframer.closeWhenComplete();
    verifyNoMoreInteractions(listener);

    deframer.request(1);
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(producer.getValue().next()));
    verify(listener).deframerClosed(false);
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(1, 1);
  }

  @Test
  public void compressed() {
    deframer = new MessageDeframer(listener, new Codec.Gzip(), DEFAULT_MAX_MESSAGE_SIZE,
        statsTraceCtx, "test");
    deframer.request(1);

    byte[] payload = compress(new byte[1000]);
    assertTrue(payload.length < 100);
    byte[] header = new byte[] {1, 0, 0, 0, (byte) payload.length};
    deframer.deframe(buffer(Bytes.concat(header, payload)));
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(Bytes.asList(new byte[1000]), bytes(producer.getValue().next()));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    checkStats(payload.length, 1000);
  }

  @Test
  public void deliverIsReentrantSafe() {
    doAnswer(
          new Answer<Void>() {
            @Override
            public Void answer(InvocationOnMock invocation) throws Throwable {
              deframer.request(1);
              return null;
            }
          })
        .when(listener)
        .messagesAvailable(Matchers.<StreamListener.MessageProducer>any());
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}));
    deframer.closeWhenComplete();
    verifyNoMoreInteractions(listener);

    deframer.request(1);
    verify(listener).messagesAvailable(producer.capture());
    assertEquals(Bytes.asList(new byte[] {3}), bytes(producer.getValue().next()));
    verify(listener).deframerClosed(false);
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
    checkSizeEnforcingInputStreamStats(3);
  }

  @Test
  public void sizeEnforcingInputStream_readByteAtLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 3, statsTraceCtx, "test");

    while (stream.read() != -1) {}

    stream.close();
    checkSizeEnforcingInputStreamStats(3);
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
    checkSizeEnforcingInputStreamStats(3);
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
    checkSizeEnforcingInputStreamStats(3);
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
    checkSizeEnforcingInputStreamStats(3);
  }

  @Test
  public void sizeEnforcingInputStream_skipAtLimit() throws IOException {
    ByteArrayInputStream in = new ByteArrayInputStream("foo".getBytes(Charsets.UTF_8));
    SizeEnforcingInputStream stream =
        new MessageDeframer.SizeEnforcingInputStream(in, 3, statsTraceCtx, "test");

    long skipped = stream.skip(4);

    assertEquals(3, skipped);
    stream.close();
    checkSizeEnforcingInputStreamStats(3);
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
    checkSizeEnforcingInputStreamStats(3);
  }

  /**
   * @param sizes in the format {wire0, uncompressed0, wire1, uncompressed1, ...}
   */
  private void checkStats(long... sizes) {
    assertEquals(0, sizes.length % 2);
    int count = sizes.length / 2;
    long expectedWireSize = 0;
    long expectedUncompressedSize = 0;
    for (int i = 0; i < count; i++) {
      assertEquals("inboundMessage(" + i + ")", tracer.nextInboundEvent());
      assertEquals("inboundMessage()", tracer.nextInboundEvent());
      assertEquals(
          String.format("inboundMessageRead(%d, %d, -1)", i, sizes[i * 2]),
          tracer.nextInboundEvent());
      expectedWireSize += sizes[i * 2];
      expectedUncompressedSize += sizes[i * 2 + 1];
    }
    assertNull(tracer.nextInboundEvent());
    assertNull(tracer.nextOutboundEvent());
    assertEquals(expectedWireSize, tracer.getInboundWireSize());
    assertEquals(expectedUncompressedSize, tracer.getInboundUncompressedSize());
  }

  private void checkSizeEnforcingInputStreamStats(long uncompressedSize) {
    assertNull(tracer.nextInboundEvent());
    assertNull(tracer.nextOutboundEvent());
    assertEquals(0, tracer.getInboundWireSize());
    // SizeEnforcingInputStream only reports uncompressed bytes
    assertEquals(uncompressedSize, tracer.getInboundUncompressedSize());
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
