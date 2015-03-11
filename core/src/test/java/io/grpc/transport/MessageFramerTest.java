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

package io.grpc.transport;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static io.grpc.transport.MessageFramer.Compression;

import com.google.common.base.Preconditions;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.ByteArrayInputStream;
import java.util.Arrays;

/**
 * Tests for {@link MessageFramer}
 */
@RunWith(JUnit4.class)
public class MessageFramerTest {
  private static final int TRANSPORT_FRAME_SIZE = 12;

  @Mock
  private MessageFramer.Sink sink;
  private MessageFramer framer;

  @Captor
  private ArgumentCaptor<ByteWritableBuffer> frameCaptor;
  private WritableBufferAllocator allocator = new BytesWritableBufferAllocator();

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);

    framer = new MessageFramer(sink, allocator, TRANSPORT_FRAME_SIZE);
  }

  @Test
  public void simplePayload() {
    writePayload(framer, new byte[] {3, 14});
    verifyNoMoreInteractions(sink);
    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 2, 3, 14}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void smallPayloadsShouldBeCombined() {
    writePayload(framer, new byte[] {3});
    verifyNoMoreInteractions(sink);
    writePayload(framer, new byte[] {14});
    verifyNoMoreInteractions(sink);
    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 1, 14}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void closeCombinedWithFullSink() {
    writePayload(framer, new byte[] {3, 14, 1, 5, 9, 2, 6});
    verifyNoMoreInteractions(sink);
    framer.close();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 7, 3, 14, 1, 5, 9, 2, 6}), true);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void closeWithoutBufferedFrameGivesEmptySink() {
    framer.close();
    verify(sink).deliverFrame(new ByteWritableBuffer(0), true);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void payloadSplitBetweenSinks() {
    writePayload(framer, new byte[] {3, 14, 1, 5, 9, 2, 6, 5});
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 8, 3, 14, 1, 5, 9, 2, 6}), false);
    verifyNoMoreInteractions(sink);

    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {5}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void frameHeaderSplitBetweenSinks() {
    writePayload(framer, new byte[] {3, 14, 1});
    writePayload(framer, new byte[] {3});
    verify(sink).deliverFrame(
            toWriteBuffer(new byte[] {0, 0, 0, 0, 3, 3, 14, 1, 0, 0, 0, 0}), false);
    verifyNoMoreInteractions(sink);

    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {1, 3}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void emptyPayloadYieldsFrame() throws Exception {
    writePayload(framer, new byte[0]);
    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 0}), false);
  }

  @Test
  public void flushIsIdempotent() {
    writePayload(framer, new byte[] {3, 14});
    framer.flush();
    framer.flush();
    verify(sink).deliverFrame(toWriteBuffer(new byte[] {0, 0, 0, 0, 2, 3, 14}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void largerFrameSize() throws Exception {
    final int transportFrameSize = 10000;
    MessageFramer framer = new MessageFramer(sink, allocator, transportFrameSize);
    writePayload(framer, new byte[1000]);
    framer.flush();
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false));
    ByteWritableBuffer buffer = frameCaptor.getValue();
    assertEquals(1005, buffer.size());

    byte data[] = new byte[1005];
    data[3] = 3;
    data[4] = (byte) 232;

    assertEquals(toWriteBuffer(data, transportFrameSize), buffer);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void compressed() throws Exception {
    final int transportFrameSize = 100;
    MessageFramer framer =
            new MessageFramer(sink, allocator, transportFrameSize, Compression.GZIP);
    writePayload(framer, new byte[1000]);
    framer.flush();
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false));
    ByteWritableBuffer buffer = frameCaptor.getValue();
    // It should have compressed very well.
    assertTrue(buffer.size() < 100);
    // We purposefully don't check the last byte of length, since that depends on how exactly it
    // compressed.
    assertEquals(1, buffer.data[0]);
    assertEquals(0, buffer.data[1]);
    assertEquals(0, buffer.data[2]);
    assertEquals(0, buffer.data[3]);
  }

  private static WritableBuffer toWriteBuffer(byte[] data) {
    return toWriteBuffer(data, TRANSPORT_FRAME_SIZE);
  }

  private static WritableBuffer toWriteBuffer(byte[] data, int maxFrameSize) {
    ByteWritableBuffer buffer = new ByteWritableBuffer(maxFrameSize);
    buffer.write(data, 0, data.length);
    return buffer;
  }

  private static void writePayload(MessageFramer framer, byte[] bytes) {
    framer.writePayload(new ByteArrayInputStream(bytes), bytes.length);
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

      return writableBytes() == other.writableBytes() &&
             readableBytes() == other.readableBytes() &&
             Arrays.equals(data, other.data);
    }

    @Override
    public int hashCode() {
      return Arrays.hashCode(data) + writableBytes() + readableBytes();
    }
  }

  static class BytesWritableBufferAllocator implements WritableBufferAllocator {

    @Override
    public WritableBuffer allocate(int maxCapacity) {
      return new ByteWritableBuffer(maxCapacity);
    }
  }
}
