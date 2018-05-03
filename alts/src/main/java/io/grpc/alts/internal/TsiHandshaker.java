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

import io.netty.buffer.ByteBufAllocator;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;

/**
 * This object protects and unprotects buffers once the handshake is done.
 *
 * <p>A typical usage of this object would be:
 *
 * <pre>{@code
 * ByteBuffer buffer = allocateDirect(ALLOCATE_SIZE);
 * while (true) {
 *   while (true) {
 *     tsiHandshaker.getBytesToSendToPeer(buffer.clear());
 *     if (!buffer.hasRemaining()) break;
 *     yourTransportSendMethod(buffer.flip());
 *     assert(!buffer.hasRemaining());  // Guaranteed by yourTransportReceiveMethod(...)
 *   }
 *   if (!tsiHandshaker.isInProgress()) break;
 *   while (true) {
 *     assert(!buffer.hasRemaining());
 *     yourTransportReceiveMethod(buffer.clear());
 *     if (tsiHandshaker.processBytesFromPeer(buffer.flip())) break;
 *   }
 *   if (!tsiHandshaker.isInProgress()) break;
 *   assert(!buffer.hasRemaining());
 * }
 * yourCheckPeerMethod(tsiHandshaker.extractPeer());
 * TsiFrameProtector tsiFrameProtector = tsiHandshaker.createFrameProtector(MAX_FRAME_SIZE);
 * if (buffer.hasRemaining()) tsiFrameProtector.unprotect(buffer, messageBuffer);
 * }</pre>
 *
 * <p>Implementations of this object must be thread compatible.
 */
public interface TsiHandshaker {
  /**
   * Gets bytes that need to be sent to the peer.
   *
   * @param bytes The buffer to put handshake bytes.
   */
  void getBytesToSendToPeer(ByteBuffer bytes) throws GeneralSecurityException;

  /**
   * Process the bytes received from the peer.
   *
   * @param bytes The buffer containing the handshake bytes from the peer.
   * @return true, if the handshake has all the data it needs to process and false, if the method
   *     must be called again to complete processing.
   */
  boolean processBytesFromPeer(ByteBuffer bytes) throws GeneralSecurityException;

  /**
   * Returns true if and only if the handshake is still in progress
   *
   * @return true, if the handshake is still in progress, false otherwise.
   */
  boolean isInProgress();

  /**
   * Returns the peer extracted from a completed handshake.
   *
   * @return the extracted peer.
   */
  TsiPeer extractPeer() throws GeneralSecurityException;

  /**
   * Returns the peer extracted from a completed handshake.
   *
   * @return the extracted peer.
   */
  public Object extractPeerObject() throws GeneralSecurityException;

  /**
   * Creates a frame protector from a completed handshake. No other methods may be called after the
   * frame protector is created.
   *
   * @param maxFrameSize the requested max frame size, the callee is free to ignore.
   * @param alloc used for allocating ByteBufs.
   * @return a new TsiFrameProtector.
   */
  TsiFrameProtector createFrameProtector(int maxFrameSize, ByteBufAllocator alloc);

  /**
   * Creates a frame protector from a completed handshake. No other methods may be called after the
   * frame protector is created.
   *
   * @param alloc used for allocating ByteBufs.
   * @return a new TsiFrameProtector.
   */
  TsiFrameProtector createFrameProtector(ByteBufAllocator alloc);
}
