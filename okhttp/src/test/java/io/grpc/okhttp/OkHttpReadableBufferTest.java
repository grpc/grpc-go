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

package io.grpc.okhttp;

import io.grpc.internal.ReadableBuffer;
import io.grpc.internal.ReadableBufferTestBase;
import okio.Buffer;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link OkHttpReadableBuffer}.
 */
@RunWith(JUnit4.class)
public class OkHttpReadableBufferTest extends ReadableBufferTestBase {

  private OkHttpReadableBuffer buffer;

  /** Initialize buffer. */
  @SuppressWarnings("resource")
  @Before
  public void setup() {
    Buffer empty = new Buffer();
    try {
      buffer = new OkHttpReadableBuffer(empty.writeUtf8(msg));
    } finally {
      empty.close();
    }
  }

  @Override
  @Test
  public void readToByteBufferShouldSucceed() {
    // Not supported.
  }

  @Override
  @Test
  public void partialReadToByteBufferShouldSucceed() {
    // Not supported.
  }

  @Override
  protected ReadableBuffer buffer() {
    return buffer;
  }
}

