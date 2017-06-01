/*
 * Copyright 2015, gRPC Authors All rights reserved.
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
