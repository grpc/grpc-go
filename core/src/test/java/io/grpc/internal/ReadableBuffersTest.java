/*
 * Copyright 2018 The gRPC Authors
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

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import java.io.InputStream;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link ReadableBuffers}.
 * See also: {@link ReadableBuffersArrayTest}, {@link ReadableBuffersByteBufferTest}.
 */
@RunWith(JUnit4.class)
public class ReadableBuffersTest {
  private static final byte[] MSG_BYTES = "hello".getBytes(UTF_8);

  @Test
  public void empty_returnsEmptyBuffer() {
    ReadableBuffer buffer = ReadableBuffers.empty();
    assertArrayEquals(new byte[0], buffer.array());
  }

  @Test(expected = NullPointerException.class)
  public void readArray_checksNotNull() {
    ReadableBuffers.readArray(null);
  }

  @Test
  public void readArray_returnsBufferArray() {
    ReadableBuffer buffer = ReadableBuffers.wrap(MSG_BYTES);
    assertArrayEquals(new byte[]{'h', 'e', 'l', 'l', 'o'}, ReadableBuffers.readArray(buffer));
  }

  @Test
  public void readAsString_returnsString() {
    ReadableBuffer buffer = ReadableBuffers.wrap(MSG_BYTES);
    assertEquals("hello", ReadableBuffers.readAsString(buffer, UTF_8));
  }

  @Test(expected = NullPointerException.class)
  public void readAsString_checksNotNull() {
    ReadableBuffers.readAsString(null, UTF_8);
  }

  @Test
  public void readAsStringUtf8_returnsString() {
    ReadableBuffer buffer = ReadableBuffers.wrap(MSG_BYTES);
    assertEquals("hello", ReadableBuffers.readAsStringUtf8(buffer));
  }

  @Test(expected = NullPointerException.class)
  public void readAsStringUtf8_checksNotNull() {
    ReadableBuffers.readAsStringUtf8(null);
  }

  @Test
  public void openStream_ignoresClose() throws Exception {
    ReadableBuffer buffer = mock(ReadableBuffer.class);
    InputStream stream = ReadableBuffers.openStream(buffer, false);
    stream.close();
    verify(buffer, never()).close();
  }

  @Test
  public void bufferInputStream_available_returnsReadableBytes() throws Exception {
    ReadableBuffer buffer = ReadableBuffers.wrap(MSG_BYTES);
    InputStream inputStream = ReadableBuffers.openStream(buffer, true);
    assertEquals(5, inputStream.available());
    while (inputStream.available() != 0) {
      inputStream.read();
    }
    assertEquals(-1, inputStream.read());
  }

  @Test
  public void bufferInputStream_read_returnsUnsignedByte() throws Exception {
    ReadableBuffer buffer = ReadableBuffers.wrap(MSG_BYTES);
    InputStream inputStream = ReadableBuffers.openStream(buffer, true);
    assertEquals((int) 'h', inputStream.read());
  }

  @Test
  public void bufferInputStream_read_writes() throws Exception {
    ReadableBuffer buffer = ReadableBuffers.wrap(MSG_BYTES);
    InputStream inputStream = ReadableBuffers.openStream(buffer, true);
    byte[] dest = new byte[5];
    assertEquals(5, inputStream.read(dest, /*destOffset*/ 0, /*length*/ 5));
    assertArrayEquals(new byte[]{'h', 'e', 'l', 'l', 'o'}, dest);
    assertEquals(-1, inputStream.read(/*dest*/ new byte[1], /*destOffset*/ 0, /*length*/1));
  }

  @Test
  public void bufferInputStream_read_writesPartially() throws Exception {
    ReadableBuffer buffer = ReadableBuffers.wrap(MSG_BYTES);
    InputStream inputStream = ReadableBuffers.openStream(buffer, true);
    byte[] dest = new byte[3];
    assertEquals(2, inputStream.read(dest, /*destOffset*/ 1, /*length*/ 2));
    assertArrayEquals(new byte[]{0x00, 'h', 'e'}, dest);
  }

  @Test
  public void bufferInputStream_close_closesBuffer() throws Exception {
    ReadableBuffer buffer = mock(ReadableBuffer.class);
    InputStream inputStream = ReadableBuffers.openStream(buffer, true);
    inputStream.close();
    verify(buffer, times(1)).close();
  }
}
