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
import io.netty.buffer.ByteBufAllocator;
import java.security.GeneralSecurityException;
import java.util.List;

/**
 * This object protects and unprotects netty buffers once the handshake is done.
 *
 * <p>Implementations of this object must be thread compatible.
 */
public interface TsiFrameProtector {

  /**
   * Protects the buffers by performing framing and encrypting/appending MACs.
   *
   * @param unprotectedBufs contain the payload that will be protected
   * @param ctxWrite is called with buffers containing protected frames and must release the given
   *     buffers
   * @param alloc is used to allocate new buffers for the protected frames
   */
  void protectFlush(
      List<ByteBuf> unprotectedBufs, Consumer<ByteBuf> ctxWrite, ByteBufAllocator alloc)
      throws GeneralSecurityException;

  /**
   * Unprotects the buffers by removing the framing and decrypting/checking MACs.
   *
   * @param in contains (partial) protected frames
   * @param out is only used to append unprotected payload buffers
   * @param alloc is used to allocate new buffers for the unprotected frames
   */
  void unprotect(ByteBuf in, List<Object> out, ByteBufAllocator alloc)
      throws GeneralSecurityException;

  /** Must be called to release all associated resources (instance cannot be used afterwards). */
  void destroy();

  /** A mirror of java.util.function.Consumer without the Java 8 dependency. */
  interface Consumer<T> {
    void accept(T t);
  }
}
