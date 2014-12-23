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

package com.google.net.stubby.transport;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.primitives.Bytes;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.ByteArrayInputStream;
import java.nio.ByteBuffer;
import java.util.List;

/**
 * Tests for {@link MessageFramer2}
 */
@RunWith(JUnit4.class)
public class MessageFramer2Test {
  private static final int TRANSPORT_FRAME_SIZE = 12;

  @Mock
  private MessageFramer2.Sink<List<Byte>> sink;
  private MessageFramer2.Sink<ByteBuffer> copyingSink;
  private MessageFramer2 framer;

  @Captor
  private ArgumentCaptor<List<Byte>> frameCaptor;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);

    copyingSink = new ByteArrayConverterSink(sink);
    framer = new MessageFramer2(copyingSink, TRANSPORT_FRAME_SIZE);
  }

  @Test
  public void simplePayload() {
    writePayload(framer, new byte[] {3, 14});
    verifyNoMoreInteractions(sink);
    framer.flush();
    verify(sink).deliverFrame(Bytes.asList(new byte[] {0, 0, 0, 0, 2, 3, 14}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void smallPayloadsShouldBeCombined() {
    writePayload(framer, new byte[] {3});
    verifyNoMoreInteractions(sink);
    writePayload(framer, new byte[] {14});
    verifyNoMoreInteractions(sink);
    framer.flush();
    verify(sink).deliverFrame(
        Bytes.asList(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 1, 14}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void closeCombinedWithFullSink() {
    writePayload(framer, new byte[] {3, 14, 1, 5, 9, 2, 6});
    verifyNoMoreInteractions(sink);
    framer.close();
    verify(sink).deliverFrame(Bytes.asList(new byte[] {0, 0, 0, 0, 7, 3, 14, 1, 5, 9, 2, 6}), true);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void closeWithoutBufferedFrameGivesEmptySink() {
    framer.close();
    verify(sink).deliverFrame(Bytes.asList(), true);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void payloadSplitBetweenSinks() {
    writePayload(framer, new byte[] {3, 14, 1, 5, 9, 2, 6, 5});
    verify(sink).deliverFrame(
        Bytes.asList(new byte[] {0, 0, 0, 0, 8, 3, 14, 1, 5, 9, 2, 6}), false);
    verifyNoMoreInteractions(sink);

    framer.flush();
    verify(sink).deliverFrame(Bytes.asList(new byte[] {5}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void frameHeaderSplitBetweenSinks() {
    writePayload(framer, new byte[] {3, 14, 1});
    writePayload(framer, new byte[] {3});
    verify(sink).deliverFrame(
        Bytes.asList(new byte[] {0, 0, 0, 0, 3, 3, 14, 1, 0, 0, 0, 0}), false);
    verifyNoMoreInteractions(sink);

    framer.flush();
    verify(sink).deliverFrame(Bytes.asList(new byte[] {1, 3}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void emptyPayloadYieldsFrame() throws Exception {
    writePayload(framer, new byte[0]);
    framer.flush();
    verify(sink).deliverFrame(Bytes.asList(new byte[] {0, 0, 0, 0, 0}), false);
  }

  @Test
  public void flushIsIdempotent() {
    writePayload(framer, new byte[] {3, 14});
    framer.flush();
    framer.flush();
    verify(sink).deliverFrame(Bytes.asList(new byte[] {0, 0, 0, 0, 2, 3, 14}), false);
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void largerFrameSize() throws Exception {
    final int transportFrameSize = 10000;
    MessageFramer2 framer = new MessageFramer2(copyingSink, transportFrameSize);
    writePayload(framer, new byte[1000]);
    framer.flush();
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false));
    List<Byte> buffer = frameCaptor.getValue();
    assertEquals(1005, buffer.size());
    assertEquals(Bytes.asList(new byte[] {0, 0, 0, 3, (byte) 232}), buffer.subList(0, 5));
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void compressed() throws Exception {
    final int transportFrameSize = 100;
    MessageFramer2 framer = new MessageFramer2(copyingSink, transportFrameSize,
        MessageFramer2.Compression.GZIP);
    writePayload(framer, new byte[1000]);
    framer.flush();
    verify(sink).deliverFrame(frameCaptor.capture(), eq(false));
    List<Byte> buffer = frameCaptor.getValue();
    // It should have compressed very well.
    assertTrue(buffer.size() < 100);
    // We purposefully don't check the last byte of length, since that depends on how exactly it
    // compressed.
    assertEquals(Bytes.asList(new byte[] {1, 0, 0, 0}), buffer.subList(0, 4));
    verifyNoMoreInteractions(sink);
  }

  private static void writePayload(MessageFramer2 framer, byte[] bytes) {
    framer.writePayload(new ByteArrayInputStream(bytes), bytes.length);
  }

  /**
   * Since ByteBuffers are reused, this sink copies their value at the time of the call. Converting
   * to List<Byte> is convenience.
   */
  private static class ByteArrayConverterSink implements MessageFramer2.Sink<ByteBuffer> {
    private final MessageFramer2.Sink<List<Byte>> delegate;

    public ByteArrayConverterSink(MessageFramer2.Sink<List<Byte>> delegate) {
      this.delegate = delegate;
    }

    @Override
    public void deliverFrame(ByteBuffer frame, boolean endOfStream) {
      byte[] frameBytes = new byte[frame.remaining()];
      frame.get(frameBytes);
      delegate.deliverFrame(Bytes.asList(frameBytes), endOfStream);
    }
  }
}
