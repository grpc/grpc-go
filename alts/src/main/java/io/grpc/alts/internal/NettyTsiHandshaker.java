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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;

/**
 * A wrapper for a {@link io.grpc.alts.internal.TsiHandshaker} that accepts netty {@link ByteBuf}s.
 */
public final class NettyTsiHandshaker {

  private BufUnwrapper unwrapper = new BufUnwrapper();
  private final TsiHandshaker internalHandshaker;

  public NettyTsiHandshaker(TsiHandshaker handshaker) {
    internalHandshaker = checkNotNull(handshaker);
  }

  /**
   * Gets data that is ready to be sent to the to the remote peer. This should be called in a loop
   * until no bytes are written to the output buffer.
   *
   * @param out the buffer to receive the bytes.
   */
  void getBytesToSendToPeer(ByteBuf out) throws GeneralSecurityException {
    checkState(unwrapper != null, "protector already created");
    try (BufUnwrapper unwrapper = this.unwrapper) {
      // Write as many bytes as possible into the buffer.
      int bytesWritten = 0;
      for (ByteBuffer nioBuffer : unwrapper.writableNioBuffers(out)) {
        if (!nioBuffer.hasRemaining()) {
          // This buffer doesn't have any more space to write, go to the next buffer.
          continue;
        }

        int prevPos = nioBuffer.position();
        internalHandshaker.getBytesToSendToPeer(nioBuffer);
        bytesWritten += nioBuffer.position() - prevPos;

        // If the buffer position was not changed, the frame has been completely read into the
        // buffers.
        if (nioBuffer.position() == prevPos) {
          break;
        }
      }

      out.writerIndex(out.writerIndex() + bytesWritten);
    }
  }

  /**
   * Process handshake data received from the remote peer.
   *
   * @return {@code true}, if the handshake has all the data it needs to process and {@code false},
   *     if the method must be called again to complete processing.
   */
  boolean processBytesFromPeer(ByteBuf data) throws GeneralSecurityException {
    checkState(unwrapper != null, "protector already created");
    try (BufUnwrapper unwrapper = this.unwrapper) {
      int bytesRead = 0;
      boolean done = false;
      for (ByteBuffer nioBuffer : unwrapper.readableNioBuffers(data)) {
        if (!nioBuffer.hasRemaining()) {
          // This buffer has been fully read, continue to the next buffer.
          continue;
        }

        int prevPos = nioBuffer.position();
        done = internalHandshaker.processBytesFromPeer(nioBuffer);
        bytesRead += nioBuffer.position() - prevPos;
        if (done) {
          break;
        }
      }

      data.readerIndex(data.readerIndex() + bytesRead);
      return done;
    }
  }

  /**
   * Returns true if and only if the handshake is still in progress
   *
   * @return true, if the handshake is still in progress, false otherwise.
   */
  boolean isInProgress() {
    return internalHandshaker.isInProgress();
  }

  /**
   * Returns the peer extracted from a completed handshake.
   *
   * @return the extracted peer.
   */
  TsiPeer extractPeer() throws GeneralSecurityException {
    checkState(!internalHandshaker.isInProgress());
    return internalHandshaker.extractPeer();
  }

  /**
   * Returns the peer extracted from a completed handshake.
   *
   * @return the extracted peer.
   */
  Object extractPeerObject() throws GeneralSecurityException {
    checkState(!internalHandshaker.isInProgress());
    return internalHandshaker.extractPeerObject();
  }

  /**
   * Creates a frame protector from a completed handshake. No other methods may be called after the
   * frame protector is created.
   *
   * @param maxFrameSize the requested max frame size, the callee is free to ignore.
   * @return a new {@link io.grpc.alts.internal.TsiFrameProtector}.
   */
  TsiFrameProtector createFrameProtector(int maxFrameSize, ByteBufAllocator alloc) {
    unwrapper = null;
    return internalHandshaker.createFrameProtector(maxFrameSize, alloc);
  }

  /**
   * Creates a frame protector from a completed handshake. No other methods may be called after the
   * frame protector is created.
   *
   * @return a new {@link io.grpc.alts.internal.TsiFrameProtector}.
   */
  TsiFrameProtector createFrameProtector(ByteBufAllocator alloc) {
    unwrapper = null;
    return internalHandshaker.createFrameProtector(alloc);
  }
}
