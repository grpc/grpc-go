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

import java.util.Arrays;
import org.junit.Before;

public class ByteWritableBufferTest extends WritableBufferTestBase {

  private MessageFramerTest.ByteWritableBuffer buffer;

  @Before
  public void setup() {
    buffer = new MessageFramerTest.ByteWritableBuffer(100);
  }

  @Override
  protected WritableBuffer buffer() {
    return buffer;
  }

  @Override
  protected byte[] writtenBytes() {
    return Arrays.copyOf(buffer.data, buffer.readableBytes());
  }
}
