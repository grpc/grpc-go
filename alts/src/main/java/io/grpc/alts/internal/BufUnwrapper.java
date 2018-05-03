/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts.internal;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.CompositeByteBuf;
import java.nio.ByteBuffer;

/** Unwraps {@link ByteBuf}s into {@link ByteBuffer}s. */
final class BufUnwrapper implements AutoCloseable {

  private final ByteBuffer[] singleReadBuffer = new ByteBuffer[1];
  private final ByteBuffer[] singleWriteBuffer = new ByteBuffer[1];

  /**
   * Called to get access to the underlying NIO buffers for a {@link ByteBuf} that will be used for
   * writing.
   */
  ByteBuffer[] writableNioBuffers(ByteBuf buf) {
    // Set the writer index to the capacity to guarantee that the returned NIO buffers will have
    // the capacity available.
    int readerIndex = buf.readerIndex();
    int writerIndex = buf.writerIndex();
    buf.readerIndex(writerIndex);
    buf.writerIndex(buf.capacity());

    try {
      return nioBuffers(buf, singleWriteBuffer);
    } finally {
      // Restore the writer index before returning.
      buf.readerIndex(readerIndex);
      buf.writerIndex(writerIndex);
    }
  }

  /**
   * Called to get access to the underlying NIO buffers for a {@link ByteBuf} that will be used for
   * reading.
   */
  ByteBuffer[] readableNioBuffers(ByteBuf buf) {
    return nioBuffers(buf, singleReadBuffer);
  }

  @Override
  public void close() {
    singleReadBuffer[0] = null;
    singleWriteBuffer[0] = null;
  }

  /**
   * Optimized accessor for obtaining the underlying NIO buffers for a Netty {@link ByteBuf}. Based
   * on code from Netty's {@code SslHandler}. This method returns NIO buffers that span the readable
   * region of the {@link ByteBuf}.
   */
  private static ByteBuffer[] nioBuffers(ByteBuf buf, ByteBuffer[] singleBuffer) {
    // As CompositeByteBuf.nioBufferCount() can be expensive (as it needs to check all composed
    // ByteBuf to calculate the count) we will just assume a CompositeByteBuf contains more than 1
    // ByteBuf. The worst that can happen is that we allocate an extra ByteBuffer[] in
    // CompositeByteBuf.nioBuffers() which is better than walking the composed ByteBuf in most
    // cases.
    if (!(buf instanceof CompositeByteBuf) && buf.nioBufferCount() == 1) {
      // We know its only backed by 1 ByteBuffer so use internalNioBuffer to keep object
      // allocation to a minimum.
      singleBuffer[0] = buf.internalNioBuffer(buf.readerIndex(), buf.readableBytes());
      return singleBuffer;
    }

    return buf.nioBuffers();
  }
}
