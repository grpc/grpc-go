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
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.util.ReferenceCounted;
import java.lang.reflect.Method;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.List;
import org.junit.After;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class NettyTsiHandshakerTest {
  private final UnpooledByteBufAllocator alloc = UnpooledByteBufAllocator.DEFAULT;
  private final List<ReferenceCounted> references = new ArrayList<>();

  private final NettyTsiHandshaker clientHandshaker =
      new NettyTsiHandshaker(FakeTsiHandshaker.newFakeHandshakerClient());
  private final NettyTsiHandshaker serverHandshaker =
      new NettyTsiHandshaker(FakeTsiHandshaker.newFakeHandshakerServer());

  @After
  public void teardown() {
    for (ReferenceCounted reference : references) {
      reference.release(reference.refCnt());
    }
  }

  @Test
  public void failsOnNullHandshaker() {
    try {
      new NettyTsiHandshaker(null);
      fail("Exception expected");
    } catch (NullPointerException ex) {
      // Do nothing.
    }
  }

  @Test
  public void processPeerHandshakeShouldAcceptPartialFrames() throws GeneralSecurityException {
    for (int i = 0; i < 1024; i++) {
      ByteBuf clientData = ref(alloc.buffer(1));
      clientHandshaker.getBytesToSendToPeer(clientData);
      if (clientData.isReadable()) {
        if (serverHandshaker.processBytesFromPeer(clientData)) {
          // Done.
          return;
        }
      }
    }
    fail("Failed to process the handshake frame.");
  }

  @Test
  public void handshakeShouldSucceed() throws GeneralSecurityException {
    doHandshake();
  }

  @Test
  public void isInProgress() throws GeneralSecurityException {
    assertTrue(clientHandshaker.isInProgress());
    assertTrue(serverHandshaker.isInProgress());

    doHandshake();

    assertFalse(clientHandshaker.isInProgress());
    assertFalse(serverHandshaker.isInProgress());
  }

  @Test
  public void extractPeer_notNull() throws GeneralSecurityException {
    doHandshake();

    assertNotNull(serverHandshaker.extractPeer());
    assertNotNull(clientHandshaker.extractPeer());
  }

  @Test
  public void extractPeer_failsBeforeHandshake() throws GeneralSecurityException {
    try {
      clientHandshaker.extractPeer();
      fail("Exception expected");
    } catch (IllegalStateException ex) {
      // Do nothing.
    }
  }

  @Test
  public void extractPeerObject_notNull() throws GeneralSecurityException {
    doHandshake();

    assertNotNull(serverHandshaker.extractPeerObject());
    assertNotNull(clientHandshaker.extractPeerObject());
  }

  @Test
  public void extractPeerObject_failsBeforeHandshake() throws GeneralSecurityException {
    try {
      clientHandshaker.extractPeerObject();
      fail("Exception expected");
    } catch (IllegalStateException ex) {
      // Do nothing.
    }
  }

  /**
   * NettyTsiHandshaker just converts {@link ByteBuffer} to {@link ByteBuf}, so check that the other
   * methods are otherwise the same.
   */
  @Test
  public void handshakerMethodsMatch() {
    List<String> expectedMethods = new ArrayList<>();
    for (Method m : TsiHandshaker.class.getDeclaredMethods()) {
      expectedMethods.add(m.getName());
    }

    List<String> actualMethods = new ArrayList<>();
    for (Method m : NettyTsiHandshaker.class.getDeclaredMethods()) {
      actualMethods.add(m.getName());
    }

    assertThat(actualMethods).containsAllIn(expectedMethods);
  }

  static void doHandshake(
      NettyTsiHandshaker clientHandshaker,
      NettyTsiHandshaker serverHandshaker,
      ByteBufAllocator alloc,
      Function<ByteBuf, ByteBuf> ref)
      throws GeneralSecurityException {
    // Get the server response handshake frames.
    for (int i = 0; i < 10; i++) {
      if (!(clientHandshaker.isInProgress() || serverHandshaker.isInProgress())) {
        return;
      }

      ByteBuf clientData = ref.apply(alloc.buffer());
      clientHandshaker.getBytesToSendToPeer(clientData);
      if (clientData.isReadable()) {
        serverHandshaker.processBytesFromPeer(clientData);
      }

      ByteBuf serverData = ref.apply(alloc.buffer());
      serverHandshaker.getBytesToSendToPeer(serverData);
      if (serverData.isReadable()) {
        clientHandshaker.processBytesFromPeer(serverData);
      }
    }

    throw new AssertionError("Failed to complete the handshake.");
  }

  private void doHandshake() throws GeneralSecurityException {
    doHandshake(
        clientHandshaker,
        serverHandshaker,
        alloc,
        new Function<ByteBuf, ByteBuf>() {
          @Override
          public ByteBuf apply(ByteBuf buf) {
            return ref(buf);
          }
        });
  }

  private ByteBuf ref(ByteBuf buf) {
    if (buf != null) {
      references.add(buf);
    }
    return buf;
  }

  /** A mirror of java.util.function.Function without the Java 8 dependency. */
  private interface Function<T, R> {
    R apply(T t);
  }
}
