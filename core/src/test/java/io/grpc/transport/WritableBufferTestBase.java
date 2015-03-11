/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Abstract base class for tests of {@link WritableBuffer} subclasses.
 */
@RunWith(JUnit4.class)
public abstract class WritableBufferTestBase {

  /**
   * Returns a new buffer for every test case with
   * at least 100 byte of capacity.
   */
  protected abstract WritableBuffer buffer();

  /**
   * Bytes written to {@link #buffer()}.
   */
  protected abstract byte[] writtenBytes();

  @Test(expected = RuntimeException.class)
  public void testWriteNegativeLength() {
    buffer().write(new byte[1], 0, -1);
  }

  @Test(expected = IndexOutOfBoundsException.class)
  public void testWriteNegativeSrcIndex() {
    buffer().write(new byte[1], -1, 0);
  }

  @Test(expected = IndexOutOfBoundsException.class)
  public void testWriteSrcIndexAndLengthExceedSrcLength() {
    buffer().write(new byte[10], 1, 10);
  }

  @Test(expected = IndexOutOfBoundsException.class)
  public void testWriteSrcIndexAndLengthExceedWritableBytes() {
    buffer().write(new byte[buffer().writableBytes()], 1, buffer().writableBytes());
  }

  @Test
  public void testWritableAndReadableBytes() {
    int before = buffer().writableBytes();
    buffer().write(new byte[10], 0, 5);

    assertEquals(5, before - buffer().writableBytes());
    assertEquals(5, buffer().readableBytes());
  }

  @Test
  public void testWriteSrcIndex() {
    byte b[] = new byte[10];
    for (byte i = 5; i < 10; i++) {
      b[i] = i;
    }

    buffer().write(b, 5, 5);

    assertEquals(5, buffer().readableBytes());
    byte writtenBytes[] = writtenBytes();
    assertEquals(5, writtenBytes.length);
    for (int i = 0; i < writtenBytes.length; i++) {
      assertEquals(5+i, writtenBytes[i]);
    }
  }

  @Test
  public void testMultipleWrites() {
    byte[] b = new byte[100];
    for (byte i = 0; i < b.length; i++) {
      b[i] = i;
    }

    // Write in chunks of 10 bytes
    for (int i = 0; i < 10; i++) {
      buffer().write(b, 10 * i, 10);
      assertEquals(10 * (i + 1), buffer().readableBytes());
    }

    assertArrayEquals(b, writtenBytes());
  }
}
