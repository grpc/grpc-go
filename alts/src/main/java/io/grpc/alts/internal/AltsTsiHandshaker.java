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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.alts.internal.HandshakerServiceGrpc.HandshakerServiceStub;
import io.netty.buffer.ByteBufAllocator;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.List;

/**
 * Negotiates a grpc channel key to be used by the TsiFrameProtector, using ALTs handshaker service.
 */
public final class AltsTsiHandshaker implements TsiHandshaker {
  public static final String TSI_SERVICE_ACCOUNT_PEER_PROPERTY = "service_account";

  private final boolean isClient;
  private final AltsHandshakerClient handshaker;

  private ByteBuffer outputFrame;

  /** Starts a new TSI handshaker with client options. */
  private AltsTsiHandshaker(
      boolean isClient, HandshakerServiceStub stub, AltsHandshakerOptions options) {
    this.isClient = isClient;
    handshaker = new AltsHandshakerClient(stub, options);
  }

  @VisibleForTesting
  AltsTsiHandshaker(boolean isClient, AltsHandshakerClient handshaker) {
    this.isClient = isClient;
    this.handshaker = handshaker;
  }

  /**
   * Process the bytes received from the peer.
   *
   * @param bytes The buffer containing the handshake bytes from the peer.
   * @return true, if the handshake has all the data it needs to process and false, if the method
   *     must be called again to complete processing.
   */
  @Override
  public boolean processBytesFromPeer(ByteBuffer bytes) throws GeneralSecurityException {
    // If we're the client and we haven't given an output frame, we shouldn't be processing any
    // bytes.
    if (outputFrame == null && isClient) {
      return true;
    }
    // If we already have bytes to write, just return.
    if (outputFrame != null && outputFrame.hasRemaining()) {
      return true;
    }
    int remaining = bytes.remaining();
    // Call handshaker service to proceess the bytes.
    if (outputFrame == null) {
      checkState(!isClient, "Client handshaker should not process any frame at the beginning.");
      outputFrame = handshaker.startServerHandshake(bytes);
    } else {
      outputFrame = handshaker.next(bytes);
    }
    // If handshake has finished or we already have bytes to write, just return true.
    if (handshaker.isFinished() || outputFrame.hasRemaining()) {
      return true;
    }
    // We have done processing input bytes, but no bytes to write. Thus we need more data.
    if (!bytes.hasRemaining()) {
      return false;
    }
    // There are still remaining bytes. Thus we need to continue processing the bytes.
    // Prevent infinite loop by checking some bytes are consumed by handshaker.
    checkState(bytes.remaining() < remaining, "Handshaker did not consume any bytes.");
    return processBytesFromPeer(bytes);
  }

  /**
   * Returns the peer extracted from a completed handshake.
   *
   * @return the extracted peer.
   */
  @Override
  public TsiPeer extractPeer() throws GeneralSecurityException {
    Preconditions.checkState(!isInProgress(), "Handshake is not complete.");
    List<TsiPeer.Property<?>> peerProperties = new ArrayList<TsiPeer.Property<?>>();
    peerProperties.add(
        new TsiPeer.StringProperty(
            TSI_SERVICE_ACCOUNT_PEER_PROPERTY,
            handshaker.getResult().getPeerIdentity().getServiceAccount()));
    return new TsiPeer(peerProperties);
  }

  /**
   * Returns the peer extracted from a completed handshake.
   *
   * @return the extracted peer.
   */
  @Override
  public Object extractPeerObject() throws GeneralSecurityException {
    Preconditions.checkState(!isInProgress(), "Handshake is not complete.");
    return new AltsAuthContext(handshaker.getResult());
  }

  /** Creates a new TsiHandshaker for use by the client. */
  public static TsiHandshaker newClient(HandshakerServiceStub stub, AltsHandshakerOptions options) {
    return new AltsTsiHandshaker(true, stub, options);
  }

  /** Creates a new TsiHandshaker for use by the server. */
  public static TsiHandshaker newServer(HandshakerServiceStub stub, AltsHandshakerOptions options) {
    return new AltsTsiHandshaker(false, stub, options);
  }

  /**
   * Gets bytes that need to be sent to the peer.
   *
   * @param bytes The buffer to put handshake bytes.
   */
  @Override
  public void getBytesToSendToPeer(ByteBuffer bytes) throws GeneralSecurityException {
    if (outputFrame == null) { // A null outputFrame indicates we haven't started the handshake.
      if (isClient) {
        outputFrame = handshaker.startClientHandshake();
      } else {
        // The server needs bytes to process before it can start the handshake.
        return;
      }
    }
    // Write as many bytes as we are able.
    ByteBuffer outputFrameAlias = outputFrame;
    if (outputFrame.remaining() > bytes.remaining()) {
      outputFrameAlias = outputFrame.duplicate();
      outputFrameAlias.limit(outputFrameAlias.position() + bytes.remaining());
    }
    bytes.put(outputFrameAlias);
    outputFrame.position(outputFrameAlias.position());
  }

  /**
   * Returns true if and only if the handshake is still in progress
   *
   * @return true, if the handshake is still in progress, false otherwise.
   */
  @Override
  public boolean isInProgress() {
    return !handshaker.isFinished() || outputFrame.hasRemaining();
  }

  /**
   * Creates a frame protector from a completed handshake. No other methods may be called after the
   * frame protector is created.
   *
   * @param maxFrameSize the requested max frame size, the callee is free to ignore.
   * @param alloc used for allocating ByteBufs.
   * @return a new TsiFrameProtector.
   */
  @Override
  public TsiFrameProtector createFrameProtector(int maxFrameSize, ByteBufAllocator alloc) {
    Preconditions.checkState(!isInProgress(), "Handshake is not complete.");

    byte[] key = handshaker.getKey();
    Preconditions.checkState(key.length == AltsChannelCrypter.getKeyLength(), "Bad key length.");

    return new AltsTsiFrameProtector(maxFrameSize, new AltsChannelCrypter(key, isClient), alloc);
  }

  /**
   * Creates a frame protector from a completed handshake. No other methods may be called after the
   * frame protector is created.
   *
   * @param alloc used for allocating ByteBufs.
   * @return a new TsiFrameProtector.
   */
  @Override
  public TsiFrameProtector createFrameProtector(ByteBufAllocator alloc) {
    return createFrameProtector(AltsTsiFrameProtector.getMaxAllowedFrameBytes(), alloc);
  }
}
