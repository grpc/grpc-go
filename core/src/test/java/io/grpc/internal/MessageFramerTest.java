/*
 * Copyright 2014 The gRPC Authors
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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import io.grpc.Codec;
import io.grpc.StreamTracer;
import io.grpc.internal.testing.TestStreamTracer.TestBaseStreamTracer;
import java.io.BufferedInputStream;
import java.io.ByteArrayInputStream;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.util.Arrays;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.junit.MockitoJUnit;
import org.mockito.junit.MockitoRule;

/**
 * Tests for {@link MessageFramer}.
 */
@RunWith(JUnit4.class)
public class MessageFramerTest {
  @Rule
  public final MockitoRule mocks = MockitoJUnit.rule();
  @Mock
  private MessageFramer.Sink sink;

  private final TestBaseStreamTracer tracer = new TestBaseStreamTracer();
  private MessageFramer framer;

  @Captor
  private ArgumentCaptor<ByteWritableBuffer> frameCaptor;
  private BytesWritableBufferAllocator allocator =
      new BytesWritableBufferAllocator(1000, 1000);
  private StatsTraceContext statsTraceCtx;

  /** Set up for test. */
  @Before
  public void setUp() {
    // MessageDeframerTest tests with a client-side StatsTraceContext, so here we test with a
    // server-side StatsTraceContext.
    statsTraceCtx = new StatsTraceContext(new StreamTracer[]{tracer});
    framer = new MessageFramer(sink, allocator, statsTraceCtx);
  }

