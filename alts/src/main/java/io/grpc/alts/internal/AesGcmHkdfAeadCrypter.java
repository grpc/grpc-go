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
import java.util.Arrays;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

/**
 * {@link AeadCrypter} implementation based on {@link AesGcmAeadCrypter} with nonce-based rekeying
 * using HKDF-expand and random nonce-mask that is XORed with the given nonce/counter. The AES-GCM
 * key is computed as HKDF-expand(kdfKey, nonce[2..7]), i.e., the first 2 bytes are ignored to
 * require rekeying only after 2^16 operations and the last 4 bytes (including the direction bit)
 * are ignored to allow for optimizations (use same AEAD context for both directions, store counter
 * as unsigned long and boolean for direction).
 */
final class AesGcmHkdfAeadCrypter implements AeadCrypter {
  private static final int KDF_KEY_LENGTH = 32;
  // Rekey after 2^(2*8) = 2^16 operations by ignoring the first 2 nonce bytes for key derivation.
  private static final int KDF_COUNTER_OFFSET = 2;
  // Use remaining bytes of 64-bit counter included in nonce for key derivation.
  private static final int KDF_COUNTER_LENGTH = 6;
  private static final int NONCE_LENGTH = AesGcmAeadCrypter.NONCE_LENGTH;
  private static final int KEY_LENGTH = KDF_KEY_LENGTH + NONCE_LENGTH;

  private final byte[] kdfKey;
  private final byte[] kdfCounter = new byte[KDF_COUNTER_LENGTH];
  private final byte[] nonceMask;
  private final byte[] nonceBuffer = new byte[NONCE_LENGTH];

  private AeadCrypter aeadCrypter;

  AesGcmHkdfAeadCrypter(byte[] key) {
    checkArgument(key.length == KEY_LENGTH);
    this.kdfKey = Arrays.copyOf(key, KDF_KEY_LENGTH);
    this.nonceMask = Arrays.copyOfRange(key, KDF_KEY_LENGTH, KDF_KEY_LENGTH + NONCE_LENGTH);
  }

  @Override
  public void encrypt(ByteBuffer ciphertext, ByteBuffer plaintext, byte[] nonce)
      throws GeneralSecurityException {
    maybeRekey(nonce);
    maskNonce(nonceBuffer, nonceMask, nonce);
    aeadCrypter.encrypt(ciphertext, plaintext, nonceBuffer);
  }

  @Override
  public void encrypt(ByteBuffer ciphertext, ByteBuffer plaintext, ByteBuffer aad, byte[] nonce)
      throws GeneralSecurityException {
    maybeRekey(nonce);
    maskNonce(nonceBuffer, nonceMask, nonce);
    aeadCrypter.encrypt(ciphertext, plaintext, aad, nonceBuffer);
  }

  @Override
  public void decrypt(ByteBuffer plaintext, ByteBuffer ciphertext, byte[] nonce)
      throws GeneralSecurityException {
    maybeRekey(nonce);
    maskNonce(nonceBuffer, nonceMask, nonce);
    aeadCrypter.decrypt(plaintext, ciphertext, nonceBuffer);
  }

  @Override
  public void decrypt(ByteBuffer plaintext, ByteBuffer ciphertext, ByteBuffer aad, byte[] nonce)
      throws GeneralSecurityException {
    maybeRekey(nonce);
    maskNonce(nonceBuffer, nonceMask, nonce);
    aeadCrypter.decrypt(plaintext, ciphertext, aad, nonceBuffer);
  }

  private void maybeRekey(byte[] nonce) throws GeneralSecurityException {
    if (aeadCrypter != null
        && arrayEqualOn(nonce, KDF_COUNTER_OFFSET, kdfCounter, 0, KDF_COUNTER_LENGTH)) {
      return;
    }
    System.arraycopy(nonce, KDF_COUNTER_OFFSET, kdfCounter, 0, KDF_COUNTER_LENGTH);
    int aeKeyLen = AesGcmAeadCrypter.getKeyLength();
    byte[] aeKey = Arrays.copyOf(hkdfExpandSha256(kdfKey, kdfCounter), aeKeyLen);
    aeadCrypter = new AesGcmAeadCrypter(aeKey);
  }

  private static void maskNonce(byte[] nonceBuffer, byte[] nonceMask, byte[] nonce) {
    checkArgument(nonce.length == NONCE_LENGTH);
    for (int i = 0; i < NONCE_LENGTH; i++) {
      nonceBuffer[i] = (byte) (nonceMask[i] ^ nonce[i]);
    }
  }

  private static byte[] hkdfExpandSha256(byte[] key, byte[] info) throws GeneralSecurityException {
    Mac mac = Mac.getInstance("HMACSHA256");
    mac.init(new SecretKeySpec(key, mac.getAlgorithm()));
    mac.update(info);
    mac.update((byte) 0x01);
    return mac.doFinal();
  }

  private static boolean arrayEqualOn(byte[] a, int aPos, byte[] b, int bPos, int length) {
    for (int i = 0; i < length; i++) {
      if (a[aPos + i] != b[bPos + i]) {
        return false;
      }
    }
    return true;
  }

  static int getKeyLength() {
    return KEY_LENGTH;
  }
}
