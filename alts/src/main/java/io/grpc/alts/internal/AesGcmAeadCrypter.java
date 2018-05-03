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

import static com.google.common.base.Preconditions.checkArgument;

import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import javax.annotation.Nullable;
import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

/** AES128-GCM implementation of {@link AeadCrypter} that uses default JCE provider. */
final class AesGcmAeadCrypter implements AeadCrypter {
  private static final int KEY_LENGTH = 16;
  private static final int TAG_LENGTH = 16;
  static final int NONCE_LENGTH = 12;

  private static final String AES = "AES";
  private static final String AES_GCM = AES + "/GCM/NoPadding";

  private final byte[] key;
  private final Cipher cipher;

  AesGcmAeadCrypter(byte[] key) throws GeneralSecurityException {
    checkArgument(key.length == KEY_LENGTH);
    this.key = key;
    cipher = Cipher.getInstance(AES_GCM);
  }

  private int encryptAad(
      ByteBuffer ciphertext, ByteBuffer plaintext, @Nullable ByteBuffer aad, byte[] nonce)
      throws GeneralSecurityException {
    checkArgument(nonce.length == NONCE_LENGTH);
    cipher.init(
        Cipher.ENCRYPT_MODE,
        new SecretKeySpec(this.key, AES),
        new GCMParameterSpec(TAG_LENGTH * 8, nonce));
    if (aad != null) {
      cipher.updateAAD(aad);
    }
    return cipher.doFinal(plaintext, ciphertext);
  }

  private void decryptAad(
      ByteBuffer plaintext, ByteBuffer ciphertext, @Nullable ByteBuffer aad, byte[] nonce)
      throws GeneralSecurityException {
    checkArgument(nonce.length == NONCE_LENGTH);
    cipher.init(
        Cipher.DECRYPT_MODE,
        new SecretKeySpec(this.key, AES),
        new GCMParameterSpec(TAG_LENGTH * 8, nonce));
    if (aad != null) {
      cipher.updateAAD(aad);
    }
    cipher.doFinal(ciphertext, plaintext);
  }

  @Override
  public void encrypt(ByteBuffer ciphertext, ByteBuffer plaintext, byte[] nonce)
      throws GeneralSecurityException {
    encryptAad(ciphertext, plaintext, null, nonce);
  }

  @Override
  public void encrypt(ByteBuffer ciphertext, ByteBuffer plaintext, ByteBuffer aad, byte[] nonce)
      throws GeneralSecurityException {
    encryptAad(ciphertext, plaintext, aad, nonce);
  }

  @Override
  public void decrypt(ByteBuffer plaintext, ByteBuffer ciphertext, byte[] nonce)
      throws GeneralSecurityException {
    decryptAad(plaintext, ciphertext, null, nonce);
  }

  @Override
  public void decrypt(ByteBuffer plaintext, ByteBuffer ciphertext, ByteBuffer aad, byte[] nonce)
      throws GeneralSecurityException {
    decryptAad(plaintext, ciphertext, aad, nonce);
  }

  static int getKeyLength() {
    return KEY_LENGTH;
  }
}
