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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.alts.internal.ByteBufTestUtils.getDirectBuffer;
import static io.grpc.alts.internal.ByteBufTestUtils.getRandom;
import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.fail;

import io.grpc.alts.internal.ByteBufTestUtils.RegisterRef;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.util.ReferenceCounted;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import javax.crypto.AEADBadTagException;
import org.junit.Test;

/** Abstract class for unit tests of {@link ChannelCrypterNetty}. */
public abstract class ChannelCrypterNettyTestBase {
  private static final String DECRYPTION_FAILURE_MESSAGE = "Tag mismatch";

  protected final List<ReferenceCounted> references = new ArrayList<>();
  public ChannelCrypterNetty client;
  public ChannelCrypterNetty server;
  private final RegisterRef ref =
      new RegisterRef() {
        @Override
        public ByteBuf register(ByteBuf buf) {
          if (buf != null) {
            references.add(buf);
          }
          return buf;
        }
      };

  static final class FrameEncrypt {
    List<ByteBuf> plain;
    ByteBuf out;
  }

  static final class FrameDecrypt {
    List<ByteBuf> ciphertext;
    ByteBuf out;
    ByteBuf tag;
  }

  FrameEncrypt createFrameEncrypt(String message) {
    byte[] messageBytes = message.getBytes(UTF_8);
    FrameEncrypt frame = new FrameEncrypt();
    ByteBuf plain = getDirectBuffer(messageBytes.length, ref);
    plain.writeBytes(messageBytes);
    frame.plain = Collections.singletonList(plain);
    frame.out = getDirectBuffer(messageBytes.length + client.getSuffixLength(), ref);
    return frame;
  }

  FrameDecrypt frameDecryptOfEncrypt(FrameEncrypt frameEncrypt) {
    int tagLen = client.getSuffixLength();
    FrameDecrypt frameDecrypt = new FrameDecrypt();
    ByteBuf out = frameEncrypt.out;
    frameDecrypt.ciphertext =
        Collections.singletonList(out.slice(out.readerIndex(), out.readableBytes() - tagLen));
    frameDecrypt.tag = out.slice(out.readerIndex() + out.readableBytes() - tagLen, tagLen);
    frameDecrypt.out = getDirectBuffer(out.readableBytes(), ref);
    return frameDecrypt;
  }

  @Test
  public void encryptDecrypt() throws GeneralSecurityException {
    String message = "Hello world";
    FrameEncrypt frameEncrypt = createFrameEncrypt(message);
    client.encrypt(frameEncrypt.out, frameEncrypt.plain);
    FrameDecrypt frameDecrypt = frameDecryptOfEncrypt(frameEncrypt);

    server.decrypt(frameDecrypt.out, frameDecrypt.tag, frameDecrypt.ciphertext);
    assertThat(frameEncrypt.plain.get(0).slice(0, frameDecrypt.out.readableBytes()))
        .isEqualTo(frameDecrypt.out);
  }

  @Test
  public void encryptDecryptLarge() throws GeneralSecurityException {
    FrameEncrypt frameEncrypt = new FrameEncrypt();
    ByteBuf plain = getRandom(17 * 1024, ref);
    frameEncrypt.plain = Collections.singletonList(plain);
    frameEncrypt.out = getDirectBuffer(plain.readableBytes() + client.getSuffixLength(), ref);

    client.encrypt(frameEncrypt.out, frameEncrypt.plain);
    FrameDecrypt frameDecrypt = frameDecryptOfEncrypt(frameEncrypt);

    // Call decrypt overload that takes ciphertext and tag.
    server.decrypt(frameDecrypt.out, frameEncrypt.out);
    assertThat(frameEncrypt.plain.get(0).slice(0, frameDecrypt.out.readableBytes()))
        .isEqualTo(frameDecrypt.out);
  }

  @Test
  public void encryptDecryptMultiple() throws GeneralSecurityException {
    String message = "Hello world";
    for (int i = 0; i < 512; ++i) {
      FrameEncrypt frameEncrypt = createFrameEncrypt(message);
      client.encrypt(frameEncrypt.out, frameEncrypt.plain);
      FrameDecrypt frameDecrypt = frameDecryptOfEncrypt(frameEncrypt);

      server.decrypt(frameDecrypt.out, frameDecrypt.tag, frameDecrypt.ciphertext);
      assertThat(frameEncrypt.plain.get(0).slice(0, frameDecrypt.out.readableBytes()))
          .isEqualTo(frameDecrypt.out);
    }
  }

