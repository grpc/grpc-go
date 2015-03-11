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

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.ByteBuffer;

/**
 * Tests for {@link CompositeReadableBuffer}.
 */
@RunWith(JUnit4.class)
public class CompositeReadableBufferTest {
  private static final String EXPECTED_VALUE = "hello world";

  private CompositeReadableBuffer composite;

  @Before
  public void setup() {
    composite = new CompositeReadableBuffer();
    splitAndAdd(EXPECTED_VALUE);
  }

  @After
  public void teardown() {
    composite.close();
  }

  @Test
  public void singleBufferShouldSucceed() {
    composite = new CompositeReadableBuffer();
    composite.addBuffer(ReadableBuffers.wrap(EXPECTED_VALUE.getBytes(UTF_8)));
    assertEquals(EXPECTED_VALUE.length(), composite.readableBytes());
    assertEquals(EXPECTED_VALUE, ReadableBuffers.readAsStringUtf8(composite));
    assertEquals(0, composite.readableBytes());
  }

  @Test
  public void readUnsignedByteShouldSucceed() {
    for (int ix = 0; ix < EXPECTED_VALUE.length(); ++ix) {
      int c = composite.readUnsignedByte();
      assertEquals(EXPECTED_VALUE.charAt(ix), (char) c);
    }
    assertEquals(0, composite.readableBytes());
  }

  @Test
  public void skipBytesShouldSucceed() {
    int remaining = EXPECTED_VALUE.length();
    composite.skipBytes(1);
    remaining--;
    assertEquals(remaining, composite.readableBytes());

    composite.skipBytes(5);
    remaining -= 5;
    assertEquals(remaining, composite.readableBytes());

    composite.skipBytes(remaining);
    assertEquals(0, composite.readableBytes());
  }

  @Test
  public void readByteArrayShouldSucceed() {
    byte[] bytes = new byte[composite.readableBytes()];
    int writeIndex = 0;

    composite.readBytes(bytes, writeIndex, 1);
    writeIndex++;
    assertEquals(EXPECTED_VALUE.length() - writeIndex, composite.readableBytes());

    composite.readBytes(bytes, writeIndex, 5);
    writeIndex += 5;
    assertEquals(EXPECTED_VALUE.length() - writeIndex, composite.readableBytes());

    int remaining = composite.readableBytes();
    composite.readBytes(bytes, writeIndex, remaining);
    writeIndex += remaining;
    assertEquals(0, composite.readableBytes());
    assertEquals(bytes.length, writeIndex);
    assertEquals(EXPECTED_VALUE, new String(bytes, UTF_8));
  }

  @Test
  public void readByteBufferShouldSucceed() {
    ByteBuffer byteBuffer = ByteBuffer.allocate(EXPECTED_VALUE.length());
    int remaining = EXPECTED_VALUE.length();

    byteBuffer.limit(1);
    composite.readBytes(byteBuffer);
    remaining--;
    assertEquals(remaining, composite.readableBytes());

    byteBuffer.limit(byteBuffer.limit() + 5);
    composite.readBytes(byteBuffer);
    remaining -= 5;
    assertEquals(remaining, composite.readableBytes());

    byteBuffer.limit(byteBuffer.limit() + remaining);
    composite.readBytes(byteBuffer);
    assertEquals(0, composite.readableBytes());
    assertEquals(EXPECTED_VALUE, new String(byteBuffer.array(), UTF_8));
  }

  @Test
  public void readStreamShouldSucceed() throws IOException {
    ByteArrayOutputStream bos = new ByteArrayOutputStream();
    int remaining = EXPECTED_VALUE.length();

    composite.readBytes(bos, 1);
    remaining--;
    assertEquals(remaining, composite.readableBytes());

    composite.readBytes(bos, 5);
    remaining -= 5;
    assertEquals(remaining, composite.readableBytes());

    composite.readBytes(bos, remaining);
    assertEquals(0, composite.readableBytes());
    assertEquals(EXPECTED_VALUE, new String(bos.toByteArray(), UTF_8));
  }

  @Test
  public void closeShouldCloseBuffers() {
    composite = new CompositeReadableBuffer();
    ReadableBuffer mock1 = mock(ReadableBuffer.class);
    ReadableBuffer mock2 = mock(ReadableBuffer.class);
    composite.addBuffer(mock1);
    composite.addBuffer(mock2);

    composite.close();
    verify(mock1).close();
    verify(mock2).close();
  }

  private void splitAndAdd(String value) {
    int partLength = Math.max(1, value.length() / 4);
    for (int startIndex = 0, endIndex = 0; startIndex < value.length(); startIndex = endIndex) {
      endIndex = Math.min(value.length(), startIndex + partLength);
      String part = value.substring(startIndex, endIndex);
      composite.addBuffer(ReadableBuffers.wrap(part.getBytes(UTF_8)));
    }

    assertEquals(value.length(), composite.readableBytes());
  }
}
