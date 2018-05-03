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

package io.grpc.netty;

import io.grpc.internal.WritableBuffer;
import io.grpc.internal.WritableBufferTestBase;
import io.netty.buffer.Unpooled;
import java.util.Arrays;
import org.junit.After;
import org.junit.Before;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link NettyWritableBuffer}.
 */
@RunWith(JUnit4.class)
public class NettyWritableBufferTest extends WritableBufferTestBase {

  private NettyWritableBuffer buffer;

  @Before
  public void setup() {
    buffer = new NettyWritableBuffer(Unpooled.buffer(100));
  }

  @After
  public void teardown() {
    buffer.release();
  }

  @Override
  protected WritableBuffer buffer() {
    return buffer;
  }

  @Override
  protected byte[] writtenBytes() {
    byte[] b = buffer.bytebuf().array();
    int fromIdx = buffer.bytebuf().arrayOffset();
    return Arrays.copyOfRange(b, fromIdx, buffer.readableBytes());
  }
}