  @Test
  public void encryptDecryptComposite() throws GeneralSecurityException {
    String message = "Hello world";
    int lastLen = 2;
    byte[] messageBytes = message.getBytes(UTF_8);
    FrameEncrypt frameEncrypt = new FrameEncrypt();
    ByteBuf plain1 = getDirectBuffer(messageBytes.length - lastLen, ref);
    ByteBuf plain2 = getDirectBuffer(lastLen, ref);
    plain1.writeBytes(messageBytes, 0, messageBytes.length - lastLen);
    plain2.writeBytes(messageBytes, messageBytes.length - lastLen, lastLen);
    ByteBuf plain = Unpooled.wrappedBuffer(plain1, plain2);
    frameEncrypt.plain = Collections.singletonList(plain);
    frameEncrypt.out = getDirectBuffer(messageBytes.length + client.getSuffixLength(), ref);

    client.encrypt(frameEncrypt.out, frameEncrypt.plain);

    int tagLen = client.getSuffixLength();
    FrameDecrypt frameDecrypt = new FrameDecrypt();
    ByteBuf out = frameEncrypt.out;
    int outLen = out.readableBytes();
    ByteBuf cipher1 = getDirectBuffer(outLen - lastLen - tagLen, ref);
    ByteBuf cipher2 = getDirectBuffer(lastLen, ref);
    cipher1.writeBytes(out, 0, outLen - lastLen - tagLen);
    cipher2.writeBytes(out, outLen - tagLen - lastLen, lastLen);
    ByteBuf cipher = Unpooled.wrappedBuffer(cipher1, cipher2);
    frameDecrypt.ciphertext = Collections.singletonList(cipher);
    frameDecrypt.tag = out.slice(out.readerIndex() + out.readableBytes() - tagLen, tagLen);
    frameDecrypt.out = getDirectBuffer(out.readableBytes(), ref);

    server.decrypt(frameDecrypt.out, frameDecrypt.tag, frameDecrypt.ciphertext);
    assertThat(frameEncrypt.plain.get(0).slice(0, frameDecrypt.out.readableBytes()))
        .isEqualTo(frameDecrypt.out);
  }

  @Test
  public void reflection() throws GeneralSecurityException {
    String message = "Hello world";
    FrameEncrypt frameEncrypt = createFrameEncrypt(message);
    client.encrypt(frameEncrypt.out, frameEncrypt.plain);
    FrameDecrypt frameDecrypt = frameDecryptOfEncrypt(frameEncrypt);
    try {
      client.decrypt(frameDecrypt.out, frameDecrypt.tag, frameDecrypt.ciphertext);
      fail("Exception expected");
    } catch (AEADBadTagException ex) {
      assertThat(ex).hasMessageThat().contains(DECRYPTION_FAILURE_MESSAGE);
    }
  }

  @Test
  public void skipMessage() throws GeneralSecurityException {
    String message = "Hello world";
    FrameEncrypt frameEncrypt1 = createFrameEncrypt(message);
    client.encrypt(frameEncrypt1.out, frameEncrypt1.plain);
    FrameEncrypt frameEncrypt2 = createFrameEncrypt(message);
    client.encrypt(frameEncrypt2.out, frameEncrypt2.plain);
    FrameDecrypt frameDecrypt = frameDecryptOfEncrypt(frameEncrypt2);

    try {
      client.decrypt(frameDecrypt.out, frameDecrypt.tag, frameDecrypt.ciphertext);
      fail("Exception expected");
    } catch (AEADBadTagException ex) {
      assertThat(ex).hasMessageThat().contains(DECRYPTION_FAILURE_MESSAGE);
    }
  }

  @Test
  public void corruptMessage() throws GeneralSecurityException {
    String message = "Hello world";
    FrameEncrypt frameEncrypt = createFrameEncrypt(message);
    client.encrypt(frameEncrypt.out, frameEncrypt.plain);
    FrameDecrypt frameDecrypt = frameDecryptOfEncrypt(frameEncrypt);
    frameEncrypt.out.setByte(3, frameEncrypt.out.getByte(3) + 1);

    try {
      client.decrypt(frameDecrypt.out, frameDecrypt.tag, frameDecrypt.ciphertext);
      fail("Exception expected");
    } catch (AEADBadTagException ex) {
      assertThat(ex).hasMessageThat().contains(DECRYPTION_FAILURE_MESSAGE);
    }
  }

  @Test
  public void replayMessage() throws GeneralSecurityException {
    String message = "Hello world";
    FrameEncrypt frameEncrypt = createFrameEncrypt(message);
    client.encrypt(frameEncrypt.out, frameEncrypt.plain);
    FrameDecrypt frameDecrypt1 = frameDecryptOfEncrypt(frameEncrypt);
    FrameDecrypt frameDecrypt2 = frameDecryptOfEncrypt(frameEncrypt);

    server.decrypt(frameDecrypt1.out, frameDecrypt1.tag, frameDecrypt1.ciphertext);

    try {
      server.decrypt(frameDecrypt2.out, frameDecrypt2.tag, frameDecrypt2.ciphertext);
      fail("Exception expected");
    } catch (AEADBadTagException ex) {
      assertThat(ex).hasMessageThat().contains(DECRYPTION_FAILURE_MESSAGE);
    }
  }
}
