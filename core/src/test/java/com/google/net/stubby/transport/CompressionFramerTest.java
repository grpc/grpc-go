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

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import com.google.common.collect.Lists;
import com.google.common.io.ByteStreams;
import com.google.common.primitives.Bytes;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.nio.ByteBuffer;
import java.util.List;
import java.util.Random;
import java.util.zip.Deflater;
import java.util.zip.InflaterInputStream;

/** Unit tests for {@link CompressionFramer}. */
@RunWith(JUnit4.class)
public class CompressionFramerTest {
  private int maxFrameSize = 1024;
  private int sufficient = 8;
  private CapturingSink sink = new CapturingSink();
  private CompressionFramer framer = new CompressionFramer(sink, maxFrameSize, true, sufficient);

  @Test
  public void testGoodCompression() {
    byte[] payload = new byte[1000];
    framer.setCompressionLevel(Deflater.BEST_COMPRESSION);
    framer.write(payload, 0, payload.length);
    framer.endOfMessage();
    framer.flush();

    assertEquals(1, sink.frames.size());
    byte[] frame = sink.frames.get(0);
    assertEquals(TransportFrameUtil.FLATE_FLAG, frame[0]);
    assertTrue(decodeFrameLength(frame) < 30);
    assertArrayEquals(payload, decompress(frame));
  }

  @Test
  public void testPoorCompression() {
    byte[] payload = new byte[3 * maxFrameSize / 2];
    new Random(1).nextBytes(payload);
    framer.setCompressionLevel(Deflater.DEFAULT_COMPRESSION);
    framer.write(payload, 0, payload.length);
    framer.endOfMessage();
    framer.flush();

    assertEquals(2, sink.frames.size());
    assertEquals(TransportFrameUtil.FLATE_FLAG, sink.frames.get(0)[0]);
    assertEquals(TransportFrameUtil.FLATE_FLAG, sink.frames.get(1)[0]);
    assertTrue(decodeFrameLength(sink.frames.get(0)) <= maxFrameSize);
    assertTrue(decodeFrameLength(sink.frames.get(0))
        >= maxFrameSize - CompressionFramer.HEADER_LENGTH - CompressionFramer.MARGIN - sufficient);
    assertArrayEquals(payload, decompress(sink.frames));
  }

  private static int decodeFrameLength(byte[] frame) {
    return ((frame[1] & 0xFF) << 16)
        | ((frame[2] & 0xFF) << 8)
        | (frame[3] & 0xFF);
  }

  private static byte[] decompress(byte[] frame) {
    try {
      return ByteStreams.toByteArray(new InflaterInputStream(new ByteArrayInputStream(frame,
          CompressionFramer.HEADER_LENGTH, frame.length - CompressionFramer.HEADER_LENGTH)));
    } catch (IOException ex) {
      throw new AssertionError();
    }
  }

  private static byte[] decompress(List<byte[]> frames) {
    byte[][] bytes = new byte[frames.size()][];
    for (int i = 0; i < frames.size(); i++) {
      bytes[i] = decompress(frames.get(i));
    }
    return Bytes.concat(bytes);
  }

  private static class CapturingSink implements Framer.Sink<ByteBuffer> {
    public final List<byte[]> frames = Lists.newArrayList();

    @Override
    public void deliverFrame(ByteBuffer frame, boolean endOfMessage) {
      byte[] frameBytes = new byte[frame.remaining()];
      frame.get(frameBytes);
      assertEquals(frameBytes.length - CompressionFramer.HEADER_LENGTH,
          decodeFrameLength(frameBytes));
      frames.add(frameBytes);
    }
  }
}
