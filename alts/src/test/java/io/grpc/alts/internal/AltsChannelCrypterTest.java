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
import static io.grpc.alts.internal.AltsChannelCrypter.incrementCounter;
import static org.junit.Assert.fail;

import com.google.common.testing.GcFinalization;
import io.netty.util.ReferenceCounted;
import io.netty.util.ResourceLeakDetector;
import io.netty.util.ResourceLeakDetector.Level;
import java.security.GeneralSecurityException;
import java.util.Arrays;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AltsChannelCrypter}. */
@RunWith(JUnit4.class)
public final class AltsChannelCrypterTest extends ChannelCrypterNettyTestBase {

  @Before
  public void setUp() throws GeneralSecurityException {
    ResourceLeakDetector.setLevel(Level.PARANOID);
    client = new AltsChannelCrypter(new byte[AltsChannelCrypter.getKeyLength()], true);
    server = new AltsChannelCrypter(new byte[AltsChannelCrypter.getKeyLength()], false);
  }

  @After
  public void tearDown() throws GeneralSecurityException {
    for (ReferenceCounted reference : references) {
      reference.release();
    }
    references.clear();
    client.destroy();
    server.destroy();
    // Increase our chances to detect ByteBuf leaks.
    GcFinalization.awaitFullGc();
  }

  @Test
  public void encryptDecryptKdfCounterIncr() throws GeneralSecurityException {
    AltsChannelCrypter client =
        new AltsChannelCrypter(new byte[AltsChannelCrypter.getKeyLength()], true);
    AltsChannelCrypter server =
        new AltsChannelCrypter(new byte[AltsChannelCrypter.getKeyLength()], false);

    String message = "Hello world";
    FrameEncrypt frameEncrypt1 = createFrameEncrypt(message);

    client.encrypt(frameEncrypt1.out, frameEncrypt1.plain);
    FrameDecrypt frameDecrypt1 = frameDecryptOfEncrypt(frameEncrypt1);

    server.decrypt(frameDecrypt1.out, frameDecrypt1.tag, frameDecrypt1.ciphertext);
    assertThat(frameEncrypt1.plain.get(0).slice(0, frameDecrypt1.out.readableBytes()))
        .isEqualTo(frameDecrypt1.out);

    // Increase counters to get a new KDF counter value (first two bytes are skipped).
    client.incrementOutCounterForTesting(1 << 17);
    server.incrementInCounterForTesting(1 << 17);

    FrameEncrypt frameEncrypt2 = createFrameEncrypt(message);

    client.encrypt(frameEncrypt2.out, frameEncrypt2.plain);
    FrameDecrypt frameDecrypt2 = frameDecryptOfEncrypt(frameEncrypt2);

    server.decrypt(frameDecrypt2.out, frameDecrypt2.tag, frameDecrypt2.ciphertext);
    assertThat(frameEncrypt2.plain.get(0).slice(0, frameDecrypt2.out.readableBytes()))
        .isEqualTo(frameDecrypt2.out);
  }

  @Test
  public void overflowsClient() throws GeneralSecurityException {
    byte[] maxFirst =
        new byte[] {
          (byte) 0xFF, (byte) 0xFF, (byte) 0xFF, (byte) 0xFF,
          (byte) 0xFF, (byte) 0xFF, (byte) 0xFF, (byte) 0xFF,
          (byte) 0x00, (byte) 0x00, (byte) 0x00, (byte) 0x00
        };

    byte[] maxFirstPred = Arrays.copyOf(maxFirst, maxFirst.length);
    maxFirstPred[0]--;

    byte[] oldCounter = new byte[AltsChannelCrypter.getCounterLength()];
    byte[] counter = Arrays.copyOf(maxFirstPred, maxFirstPred.length);

    incrementCounter(counter, oldCounter);

    assertThat(oldCounter).isEqualTo(maxFirstPred);
    assertThat(counter).isEqualTo(maxFirst);

    try {
      incrementCounter(counter, oldCounter);
      fail("Exception expected");
    } catch (GeneralSecurityException ex) {
      assertThat(ex).hasMessageThat().contains("Counter has overflowed");
    }

    assertThat(oldCounter).isEqualTo(maxFirst);
    assertThat(counter).isEqualTo(maxFirst);
  }

  @Test
  public void overflowsServer() throws GeneralSecurityException {
    byte[] maxSecond =
        new byte[] {
          (byte) 0xFF, (byte) 0xFF, (byte) 0xFF, (byte) 0xFF,
          (byte) 0xFF, (byte) 0xFF, (byte) 0xFF, (byte) 0xFF,
          (byte) 0x00, (byte) 0x00, (byte) 0x00, (byte) 0x80
        };

    byte[] maxSecondPred = Arrays.copyOf(maxSecond, maxSecond.length);
    maxSecondPred[0]--;

    byte[] oldCounter = new byte[AltsChannelCrypter.getCounterLength()];
    byte[] counter = Arrays.copyOf(maxSecondPred, maxSecondPred.length);

    incrementCounter(counter, oldCounter);

    assertThat(oldCounter).isEqualTo(maxSecondPred);
    assertThat(counter).isEqualTo(maxSecond);

    try {
      incrementCounter(counter, oldCounter);
      fail("Exception expected");
    } catch (GeneralSecurityException ex) {
      assertThat(ex).hasMessageThat().contains("Counter has overflowed");
    }

    assertThat(oldCounter).isEqualTo(maxSecond);
    assertThat(counter).isEqualTo(maxSecond);
  }
}
