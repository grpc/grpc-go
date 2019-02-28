/*
 * Copyright 2015 The gRPC Authors
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
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.ForwardingTestUtil;
import java.io.IOException;
import java.io.OutputStream;
import java.lang.reflect.Method;
import java.nio.ByteBuffer;
import java.util.Collections;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.junit.MockitoJUnit;
import org.mockito.junit.MockitoRule;

/**
 * Tests for {@link ForwardingReadableBuffer}.
 */
@RunWith(JUnit4.class)
public class ForwardingReadableBufferTest {
  @Rule
  public final MockitoRule mocks = MockitoJUnit.rule();

  @Mock private ReadableBuffer delegate;
  private ForwardingReadableBuffer buffer;

  @Before
  public void setUp() {
    buffer = new ForwardingReadableBuffer(delegate) {};
  }

  @Test
  public void allMethodsForwarded() throws Exception {
    ForwardingTestUtil.testMethodsForwarded(
        ReadableBuffer.class,
        delegate,
        buffer,
        Collections.<Method>emptyList());
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
    byte[] dest = new byte[1];
    buffer.readBytes(dest, 1, 2);

    verify(delegate).readBytes(dest, 1, 2);
  }

  @Test
  public void readBytes_overload1() {
    ByteBuffer dest = mock(ByteBuffer.class);
    buffer.readBytes(dest);

    verify(delegate).readBytes(dest);
  }

  @Test
  public void readBytes_overload2() throws IOException {
    OutputStream dest = mock(OutputStream.class);
    buffer.readBytes(dest, 1);

    verify(delegate).readBytes(dest, 1);
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
    byte[] array = new byte[1];
    when(delegate.array()).thenReturn(array);

    assertEquals(array, buffer.array());
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
