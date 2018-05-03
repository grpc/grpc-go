/*
 * Copyright 2014 The gRPC Authors
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
import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.ByteBuffer;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

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
  public void readUnsignedByteShouldSkipZeroLengthBuffer() {
    composite = new CompositeReadableBuffer();
    composite.addBuffer(ReadableBuffers.wrap(new byte[0]));
    byte[] in = {1};
    composite.addBuffer(ReadableBuffers.wrap(in));
    assertEquals(1, composite.readUnsignedByte());
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
