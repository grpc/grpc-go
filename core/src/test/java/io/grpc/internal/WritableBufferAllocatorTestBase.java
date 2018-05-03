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

import static org.junit.Assert.assertNotSame;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Abstract base class for tests of {@link WritableBufferAllocator} subclasses.
 */
@RunWith(JUnit4.class)
public abstract class WritableBufferAllocatorTestBase {

  protected abstract WritableBufferAllocator allocator();

  @Test
  public void testBuffersAreDifferent() {
    WritableBuffer buffer1 = allocator().allocate(100);
    WritableBuffer buffer2 = allocator().allocate(100);

    assertNotSame(buffer1, buffer2);

    buffer1.release();
    buffer2.release();
  }
}
