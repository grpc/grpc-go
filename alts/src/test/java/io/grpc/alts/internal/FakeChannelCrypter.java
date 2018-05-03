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

import static com.google.common.base.Preconditions.checkState;

import io.netty.buffer.ByteBuf;
import java.security.GeneralSecurityException;
import java.util.Collections;
import java.util.List;
import javax.crypto.AEADBadTagException;

public final class FakeChannelCrypter implements ChannelCrypterNetty {
  private static final int TAG_BYTES = 16;
  private static final byte TAG_BYTE = (byte) 0xa1;

  private boolean destroyCalled = false;

  public static int getTagBytes() {
    return TAG_BYTES;
  }

  @Override
  public void encrypt(ByteBuf out, List<ByteBuf> plain) throws GeneralSecurityException {
    checkState(!destroyCalled);
    for (ByteBuf buf : plain) {
      out.writeBytes(buf);
      for (int i = 0; i < TAG_BYTES; ++i) {
        out.writeByte(TAG_BYTE);
      }
    }
  }

  @Override
  public void decrypt(ByteBuf out, ByteBuf tag, List<ByteBuf> ciphertext)
      throws GeneralSecurityException {
    checkState(!destroyCalled);
    for (ByteBuf buf : ciphertext) {
      out.writeBytes(buf);
    }
    while (tag.isReadable()) {
      if (tag.readByte() != TAG_BYTE) {
        throw new AEADBadTagException("Tag mismatch!");
      }
    }
  }

  @Override
  public void decrypt(ByteBuf out, ByteBuf ciphertextAndTag) throws GeneralSecurityException {
    checkState(!destroyCalled);
    ByteBuf ciphertext = ciphertextAndTag.readSlice(ciphertextAndTag.readableBytes() - TAG_BYTES);
    decrypt(out, /*tag=*/ ciphertextAndTag, Collections.singletonList(ciphertext));
  }

  @Override
  public int getSuffixLength() {
    return TAG_BYTES;
  }

  @Override
  public void destroy() {
    destroyCalled = true;
  }
}
