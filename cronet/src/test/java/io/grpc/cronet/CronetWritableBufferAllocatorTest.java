/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.cronet;

import static org.junit.Assert.assertEquals;

import io.grpc.internal.WritableBuffer;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class CronetWritableBufferAllocatorTest {

  @Test
  public void testAllocate() throws Exception {
    CronetWritableBufferAllocator allocator = new CronetWritableBufferAllocator();
    WritableBuffer buffer = allocator.allocate(1000);
    assertEquals(1000, buffer.writableBytes());
  }

  @Test
  public void testAllocateLargeBuffer() throws Exception {
    CronetWritableBufferAllocator allocator = new CronetWritableBufferAllocator();
    // Ask for 1GB
    WritableBuffer buffer = allocator.allocate(1024 * 1024 * 1024);
    // Only get 1MB
    assertEquals(1024 * 1024, buffer.writableBytes());
  }
}
