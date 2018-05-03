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

package io.grpc.netty;

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;

import io.grpc.internal.ReadableBuffer;
import io.grpc.internal.ReadableBufferTestBase;
import io.netty.buffer.Unpooled;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link NettyReadableBuffer}.
 */
@RunWith(JUnit4.class)
public class NettyReadableBufferTest extends ReadableBufferTestBase {
  private NettyReadableBuffer buffer;

  @Before
  public void setup() {
    buffer = new NettyReadableBuffer(Unpooled.copiedBuffer(msg, UTF_8));
  }

  @Test
  public void closeShouldReleaseBuffer() {
    buffer.close();
    assertEquals(0, buffer.buffer().refCnt());
  }

  @Test
  public void closeMultipleTimesShouldReleaseBufferOnce() {
    buffer.close();
    buffer.close();
    assertEquals(0, buffer.buffer().refCnt());
  }

  @Override
  protected ReadableBuffer buffer() {
    return buffer;
  }
}
