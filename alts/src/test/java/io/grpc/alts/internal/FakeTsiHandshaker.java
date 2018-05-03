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

import com.google.common.base.Preconditions;
import io.grpc.alts.internal.TsiPeer.Property;
import io.netty.buffer.ByteBufAllocator;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.Collections;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A fake handshaker compatible with security/transport_security/fake_transport_security.h See
 * {@link TsiHandshaker} for documentation.
 */
public class FakeTsiHandshaker implements TsiHandshaker {
  private static final Logger logger = Logger.getLogger(FakeTsiHandshaker.class.getName());

  private static final TsiHandshakerFactory clientHandshakerFactory =
      new TsiHandshakerFactory() {
        @Override
        public TsiHandshaker newHandshaker() {
          return new FakeTsiHandshaker(true);
        }
      };

  private static final TsiHandshakerFactory serverHandshakerFactory =
      new TsiHandshakerFactory() {
        @Override
        public TsiHandshaker newHandshaker() {
          return new FakeTsiHandshaker(false);
        }
      };

  private boolean isClient;
  private ByteBuffer sendBuffer = null;
  private AltsFraming.Parser frameParser = new AltsFraming.Parser();

  private State sendState;
  private State receiveState;

  enum State {
    CLIENT_NONE,
    SERVER_NONE,
    CLIENT_INIT,
    SERVER_INIT,
    CLIENT_FINISHED,
    SERVER_FINISHED;

    // Returns the next State. In order to advance to sendState=N, receiveState must be N-1.
    public State next() {
      if (ordinal() + 1 < values().length) {
        return values()[ordinal() + 1];
      }
      throw new UnsupportedOperationException("Can't call next() on last element: " + this);
    }
  }

  public static TsiHandshakerFactory clientHandshakerFactory() {
    return clientHandshakerFactory;
  }

  public static TsiHandshakerFactory serverHandshakerFactory() {
    return serverHandshakerFactory;
  }

  public static TsiHandshaker newFakeHandshakerClient() {
    return clientHandshakerFactory.newHandshaker();
  }

  public static TsiHandshaker newFakeHandshakerServer() {
    return serverHandshakerFactory.newHandshaker();
  }

  protected FakeTsiHandshaker(boolean isClient) {
    this.isClient = isClient;
    if (isClient) {
      sendState = State.CLIENT_NONE;
      receiveState = State.SERVER_NONE;
    } else {
      sendState = State.SERVER_NONE;
      receiveState = State.CLIENT_NONE;
    }
  }

  private State getNextState(State state) {
    switch (state) {
      case CLIENT_NONE:
        return State.CLIENT_INIT;
      case SERVER_NONE:
        return State.SERVER_INIT;
      case CLIENT_INIT:
        return State.CLIENT_FINISHED;
      case SERVER_INIT:
        return State.SERVER_FINISHED;
      default:
        return null;
    }
  }

  private String getNextMessage() {
    State result = getNextState(sendState);
    return result == null ? "BAD STATE" : result.toString();
  }

  private String getExpectedMessage() {
    State result = getNextState(receiveState);
    return result == null ? "BAD STATE" : result.toString();
  }

  private void incrementSendState() {
    sendState = getNextState(sendState);
  }

  private void incrementReceiveState() {
    receiveState = getNextState(receiveState);
  }

  @Override
  public void getBytesToSendToPeer(ByteBuffer bytes) throws GeneralSecurityException {
    Preconditions.checkNotNull(bytes);

    // If we're done, return nothing.
    if (sendState == State.CLIENT_FINISHED || sendState == State.SERVER_FINISHED) {
      return;
    }

    // Prepare the next message, if neeeded.
    if (sendBuffer == null) {
      if (sendState.next() != receiveState) {
        // We're still waiting for bytes from the peer, so bail.
        return;
      }
      ByteBuffer payload = ByteBuffer.wrap(getNextMessage().getBytes(UTF_8));
      sendBuffer = AltsFraming.toFrame(payload, payload.remaining());
      logger.log(Level.FINE, "Buffered message: {0}", getNextMessage());
    }
    while (bytes.hasRemaining() && sendBuffer.hasRemaining()) {
      bytes.put(sendBuffer.get());
    }
    if (!sendBuffer.hasRemaining()) {
      // Get ready to send the next message.
      sendBuffer = null;
      incrementSendState();
    }
  }

  @Override
  public boolean processBytesFromPeer(ByteBuffer bytes) throws GeneralSecurityException {
    Preconditions.checkNotNull(bytes);

    frameParser.readBytes(bytes);
    if (frameParser.isComplete()) {
      ByteBuffer messageBytes = frameParser.getRawFrame();
      int offset = AltsFraming.getFramingOverhead();
      int length = messageBytes.limit() - offset;
      String message = new String(messageBytes.array(), offset, length, UTF_8);
      logger.log(Level.FINE, "Read message: {0}", message);

      if (!message.equals(getExpectedMessage())) {
        throw new IllegalArgumentException(
            "Bad handshake message. Got "
                + message
                + " (length = "
                + message.length()
                + ") expected "
                + getExpectedMessage()
                + " (length = "
                + getExpectedMessage().length()
                + ")");
      }
      incrementReceiveState();
      return true;
    }
    return false;
  }

  @Override
  public boolean isInProgress() {
    boolean finishedReceiving =
        receiveState == State.CLIENT_FINISHED || receiveState == State.SERVER_FINISHED;
    boolean finishedSending =
        sendState == State.CLIENT_FINISHED || sendState == State.SERVER_FINISHED;
    return !finishedSending || !finishedReceiving;
  }

  @Override
  public TsiPeer extractPeer() {
    return new TsiPeer(Collections.<Property<?>>emptyList());
  }

  @Override
  public Object extractPeerObject() {
    return AltsAuthContext.getDefaultInstance();
  }

  @Override
  public TsiFrameProtector createFrameProtector(int maxFrameSize, ByteBufAllocator alloc) {
    Preconditions.checkState(!isInProgress(), "Handshake is not complete.");

    // We use an all-zero key, since this is the fake handshaker.
    byte[] key = new byte[AltsChannelCrypter.getKeyLength()];
    return new AltsTsiFrameProtector(maxFrameSize, new AltsChannelCrypter(key, isClient), alloc);
  }

  @Override
  public TsiFrameProtector createFrameProtector(ByteBufAllocator alloc) {
    return createFrameProtector(AltsTsiFrameProtector.getMaxAllowedFrameBytes(), alloc);
  }
}
