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
import java.security.GeneralSecurityException;
import java.util.List;

/**
 * A @{code ChannelCrypterNetty} performs stateful encryption and decryption of independent input
 * and output streams. Both decrypt and encrypt gather their input from a list of Netty @{link
 * ByteBuf} instances.
 *
 * <p>Note that we provide implementations of this interface that provide integrity only and
 * implementations that provide privacy and integrity. All methods should be thread-compatible.
 */
public interface ChannelCrypterNetty {

  /**
   * Encrypt plaintext into output buffer.
   *
   * @param out the protected input will be written into this buffer. The buffer must be direct and
   *     have enough space to hold all input buffers and the tag. Encrypt does not take ownership of
   *     this buffer.
   * @param plain the input buffers that should be protected. Encrypt does not modify or take
   *     ownership of these buffers.
   */
  void encrypt(ByteBuf out, List<ByteBuf> plain) throws GeneralSecurityException;

  /**
   * Decrypt ciphertext into the given output buffer and check tag.
   *
   * @param out the unprotected input will be written into this buffer. The buffer must be direct
   *     and have enough space to hold all ciphertext buffers and the tag, i.e., it must have
   *     additional space for the tag, even though this space will be unused in the final result.
   *     Decrypt does not take ownership of this buffer.
   * @param tag the tag appended to the ciphertext. Decrypt does not modify or take ownership of
   *     this buffer.
   * @param ciphertext the buffers that should be unprotected (excluding the tag). Decrypt does not
   *     modify or take ownership of these buffers.
   */
  void decrypt(ByteBuf out, ByteBuf tag, List<ByteBuf> ciphertext) throws GeneralSecurityException;

  /**
   * Decrypt ciphertext into the given output buffer and check tag.
   *
   * @param out the unprotected input will be written into this buffer. The buffer must be direct
   *     and have enough space to hold all ciphertext buffers and the tag, i.e., it must have
   *     additional space for the tag, even though this space will be unused in the final result.
   *     Decrypt does not take ownership of this buffer.
   * @param ciphertextAndTag single buffer containing ciphertext and tag that should be unprotected.
   *     The buffer must be direct and either completely overlap with {@code out} or not overlap at
   *     all.
   */
  void decrypt(ByteBuf out, ByteBuf ciphertextAndTag) throws GeneralSecurityException;

  /** Returns the length of the tag in bytes. */
  int getSuffixLength();

  /** Must be called to release all associated resources (instance cannot be used afterwards). */
  void destroy();
}