  @Test
  public void simplePayload() {
    writeKnownLength(framer, new byte[]{3, 14});
    verifyNoMoreInteractions(sink);
    framer.flush();

    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 2, 3, 14}), false, true, 1);
    assertEquals(1, allocator.allocCount);
    verifyNoMoreInteractions(sink);
    checkStats(2, 2);
  }

  @Test
  public void simpleUnknownLengthPayload() {
    writeUnknownLength(framer, new byte[]{3, 14});
    framer.flush();
    // Header is written first, then payload
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 2}), false, false, 0);
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {3, 14}), false, true, 1);
    assertEquals(2, allocator.allocCount);
    verifyNoMoreInteractions(sink);
    checkStats(2, 2);
  }

  @Test
  public void smallPayloadsShouldBeCombined() {
    writeKnownLength(framer, new byte[]{3});
    verifyNoMoreInteractions(sink);
    writeKnownLength(framer, new byte[]{14});
    verifyNoMoreInteractions(sink);
    framer.flush();
    verify(sink).deliverFrame(
        toWriteBuffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 1, 14}), false, true, 2);
    verifyNoMoreInteractions(sink);
    assertEquals(1, allocator.allocCount);
    checkStats(1, 1, 1, 1);
  }

  @Test
  public void closeCombinedWithFullSink() {
    writeKnownLength(framer, new byte[]{3, 14, 1, 5, 9, 2, 6});
    verifyNoMoreInteractions(sink);
    framer.close();
    verify(sink).deliverFrame(
        toWriteBuffer(new byte[] {0, 0, 0, 0, 7, 3, 14, 1, 5, 9, 2, 6}), true, true, 1);
    verifyNoMoreInteractions(sink);
    assertEquals(1, allocator.allocCount);
    checkStats(7, 7);
  }

  @Test
  public void closeWithoutBufferedFrameGivesNullBuffer() {
    framer.close();
    verify(sink).deliverFrame(null, true, true, 0);
    verifyNoMoreInteractions(sink);
    assertEquals(0, allocator.allocCount);
    checkStats();
  }

  @Test
  public void payloadSplitBetweenSinks() {
    allocator = new BytesWritableBufferAllocator(12, 12);
    framer = new MessageFramer(sink, allocator, statsTraceCtx);
    writeKnownLength(framer, new byte[]{3, 14, 1, 5, 9, 2, 6, 5});
    verify(sink).deliverFrame(
        toWriteBuffer(new byte[] {0, 0, 0, 0, 8, 3, 14, 1, 5, 9, 2, 6}), false, false, 1);
    verifyNoMoreInteractions(sink);

    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {5}), false, true, 0);
    verifyNoMoreInteractions(sink);
    assertEquals(2, allocator.allocCount);
    checkStats(8, 8);
  }

  @Test
  public void frameHeaderSplitBetweenSinks() {
    allocator = new BytesWritableBufferAllocator(12, 12);
    framer = new MessageFramer(sink, allocator, statsTraceCtx);
    writeKnownLength(framer, new byte[]{3, 14, 1});
    writeKnownLength(framer, new byte[]{3});
    verify(sink).deliverFrame(
            toWriteBuffer(new byte[] {0, 0, 0, 0, 3, 3, 14, 1, 0, 0, 0, 0}), false, false, 2);
    verifyNoMoreInteractions(sink);

    framer.flush();
    verify(sink).deliverFrame(toWriteBufferWithMinSize(new byte[] {1, 3}, 12), false, true, 0);
    verifyNoMoreInteractions(sink);
    assertEquals(2, allocator.allocCount);
    checkStats(3, 3, 1, 1);
  }

  @Test
  public void emptyPayloadYieldsFrame() {
    writeKnownLength(framer, new byte[0]);
    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 0}), false, true, 1);
    assertEquals(1, allocator.allocCount);
    checkStats(0, 0);
  }

  @Test
  public void emptyUnknownLengthPayloadYieldsFrame() {
    writeUnknownLength(framer, new byte[0]);
    verifyZeroInteractions(sink);
    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 0}), false, true, 1);
    // One alloc for the header
    assertEquals(1, allocator.allocCount);
    checkStats(0, 0);
  }

  @Test
  public void flushIsIdempotent() {
    writeKnownLength(framer, new byte[]{3, 14});
    framer.flush();
    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 2, 3, 14}), false, true, 1);
    verifyNoMoreInteractions(sink);
    assertEquals(1, allocator.allocCount);
    checkStats(2, 2);
  }

  @Test
  public void largerFrameSize() {
    allocator = new BytesWritableBufferAllocator(0, 10000);
    framer = new MessageFramer(sink, allocator, statsTraceCtx);
    writeKnownLength(framer, new byte[1000]);
    framer.flush();
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false), eq(true), eq(1));
    ByteWritableBuffer buffer = frameCaptor.getValue();
    assertEquals(1005, buffer.size());

    byte[] data = new byte[1005];
    data[3] = 3;
    data[4] = (byte) 232;

    assertEquals(toWriteBuffer(data), buffer);
    verifyNoMoreInteractions(sink);
    assertEquals(1, allocator.allocCount);
    checkStats(1000, 1000);
  }

  @Test
  public void largerFrameSizeUnknownLength() {
    // Force payload to be split into two chunks
    allocator = new BytesWritableBufferAllocator(500, 500);
    framer = new MessageFramer(sink, allocator, statsTraceCtx);
    writeUnknownLength(framer, new byte[1000]);
    framer.flush();
    // Header and first chunk written with flush = false
    verify(sink, times(2)).deliverFrame(frameCaptor.capture(), eq(false), eq(false), eq(0));
    // On flush third buffer written with flish = true
    // The message count is only bumped when a message is completely written.
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false), eq(true), eq(1));

    // header has fixed length of 5 and specifies correct length
    assertEquals(5, frameCaptor.getAllValues().get(0).readableBytes());
    byte[] data = new byte[5];
    data[3] = 3;
    data[4] = (byte) 232;
    assertEquals(toWriteBuffer(data), frameCaptor.getAllValues().get(0));

    assertEquals(500, frameCaptor.getAllValues().get(1).readableBytes());
    assertEquals(500, frameCaptor.getAllValues().get(2).readableBytes());

    verifyNoMoreInteractions(sink);
    assertEquals(3, allocator.allocCount);
    checkStats(1000, 1000);
  }

  @Test
  public void compressed() {
    allocator = new BytesWritableBufferAllocator(100, Integer.MAX_VALUE);
    // setMessageCompression should default to true
    framer = new MessageFramer(sink, allocator, statsTraceCtx)
        .setCompressor(new Codec.Gzip());
    writeKnownLength(framer, new byte[1000]);
    framer.flush();
    // The GRPC header is written first as a separate frame.
    // The message count is only bumped when a message is completely written.
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false), eq(false), eq(0));
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false), eq(true), eq(1));

    // Check the header
    ByteWritableBuffer buffer = frameCaptor.getAllValues().get(0);

    assertEquals(0x1, buffer.data[0]);
    ByteBuffer byteBuf = ByteBuffer.wrap(buffer.data, 1, 4);
    byteBuf.order(ByteOrder.BIG_ENDIAN);
    int length = byteBuf.getInt();
    // compressed data should be smaller than uncompressed data.
    assertTrue(length < 1000);

    assertEquals(frameCaptor.getAllValues().get(1).size(), length);
    checkStats(length, 1000);
  }

  @Test
  public void dontCompressIfNoEncoding() {
    allocator = new BytesWritableBufferAllocator(100, Integer.MAX_VALUE);
    framer = new MessageFramer(sink, allocator, statsTraceCtx)
        .setMessageCompression(true);
    writeKnownLength(framer, new byte[1000]);
    framer.flush();
    // The GRPC header is written first as a separate frame
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false), eq(true), eq(1));

    // Check the header
    ByteWritableBuffer buffer = frameCaptor.getAllValues().get(0);
    // We purposefully don't check the last byte of length, since that depends on how exactly it
    // compressed.

    assertEquals(0x0, buffer.data[0]);
    ByteBuffer byteBuf = ByteBuffer.wrap(buffer.data, 1, 4);
    byteBuf.order(ByteOrder.BIG_ENDIAN);
    int length = byteBuf.getInt();
    assertEquals(1000, length);

    assertEquals(buffer.data.length - 5 , length);
    checkStats(1000, 1000);
  }

  @Test
  public void dontCompressIfNotRequested() {
    allocator = new BytesWritableBufferAllocator(100, Integer.MAX_VALUE);
    framer = new MessageFramer(sink, allocator, statsTraceCtx)
        .setCompressor(new Codec.Gzip())
        .setMessageCompression(false);
    writeKnownLength(framer, new byte[1000]);
    framer.flush();
    // The GRPC header is written first as a separate frame
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false), eq(true), eq(1));

    // Check the header
    ByteWritableBuffer buffer = frameCaptor.getAllValues().get(0);
    // We purposefully don't check the last byte of length, since that depends on how exactly it
    // compressed.

    assertEquals(0x0, buffer.data[0]);
    ByteBuffer byteBuf = ByteBuffer.wrap(buffer.data, 1, 4);
    byteBuf.order(ByteOrder.BIG_ENDIAN);
    int length = byteBuf.getInt();
    assertEquals(1000, length);

    assertEquals(buffer.data.length - 5 , length);
    checkStats(1000, 1000);
  }

  @Test
  public void closeIsRentrantSafe() {
    MessageFramer.Sink reentrant = new MessageFramer.Sink() {
      int count = 0;
      @Override
      public void deliverFrame(
          WritableBuffer frame, boolean endOfStream, boolean flush, int numMessages) {
        if (count == 0) {
          framer.close();
          count++;
        } else {
          fail("received event from reentrant call to close");
        }
      }
    };
    framer = new MessageFramer(reentrant, allocator, statsTraceCtx);
    writeKnownLength(framer, new byte[]{3, 14});
    framer.close();
  }

  @Test
  public void zeroLengthCompressibleMessageIsNotCompressed() {
    framer.setCompressor(new Codec.Gzip());
    framer.setMessageCompression(true);
    writeKnownLength(framer, new byte[]{});
    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 0}), false, true, 1);
    checkStats(0, 0);
  }

  private static WritableBuffer toWriteBuffer(byte[] data) {
    return toWriteBufferWithMinSize(data, 0);
  }

  private static WritableBuffer toWriteBufferWithMinSize(byte[] data, int minFrameSize) {
    ByteWritableBuffer buffer = new ByteWritableBuffer(Math.max(data.length, minFrameSize));
    buffer.write(data, 0, data.length);
    return buffer;
  }

  private static void writeUnknownLength(MessageFramer framer, byte[] bytes) {
    framer.writePayload(new BufferedInputStream(new ByteArrayInputStream(bytes)));
  }

  private static void writeKnownLength(MessageFramer framer, byte[] bytes) {
    framer.writePayload(new ByteArrayInputStream(bytes));
    // TODO(carl-mastrangelo): add framer.flush() here.
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
      assertEquals("outboundMessage(" + i + ")", tracer.nextOutboundEvent());
      assertEquals(
          String.format("outboundMessageSent(%d, %d, %d)", i, sizes[i * 2], sizes[i * 2 + 1]),
          tracer.nextOutboundEvent());
      expectedWireSize += sizes[i * 2];
      expectedUncompressedSize += sizes[i * 2 + 1];
    }
    assertNull(tracer.nextOutboundEvent());
    assertNull(tracer.nextInboundEvent());
    assertEquals(expectedWireSize, tracer.getOutboundWireSize());
    assertEquals(expectedUncompressedSize, tracer.getOutboundUncompressedSize());
  }

  static class ByteWritableBuffer implements WritableBuffer {
    byte[] data;
    private int writeIdx;

    ByteWritableBuffer(int maxFrameSize) {
      data = new byte[maxFrameSize];
    }

    @Override
    public void write(byte[] bytes, int srcIndex, int length) {
      System.arraycopy(bytes, srcIndex, data, writeIdx, length);
      writeIdx += length;
    }

    @Override
    public void write(byte b) {
      data[writeIdx++] = b;
    }

    @Override
    public int writableBytes() {
      return data.length - writeIdx;
    }

    @Override
    public int readableBytes() {
      return writeIdx;
    }

    @Override
    public void release() {
      data = null;
    }

    int size() {
      return writeIdx;
    }

    @Override
    public boolean equals(Object buffer) {
      if (!(buffer instanceof ByteWritableBuffer)) {
        return false;
      }

      ByteWritableBuffer other = (ByteWritableBuffer) buffer;

      return readableBytes() == other.readableBytes()
          && Arrays.equals(Arrays.copyOf(data, readableBytes()),
            Arrays.copyOf(other.data, readableBytes()));
    }

    @Override
    public int hashCode() {
      return Arrays.hashCode(data) + writableBytes() + readableBytes();
    }
  }

  static class BytesWritableBufferAllocator implements WritableBufferAllocator {
    public int minSize;
    public int maxSize;
    public int allocCount = 0;

    BytesWritableBufferAllocator(int minSize, int maxSize) {
      this.minSize = minSize;
      this.maxSize = maxSize;
    }

    @Override
    public WritableBuffer allocate(int capacityHint) {
      allocCount++;
      return new ByteWritableBuffer(Math.min(maxSize, Math.max(capacityHint, minSize)));
    }
  }
}
