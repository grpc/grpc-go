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

import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;

import com.google.common.testing.GcFinalization;
import io.grpc.alts.internal.ByteBufTestUtils.RegisterRef;
import io.grpc.alts.internal.TsiTest.Handshakers;
import io.netty.buffer.ByteBuf;
import io.netty.util.ReferenceCounted;
import io.netty.util.ResourceLeakDetector;
import io.netty.util.ResourceLeakDetector.Level;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.List;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link TsiHandshaker}. */
@RunWith(JUnit4.class)
public class FakeTsiTest {

  private static final int OVERHEAD =
      FakeChannelCrypter.getTagBytes() + AltsTsiFrameProtector.getHeaderBytes();

  private final List<ReferenceCounted> references = new ArrayList<>();
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

  private static Handshakers newHandshakers() {
    TsiHandshaker clientHandshaker = FakeTsiHandshaker.newFakeHandshakerClient();
    TsiHandshaker serverHandshaker = FakeTsiHandshaker.newFakeHandshakerServer();
    return new Handshakers(clientHandshaker, serverHandshaker);
  }

  @Before
  public void setUp() {
    ResourceLeakDetector.setLevel(Level.PARANOID);
  }

  @After
  public void tearDown() {
    for (ReferenceCounted reference : references) {
      reference.release();
    }
    references.clear();
    // Increase our chances to detect ByteBuf leaks.
    GcFinalization.awaitFullGc();
  }

  @Test
  public void handshakeStateOrderTest() {
    try {
      Handshakers handshakers = newHandshakers();
      TsiHandshaker clientHandshaker = handshakers.getClient();
      TsiHandshaker serverHandshaker = handshakers.getServer();

      byte[] transportBufferBytes = new byte[TsiTest.getDefaultTransportBufferSize()];
      ByteBuffer transportBuffer = ByteBuffer.wrap(transportBufferBytes);
      transportBuffer.limit(0); // Start off with an empty buffer

      transportBuffer.clear();
      clientHandshaker.getBytesToSendToPeer(transportBuffer);
      transportBuffer.flip();
      assertEquals(
          FakeTsiHandshaker.State.CLIENT_INIT.toString().trim(),
          new String(transportBufferBytes, 4, transportBuffer.remaining(), UTF_8).trim());

      serverHandshaker.processBytesFromPeer(transportBuffer);
      assertFalse(transportBuffer.hasRemaining());

      // client shouldn't offer any more bytes
      transportBuffer.clear();
      clientHandshaker.getBytesToSendToPeer(transportBuffer);
      transportBuffer.flip();
      assertFalse(transportBuffer.hasRemaining());

      transportBuffer.clear();
      serverHandshaker.getBytesToSendToPeer(transportBuffer);
      transportBuffer.flip();
      assertEquals(
          FakeTsiHandshaker.State.SERVER_INIT.toString().trim(),
          new String(transportBufferBytes, 4, transportBuffer.remaining(), UTF_8).trim());

      clientHandshaker.processBytesFromPeer(transportBuffer);
      assertFalse(transportBuffer.hasRemaining());

      // server shouldn't offer any more bytes
      transportBuffer.clear();
      serverHandshaker.getBytesToSendToPeer(transportBuffer);
      transportBuffer.flip();
      assertFalse(transportBuffer.hasRemaining());

      transportBuffer.clear();
      clientHandshaker.getBytesToSendToPeer(transportBuffer);
      transportBuffer.flip();
      assertEquals(
          FakeTsiHandshaker.State.CLIENT_FINISHED.toString().trim(),
          new String(transportBufferBytes, 4, transportBuffer.remaining(), UTF_8).trim());

      serverHandshaker.processBytesFromPeer(transportBuffer);
      assertFalse(transportBuffer.hasRemaining());

      // client shouldn't offer any more bytes
      transportBuffer.clear();
      clientHandshaker.getBytesToSendToPeer(transportBuffer);
      transportBuffer.flip();
      assertFalse(transportBuffer.hasRemaining());

      transportBuffer.clear();
      serverHandshaker.getBytesToSendToPeer(transportBuffer);
      transportBuffer.flip();
      assertEquals(
          FakeTsiHandshaker.State.SERVER_FINISHED.toString().trim(),
          new String(transportBufferBytes, 4, transportBuffer.remaining(), UTF_8).trim());

      clientHandshaker.processBytesFromPeer(transportBuffer);
      assertFalse(transportBuffer.hasRemaining());

      // server shouldn't offer any more bytes
      transportBuffer.clear();
      serverHandshaker.getBytesToSendToPeer(transportBuffer);
      transportBuffer.flip();
      assertFalse(transportBuffer.hasRemaining());
    } catch (GeneralSecurityException e) {
      throw new AssertionError(e);
    }
  }

  @Test
  public void handshake() throws GeneralSecurityException {
    TsiTest.handshakeTest(newHandshakers());
  }

  @Test
  public void handshakeSmallBuffer() throws GeneralSecurityException {
    TsiTest.handshakeSmallBufferTest(newHandshakers());
  }

  @Test
  public void pingPong() throws GeneralSecurityException {
    TsiTest.pingPongTest(newHandshakers(), ref);
  }

  @Test
  public void pingPongExactFrameSize() throws GeneralSecurityException {
    TsiTest.pingPongExactFrameSizeTest(newHandshakers(), ref);
  }

  @Test
  public void pingPongSmallBuffer() throws GeneralSecurityException {
    TsiTest.pingPongSmallBufferTest(newHandshakers(), ref);
  }

  @Test
  public void pingPongSmallFrame() throws GeneralSecurityException {
    TsiTest.pingPongSmallFrameTest(OVERHEAD, newHandshakers(), ref);
  }

  @Test
  public void pingPongSmallFrameSmallBuffer() throws GeneralSecurityException {
    TsiTest.pingPongSmallFrameSmallBufferTest(OVERHEAD, newHandshakers(), ref);
  }

  @Test
  public void corruptedCounter() throws GeneralSecurityException {
    TsiTest.corruptedCounterTest(newHandshakers(), ref);
  }

  @Test
  public void corruptedCiphertext() throws GeneralSecurityException {
    TsiTest.corruptedCiphertextTest(newHandshakers(), ref);
  }

  @Test
  public void corruptedTag() throws GeneralSecurityException {
    TsiTest.corruptedTagTest(newHandshakers(), ref);
  }

  @Test
  public void reflectedCiphertext() throws GeneralSecurityException {
    TsiTest.reflectedCiphertextTest(newHandshakers(), ref);
  }
}
