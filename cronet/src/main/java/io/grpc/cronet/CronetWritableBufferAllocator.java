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

import io.grpc.internal.WritableBuffer;
import io.grpc.internal.WritableBufferAllocator;
import java.nio.ByteBuffer;

/**
 * The default allocator for {@link CronetWritableBuffer}s used by the Cronet transport.
 */
class CronetWritableBufferAllocator implements WritableBufferAllocator {
  // Set the maximum buffer size to 1MB
  private static final int MAX_BUFFER = 1024 * 1024;

  /**
   * Construct a new instance.
   */
  CronetWritableBufferAllocator() {
  }

  @Override
  public WritableBuffer allocate(int capacityHint) {
    capacityHint = Math.min(MAX_BUFFER, capacityHint);
    return new CronetWritableBuffer(ByteBuffer.allocateDirect(capacityHint), capacityHint);
  }
}
