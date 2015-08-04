/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.okhttp;

import io.grpc.internal.WritableBuffer;
import io.grpc.internal.WritableBufferAllocator;
import okio.Buffer;

/**
 * The default allocator for {@link OkHttpWritableBuffer}s used by the OkHttp transport. OkHttp
 * cannot receive buffers larger than the max DATA frame size - 1 so we must set an upper bound on
 * the allocated buffer size here.
 */
class OkHttpWritableBufferAllocator implements WritableBufferAllocator {

  // Use 4k as our minimum buffer size.
  private static final int MIN_BUFFER = 4096;

  // Set the maximum buffer size to 1MB
  private static final int MAX_BUFFER = 1024 * 1024;

  /**
   * Construct a new instance.
   */
  OkHttpWritableBufferAllocator() {
  }

  /**
   * For OkHttp we will often return a buffer smaller than the requested capacity as this is the
   * mechanism for chunking a large GRPC message over many DATA frames.
   */
  @Override
  public WritableBuffer allocate(int capacityHint) {
    capacityHint = Math.min(MAX_BUFFER, Math.max(MIN_BUFFER, capacityHint));
    return new OkHttpWritableBuffer(new Buffer(), capacityHint);
  }
}
