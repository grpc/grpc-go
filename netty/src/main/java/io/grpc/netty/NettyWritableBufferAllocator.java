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

package io.grpc.netty;

import io.grpc.internal.WritableBuffer;
import io.grpc.internal.WritableBufferAllocator;
import io.netty.buffer.ByteBufAllocator;

/**
 * The default allocator for {@link NettyWritableBuffer}s used by the Netty transport. We set a
 * minimum bound to avoid unnecessary re-allocation for small follow-on writes and to facilitate
 * Netty's caching of buffer objects for small writes. We set an upper-bound to avoid allocations
 * outside of the arena-pool which are orders of magnitude slower. The Netty transport can receive
 * buffers of arbitrary size and will chunk them based on flow-control so there is no transport
 * requirement for an upper bound.
 *
 * <p>Note: It is assumed that most applications will be using Netty's direct buffer pools for
 * maximum performance.
 */
class NettyWritableBufferAllocator implements WritableBufferAllocator {

  // Use 4k as our minimum buffer size.
  private static final int MIN_BUFFER = 4096;

  // Set the maximum buffer size to 1MB
  private static final int MAX_BUFFER = 1024 * 1024;

  private final ByteBufAllocator allocator;

  NettyWritableBufferAllocator(ByteBufAllocator allocator) {
    this.allocator = allocator;
  }

  @Override
  public WritableBuffer allocate(int capacityHint) {
    capacityHint = Math.min(MAX_BUFFER, Math.max(MIN_BUFFER, capacityHint));
    return new NettyWritableBuffer(allocator.buffer(capacityHint, capacityHint));
  }
}
