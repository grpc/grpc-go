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

import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;

/**
 * {@code AeadCrypter} performs authenticated encryption and decryption for a fixed key given unique
 * nonces. Authenticated additional data is supported.
 */
interface AeadCrypter {
  /**
   * Encrypt plaintext into ciphertext buffer using the given nonce.
   *
   * @param ciphertext the encrypted plaintext and the tag will be written into this buffer.
   * @param plaintext the input that should be encrypted.
   * @param nonce the unique nonce used for the encryption.
   * @throws GeneralSecurityException if ciphertext buffer is short or the nonce does not have the
   *     expected size.
   */
  void encrypt(ByteBuffer ciphertext, ByteBuffer plaintext, byte[] nonce)
      throws GeneralSecurityException;

  /**
   * Encrypt plaintext into ciphertext buffer using the given nonce with authenticated data.
   *
   * @param ciphertext the encrypted plaintext and the tag will be written into this buffer.
   * @param plaintext the input that should be encrypted.
   * @param aad additional data that should be authenticated, but not encrypted.
   * @param nonce the unique nonce used for the encryption.
   * @throws GeneralSecurityException if ciphertext buffer is short or the nonce does not have the
   *     expected size.
   */
  void encrypt(ByteBuffer ciphertext, ByteBuffer plaintext, ByteBuffer aad, byte[] nonce)
      throws GeneralSecurityException;

  /**
   * Decrypt ciphertext into plaintext buffer using the given nonce.
   *
   * @param plaintext the decrypted plaintext will be written into this buffer.
   * @param ciphertext the ciphertext and tag that should be decrypted.
   * @param nonce the nonce that was used for the encryption.
   * @throws GeneralSecurityException if the tag is invalid or any of the inputs do not have the
   *     expected size.
   */
  void decrypt(ByteBuffer plaintext, ByteBuffer ciphertext, byte[] nonce)
      throws GeneralSecurityException;

  /**
   * Decrypt ciphertext into plaintext buffer using the given nonce.
   *
   * @param plaintext the decrypted plaintext will be written into this buffer.
   * @param ciphertext the ciphertext and tag that should be decrypted.
   * @param aad additional data that is checked for authenticity.
   * @param nonce the nonce that was used for the encryption.
   * @throws GeneralSecurityException if the tag is invalid or any of the inputs do not have the
   *     expected size.
   */
  void decrypt(ByteBuffer plaintext, ByteBuffer ciphertext, ByteBuffer aad, byte[] nonce)
      throws GeneralSecurityException;
}
