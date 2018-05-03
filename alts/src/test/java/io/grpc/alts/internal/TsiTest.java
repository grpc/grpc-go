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
import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.fail;

import io.grpc.alts.internal.ByteBufTestUtils.RegisterRef;
import io.grpc.alts.internal.TsiFrameProtector.Consumer;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.buffer.UnpooledByteBufAllocator;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import javax.crypto.AEADBadTagException;

/** Utility class that provides tests for implementations of @{link TsiHandshaker}. */
public final class TsiTest {
  private static final String DECRYPTION_FAILURE_RE = "Tag mismatch!";

  private TsiTest() {}

  /** A @{code TsiHandshaker} pair for running tests. */
  public static class Handshakers {
    private final TsiHandshaker client;
    private final TsiHandshaker server;

    public Handshakers(TsiHandshaker client, TsiHandshaker server) {
      this.client = client;
      this.server = server;
    }

    public TsiHandshaker getClient() {
      return client;
    }

    public TsiHandshaker getServer() {
      return server;
    }
  }

  private static final int DEFAULT_TRANSPORT_BUFFER_SIZE = 2048;

  private static final UnpooledByteBufAllocator alloc = UnpooledByteBufAllocator.DEFAULT;

  private static final String EXAMPLE_MESSAGE1 = "hello world";
  private static final String EXAMPLE_MESSAGE2 = "oysteroystersoysterseateateat";

  private static final int EXAMPLE_MESSAGE1_LEN = EXAMPLE_MESSAGE1.getBytes(UTF_8).length;
  private static final int EXAMPLE_MESSAGE2_LEN = EXAMPLE_MESSAGE2.getBytes(UTF_8).length;

  static int getDefaultTransportBufferSize() {
    return DEFAULT_TRANSPORT_BUFFER_SIZE;
  }

  /**
   * Performs a handshake between the client handshaker and server handshaker using a transport of
   * length transportBufferSize.
   */
  static void performHandshake(int transportBufferSize, Handshakers handshakers)
      throws GeneralSecurityException {
    TsiHandshaker clientHandshaker = handshakers.getClient();
    TsiHandshaker serverHandshaker = handshakers.getServer();

    byte[] transportBufferBytes = new byte[transportBufferSize];
    ByteBuffer transportBuffer = ByteBuffer.wrap(transportBufferBytes);
    transportBuffer.limit(0); // Start off with an empty buffer

    while (clientHandshaker.isInProgress() || serverHandshaker.isInProgress()) {
      for (TsiHandshaker handshaker : new TsiHandshaker[] {clientHandshaker, serverHandshaker}) {
        if (handshaker.isInProgress()) {
          // Process any bytes on the wire.
          if (transportBuffer.hasRemaining()) {
            handshaker.processBytesFromPeer(transportBuffer);
          }
          // Put new bytes on the wire, if needed.
          if (handshaker.isInProgress()) {
            transportBuffer.clear();
            handshaker.getBytesToSendToPeer(transportBuffer);
            transportBuffer.flip();
          }
        }
      }
    }
    clientHandshaker.extractPeer();
    serverHandshaker.extractPeer();
  }

