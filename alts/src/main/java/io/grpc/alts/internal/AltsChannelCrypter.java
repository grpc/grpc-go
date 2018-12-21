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
import static com.google.common.base.Verify.verify;

import com.google.common.annotations.VisibleForTesting;
import io.netty.buffer.ByteBuf;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.List;

/** Performs encryption and decryption with AES-GCM using JCE. All methods are thread-compatible. */
final class AltsChannelCrypter implements ChannelCrypterNetty {
  private static final int KEY_LENGTH = AesGcmHkdfAeadCrypter.getKeyLength();
  private static final int COUNTER_LENGTH = 12;
  // The counter will overflow after 2^64 operations and encryption/decryption will stop working.
  private static final int COUNTER_OVERFLOW_LENGTH = 8;
  private static final int TAG_LENGTH = 16;

  private final AeadCrypter aeadCrypter;

  private final byte[] outCounter = new byte[COUNTER_LENGTH];
  private final byte[] inCounter = new byte[COUNTER_LENGTH];
  private final byte[] oldCounter = new byte[COUNTER_LENGTH];

  AltsChannelCrypter(byte[] key, boolean isClient) {
    checkArgument(key.length == KEY_LENGTH);
    byte[] counter = isClient ? inCounter : outCounter;
    counter[counter.length - 1] = (byte) 0x80;
    this.aeadCrypter = new AesGcmHkdfAeadCrypter(key);
  }

  static int getKeyLength() {
    return KEY_LENGTH;
  }

  static int getCounterLength() {
    return COUNTER_LENGTH;
  }

  @Override
  public void encrypt(ByteBuf outBuf, List<ByteBuf> plainBufs) throws GeneralSecurityException {
    checkArgument(outBuf.nioBufferCount() == 1);
    // Copy plaintext buffers into outBuf for in-place encryption on single direct buffer.
    ByteBuf plainBuf = outBuf.slice(outBuf.writerIndex(), outBuf.writableBytes());
    plainBuf.writerIndex(0);
    for (ByteBuf inBuf : plainBufs) {
      plainBuf.writeBytes(inBuf);
    }

    verify(outBuf.writableBytes() == plainBuf.readableBytes() + TAG_LENGTH);
    ByteBuffer out = outBuf.internalNioBuffer(outBuf.writerIndex(), outBuf.writableBytes());
    ByteBuffer plain = out.duplicate();
    plain.limit(out.limit() - TAG_LENGTH);

    byte[] counter = incrementOutCounter();
    int outPosition = out.position();
    aeadCrypter.encrypt(out, plain, counter);
    int bytesWritten = out.position() - outPosition;
    outBuf.writerIndex(outBuf.writerIndex() + bytesWritten);
    verify(!outBuf.isWritable());
  }

  @Override
  public void decrypt(ByteBuf out, ByteBuf tag, List<ByteBuf> ciphertextBufs)
      throws GeneralSecurityException {

    ByteBuf cipherTextAndTag = out.slice(out.writerIndex(), out.writableBytes());
    cipherTextAndTag.writerIndex(0);

    for (ByteBuf inBuf : ciphertextBufs) {
      cipherTextAndTag.writeBytes(inBuf);
    }
    cipherTextAndTag.writeBytes(tag);

    decrypt(out, cipherTextAndTag);
  }

  @Override
  public void decrypt(ByteBuf out, ByteBuf ciphertextAndTag) throws GeneralSecurityException {
    int bytesRead = ciphertextAndTag.readableBytes();
    checkArgument(bytesRead == out.writableBytes());

    checkArgument(out.nioBufferCount() == 1);
    ByteBuffer outBuffer = out.internalNioBuffer(out.writerIndex(), out.writableBytes());

    checkArgument(ciphertextAndTag.nioBufferCount() == 1);
    ByteBuffer ciphertextAndTagBuffer =
        ciphertextAndTag.nioBuffer(ciphertextAndTag.readerIndex(), bytesRead);

    byte[] counter = incrementInCounter();
    int outPosition = outBuffer.position();
    aeadCrypter.decrypt(outBuffer, ciphertextAndTagBuffer, counter);
    int bytesWritten = outBuffer.position() - outPosition;
    out.writerIndex(out.writerIndex() + bytesWritten);
    ciphertextAndTag.readerIndex(out.readerIndex() + bytesRead);
    verify(out.writableBytes() == TAG_LENGTH);
  }

  @Override
  public int getSuffixLength() {
    return TAG_LENGTH;
  }

  @Override
  public void destroy() {
    // no destroy required
  }

  /** Increments {@code counter}, store the unincremented value in {@code oldCounter}. */
  static void incrementCounter(byte[] counter, byte[] oldCounter) throws GeneralSecurityException {
    System.arraycopy(counter, 0, oldCounter, 0, counter.length);
    int i = 0;
    for (; i < COUNTER_OVERFLOW_LENGTH; i++) {
      counter[i]++;
      if (counter[i] != (byte) 0x00) {
        break;
      }
    }

    if (i == COUNTER_OVERFLOW_LENGTH) {
      // Restore old counter value to ensure that encrypt and decrypt keep failing.
      System.arraycopy(oldCounter, 0, counter, 0, counter.length);
      throw new GeneralSecurityException("Counter has overflowed.");
    }
  }

  /** Increments the input counter, returning the previous (unincremented) value. */
  private byte[] incrementInCounter() throws GeneralSecurityException {
    incrementCounter(inCounter, oldCounter);
    return oldCounter;
  }

  /** Increments the output counter, returning the previous (unincremented) value. */
  private byte[] incrementOutCounter() throws GeneralSecurityException {
    incrementCounter(outCounter, oldCounter);
    return oldCounter;
  }

  @VisibleForTesting
  void incrementInCounterForTesting(int n) throws GeneralSecurityException {
    for (int i = 0; i < n; i++) {
      incrementInCounter();
    }
  }

  @VisibleForTesting
  void incrementOutCounterForTesting(int n) throws GeneralSecurityException {
    for (int i = 0; i < n; i++) {
      incrementOutCounter();
    }
  }
}
