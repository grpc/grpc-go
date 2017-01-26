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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import java.io.IOException;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Tests for {@link ForwardingReadableBuffer}.
 */
@RunWith(JUnit4.class)
public class ForwardingReadableBufferTest {

  @Mock private ReadableBuffer delegate;
  private ForwardingReadableBuffer buffer;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    buffer = new ForwardingReadableBuffer(delegate) {};
  }

  @Test
  public void readableBytes() {
    when(delegate.readableBytes()).thenReturn(1);

    assertEquals(1, buffer.readableBytes());
  }

  @Test
  public void readUnsignedByte() {
    when(delegate.readUnsignedByte()).thenReturn(1);

    assertEquals(1, buffer.readUnsignedByte());
  }

  @Test
  public void readUnsignedMedium() {
    when(delegate.readUnsignedMedium()).thenReturn(1);

    assertEquals(1, buffer.readUnsignedMedium());
  }

  @Test
  public void readUnsignedShort() {
    when(delegate.readUnsignedShort()).thenReturn(1);

    assertEquals(1, buffer.readUnsignedShort());
  }

  @Test
  public void readInt() {
    when(delegate.readInt()).thenReturn(1);

    assertEquals(1, buffer.readInt());
  }

  @Test
  public void skipBytes() {
    buffer.skipBytes(1);

    verify(delegate).skipBytes(1);
  }

  @Test
  public void readBytes() {
    buffer.readBytes(null, 1, 2);

    verify(delegate).readBytes(null, 1, 2);
  }

  @Test
  public void readBytes_overload1() {
    buffer.readBytes(null);

    verify(delegate).readBytes(null);
  }

  @Test
  public void readBytes_overload2() throws IOException {
    buffer.readBytes(null, 1);

    verify(delegate).readBytes(null, 1);
  }

  @Test
  public void readBytes_overload3() {
    buffer.readBytes(1);

    verify(delegate).readBytes(1);
  }

  @Test
  public void hasArray() {
    when(delegate.hasArray()).thenReturn(true);

    assertEquals(true, buffer.hasArray());
  }

  @Test
  public void array() {
    when(delegate.array()).thenReturn(null);

    assertEquals(null, buffer.array());
  }

  @Test
  public void arrayOffset() {
    when(delegate.arrayOffset()).thenReturn(1);

    assertEquals(1, buffer.arrayOffset());
  }

  @Test
  public void close() {
    buffer.close();

    verify(delegate).close();
  }
}