  public static void handshakeTest(Handshakers handshakers) throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);
  }

  public static void handshakeSmallBufferTest(Handshakers handshakers)
      throws GeneralSecurityException {
    performHandshake(9, handshakers);
  }

  /** Sends a message between the sender and receiver. */
  private static void sendMessage(
      TsiFrameProtector sender,
      TsiFrameProtector receiver,
      int recvFragmentSize,
      String message,
      RegisterRef ref)
      throws GeneralSecurityException {

    ByteBuf plaintextBuffer = Unpooled.wrappedBuffer(message.getBytes(UTF_8));
    final List<ByteBuf> protectOut = new ArrayList<>();
    List<Object> unprotectOut = new ArrayList<>();

    sender.protectFlush(
        Collections.singletonList(plaintextBuffer),
        new Consumer<ByteBuf>() {
          @Override
          public void accept(ByteBuf buf) {
            protectOut.add(buf);
          }
        },
        alloc);
    assertThat(protectOut.size()).isEqualTo(1);

    ByteBuf protect = ref.register(protectOut.get(0));
    while (protect.isReadable()) {
      ByteBuf buf = protect;
      if (recvFragmentSize > 0) {
        int size = Math.min(protect.readableBytes(), recvFragmentSize);
        buf = protect.readSlice(size);
      }
      receiver.unprotect(buf, unprotectOut, alloc);
    }
    ByteBuf plaintextRecvd = getDirectBuffer(message.getBytes(UTF_8).length, ref);
    for (Object unprotect : unprotectOut) {
      ByteBuf unprotectBuf = ref.register((ByteBuf) unprotect);
      plaintextRecvd.writeBytes(unprotectBuf);
    }
    assertThat(plaintextRecvd).isEqualTo(Unpooled.wrappedBuffer(message.getBytes(UTF_8)));
  }

  /** Ping pong test. */
  public static void pingPongTest(Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    TsiFrameProtector clientProtector = handshakers.getClient().createFrameProtector(alloc);
    TsiFrameProtector serverProtector = handshakers.getServer().createFrameProtector(alloc);

    sendMessage(clientProtector, serverProtector, -1, EXAMPLE_MESSAGE1, ref);
    sendMessage(serverProtector, clientProtector, -1, EXAMPLE_MESSAGE2, ref);

    clientProtector.destroy();
    serverProtector.destroy();
  }

  /** Ping pong test with exact frame size. */
  public static void pingPongExactFrameSizeTest(Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    int frameSize =
        EXAMPLE_MESSAGE1.getBytes(UTF_8).length
            + AltsTsiFrameProtector.getHeaderBytes()
            + FakeChannelCrypter.getTagBytes();

    TsiFrameProtector clientProtector =
        handshakers.getClient().createFrameProtector(frameSize, alloc);
    TsiFrameProtector serverProtector =
        handshakers.getServer().createFrameProtector(frameSize, alloc);

    sendMessage(clientProtector, serverProtector, -1, EXAMPLE_MESSAGE1, ref);
    sendMessage(serverProtector, clientProtector, -1, EXAMPLE_MESSAGE1, ref);

    clientProtector.destroy();
    serverProtector.destroy();
  }

  /** Ping pong test with small buffer size. */
  public static void pingPongSmallBufferTest(Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    TsiFrameProtector clientProtector = handshakers.getClient().createFrameProtector(alloc);
    TsiFrameProtector serverProtector = handshakers.getServer().createFrameProtector(alloc);

    sendMessage(clientProtector, serverProtector, 1, EXAMPLE_MESSAGE1, ref);
    sendMessage(serverProtector, clientProtector, 1, EXAMPLE_MESSAGE2, ref);

    clientProtector.destroy();
    serverProtector.destroy();
  }

  /** Ping pong test with small frame size. */
  public static void pingPongSmallFrameTest(
      int frameProtectorOverhead, Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    // We send messages using small non-aligned buffers. We use 3 and 5, small primes.
    TsiFrameProtector clientProtector =
        handshakers.getClient().createFrameProtector(frameProtectorOverhead + 3, alloc);
    TsiFrameProtector serverProtector =
        handshakers.getServer().createFrameProtector(frameProtectorOverhead + 5, alloc);

    sendMessage(clientProtector, serverProtector, EXAMPLE_MESSAGE1_LEN, EXAMPLE_MESSAGE1, ref);
    sendMessage(serverProtector, clientProtector, EXAMPLE_MESSAGE2_LEN, EXAMPLE_MESSAGE2, ref);

    clientProtector.destroy();
    serverProtector.destroy();
  }

  /** Ping pong test with small frame and small buffer. */
  public static void pingPongSmallFrameSmallBufferTest(
      int frameProtectorOverhead, Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    // We send messages using small non-aligned buffers. We use 3 and 5, small primes.
    TsiFrameProtector clientProtector =
        handshakers.getClient().createFrameProtector(frameProtectorOverhead + 3, alloc);
    TsiFrameProtector serverProtector =
        handshakers.getServer().createFrameProtector(frameProtectorOverhead + 5, alloc);

    sendMessage(clientProtector, serverProtector, EXAMPLE_MESSAGE1_LEN, EXAMPLE_MESSAGE1, ref);
    sendMessage(serverProtector, clientProtector, EXAMPLE_MESSAGE2_LEN, EXAMPLE_MESSAGE2, ref);

    sendMessage(clientProtector, serverProtector, EXAMPLE_MESSAGE1_LEN, EXAMPLE_MESSAGE1, ref);
    sendMessage(serverProtector, clientProtector, EXAMPLE_MESSAGE2_LEN, EXAMPLE_MESSAGE2, ref);

    clientProtector.destroy();
    serverProtector.destroy();
  }

  /** Test corrupted counter. */
  public static void corruptedCounterTest(Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    TsiFrameProtector sender = handshakers.getClient().createFrameProtector(alloc);
    TsiFrameProtector receiver = handshakers.getServer().createFrameProtector(alloc);

    String message = "hello world";
    ByteBuf plaintextBuffer = Unpooled.wrappedBuffer(message.getBytes(UTF_8));
    final List<ByteBuf> protectOut = new ArrayList<>();
    List<Object> unprotectOut = new ArrayList<>();

    sender.protectFlush(
        Collections.singletonList(plaintextBuffer),
        new Consumer<ByteBuf>() {
          @Override
          public void accept(ByteBuf buf) {
            protectOut.add(buf);
          }
        },
        alloc);
    assertThat(protectOut.size()).isEqualTo(1);

    ByteBuf protect = ref.register(protectOut.get(0));
    // Unprotect once to increase receiver counter.
    receiver.unprotect(protect.slice(), unprotectOut, alloc);
    assertThat(unprotectOut.size()).isEqualTo(1);
    ref.register((ByteBuf) unprotectOut.get(0));

    try {
      receiver.unprotect(protect, unprotectOut, alloc);
      fail("Exception expected");
    } catch (AEADBadTagException ex) {
      assertThat(ex).hasMessageThat().contains(DECRYPTION_FAILURE_RE);
    }

    sender.destroy();
    receiver.destroy();
  }

  /** Test corrupted ciphertext. */
  public static void corruptedCiphertextTest(Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    TsiFrameProtector sender = handshakers.getClient().createFrameProtector(alloc);
    TsiFrameProtector receiver = handshakers.getServer().createFrameProtector(alloc);

    String message = "hello world";
    ByteBuf plaintextBuffer = Unpooled.wrappedBuffer(message.getBytes(UTF_8));
    final List<ByteBuf> protectOut = new ArrayList<>();
    List<Object> unprotectOut = new ArrayList<>();

    sender.protectFlush(
        Collections.singletonList(plaintextBuffer),
        new Consumer<ByteBuf>() {
          @Override
          public void accept(ByteBuf buf) {
            protectOut.add(buf);
          }
        },
        alloc);
    assertThat(protectOut.size()).isEqualTo(1);

    ByteBuf protect = ref.register(protectOut.get(0));
    int ciphertextIdx = protect.writerIndex() - FakeChannelCrypter.getTagBytes() - 2;
    protect.setByte(ciphertextIdx, protect.getByte(ciphertextIdx) + 1);

    try {
      receiver.unprotect(protect, unprotectOut, alloc);
      fail("Exception expected");
    } catch (AEADBadTagException ex) {
      assertThat(ex).hasMessageThat().contains(DECRYPTION_FAILURE_RE);
    }

    sender.destroy();
    receiver.destroy();
  }

  /** Test corrupted tag. */
  public static void corruptedTagTest(Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    TsiFrameProtector sender = handshakers.getClient().createFrameProtector(alloc);
    TsiFrameProtector receiver = handshakers.getServer().createFrameProtector(alloc);

    String message = "hello world";
    ByteBuf plaintextBuffer = Unpooled.wrappedBuffer(message.getBytes(UTF_8));
    final List<ByteBuf> protectOut = new ArrayList<>();
    List<Object> unprotectOut = new ArrayList<>();

    sender.protectFlush(
        Collections.singletonList(plaintextBuffer),
        new Consumer<ByteBuf>() {
          @Override
          public void accept(ByteBuf buf) {
            protectOut.add(buf);
          }
        },
        alloc);
    assertThat(protectOut.size()).isEqualTo(1);

    ByteBuf protect = ref.register(protectOut.get(0));
    int tagIdx = protect.writerIndex() - 1;
    protect.setByte(tagIdx, protect.getByte(tagIdx) + 1);

    try {
      receiver.unprotect(protect, unprotectOut, alloc);
      fail("Exception expected");
    } catch (AEADBadTagException ex) {
      assertThat(ex).hasMessageThat().contains(DECRYPTION_FAILURE_RE);
    }

    sender.destroy();
    receiver.destroy();
  }

  /** Test reflected ciphertext. */
  public static void reflectedCiphertextTest(Handshakers handshakers, RegisterRef ref)
      throws GeneralSecurityException {
    performHandshake(DEFAULT_TRANSPORT_BUFFER_SIZE, handshakers);

    TsiFrameProtector sender = handshakers.getClient().createFrameProtector(alloc);
    TsiFrameProtector receiver = handshakers.getServer().createFrameProtector(alloc);

    String message = "hello world";
    ByteBuf plaintextBuffer = Unpooled.wrappedBuffer(message.getBytes(UTF_8));
    final List<ByteBuf> protectOut = new ArrayList<>();
    List<Object> unprotectOut = new ArrayList<>();

    sender.protectFlush(
        Collections.singletonList(plaintextBuffer),
        new Consumer<ByteBuf>() {
          @Override
          public void accept(ByteBuf buf) {
            protectOut.add(buf);
          }
        },
        alloc);
    assertThat(protectOut.size()).isEqualTo(1);

    ByteBuf protect = ref.register(protectOut.get(0));
    try {
      sender.unprotect(protect.slice(), unprotectOut, alloc);
      fail("Exception expected");
    } catch (AEADBadTagException ex) {
      assertThat(ex).hasMessageThat().contains(DECRYPTION_FAILURE_RE);
    }

    sender.destroy();
    receiver.destroy();
  }
}
