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

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;

import java.io.ByteArrayOutputStream;
import java.nio.ByteBuffer;
import java.util.Arrays;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Abstract base class for tests of {@link ReadableBuffer} subclasses.
 */
@RunWith(JUnit4.class)
public abstract class ReadableBufferTestBase {
  // Use a long string to ensure that any chunking/splitting works correctly.
  protected static final String msg = repeatUntilLength("hello", 8 * 1024);

  @Test
  public void bufferShouldReadAllBytes() {
    ReadableBuffer buffer = buffer();
    for (int ix = 0; ix < msg.length(); ++ix) {
      assertEquals(msg.length() - ix, buffer.readableBytes());
      assertEquals(msg.charAt(ix), buffer.readUnsignedByte());
    }
    assertEquals(0, buffer.readableBytes());
  }

  @Test
  public void readToArrayShouldSucceed() {
    ReadableBuffer buffer = buffer();
    byte[] array = new byte[msg.length()];
    buffer.readBytes(array, 0, array.length);
    assertArrayEquals(msg.getBytes(UTF_8), array);
    assertEquals(0, buffer.readableBytes());
  }

  @Test
  public void partialReadToArrayShouldSucceed() {
    ReadableBuffer buffer = buffer();
    byte[] array = new byte[msg.length()];
    buffer.readBytes(array, 1, 2);
    assertArrayEquals(new byte[] {'h', 'e'}, Arrays.copyOfRange(array, 1, 3));
    assertEquals(msg.length() - 2, buffer.readableBytes());
  }

  @Test
  public void readToStreamShouldSucceed() throws Exception {
    ReadableBuffer buffer = buffer();
    ByteArrayOutputStream stream = new ByteArrayOutputStream();
    buffer.readBytes(stream, msg.length());
    assertArrayEquals(msg.getBytes(UTF_8), stream.toByteArray());
    assertEquals(0, buffer.readableBytes());
  }

  @Test
  public void partialReadToStreamShouldSucceed() throws Exception {
    ReadableBuffer buffer = buffer();
    ByteArrayOutputStream stream = new ByteArrayOutputStream();
    buffer.readBytes(stream, 2);
    assertArrayEquals(new byte[]{'h', 'e'}, Arrays.copyOfRange(stream.toByteArray(), 0, 2));
    assertEquals(msg.length() - 2, buffer.readableBytes());
  }

  @Test
  public void readToByteBufferShouldSucceed() {
    ReadableBuffer buffer = buffer();
    ByteBuffer byteBuffer = ByteBuffer.allocate(msg.length());
    buffer.readBytes(byteBuffer);
    byteBuffer.flip();
    byte[] array = new byte[msg.length()];
    byteBuffer.get(array);
    assertArrayEquals(msg.getBytes(UTF_8), array);
    assertEquals(0, buffer.readableBytes());
  }

  @Test
  public void partialReadToByteBufferShouldSucceed() {
    ReadableBuffer buffer = buffer();
    ByteBuffer byteBuffer = ByteBuffer.allocate(2);
    buffer.readBytes(byteBuffer);
    byteBuffer.flip();
    byte[] array = new byte[2];
    byteBuffer.get(array);
    assertArrayEquals(new byte[]{'h', 'e'}, array);
    assertEquals(msg.length() - 2, buffer.readableBytes());
  }

  @Test
  public void partialReadToReadableBufferShouldSucceed() {
    ReadableBuffer buffer = buffer();
    ReadableBuffer newBuffer = buffer.readBytes(2);
    assertEquals(2, newBuffer.readableBytes());
    assertEquals(msg.length() - 2, buffer.readableBytes());
    byte[] array = new byte[2];
    newBuffer.readBytes(array, 0, 2);
    assertArrayEquals(new byte[] {'h', 'e'}, Arrays.copyOfRange(array, 0, 2));     
  }

  protected abstract ReadableBuffer buffer();

  private static String repeatUntilLength(String toRepeat, int length) {
    StringBuilder buf = new StringBuilder();
    while (buf.length() < length) {
      buf.append(toRepeat);
    }
    buf.setLength(length);
    return buf.toString();
  }
}
