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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Strings;
import com.google.protobuf.ByteString;
import io.grpc.Status;
import io.grpc.alts.internal.Handshaker.HandshakeProtocol;
import io.grpc.alts.internal.Handshaker.HandshakerReq;
import io.grpc.alts.internal.Handshaker.HandshakerResp;
import io.grpc.alts.internal.Handshaker.HandshakerResult;
import io.grpc.alts.internal.Handshaker.HandshakerStatus;
import io.grpc.alts.internal.Handshaker.NextHandshakeMessageReq;
import io.grpc.alts.internal.Handshaker.ServerHandshakeParameters;
import io.grpc.alts.internal.Handshaker.StartClientHandshakeReq;
import io.grpc.alts.internal.Handshaker.StartServerHandshakeReq;
import io.grpc.alts.internal.HandshakerServiceGrpc.HandshakerServiceStub;
import java.io.IOException;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.logging.Level;
import java.util.logging.Logger;

/** An API for conducting handshakes via ALTS handshaker service. */
class AltsHandshakerClient {
  private static final Logger logger = Logger.getLogger(AltsHandshakerClient.class.getName());

  private static final String APPLICATION_PROTOCOL = "grpc";
  private static final String RECORD_PROTOCOL = "ALTSRP_GCM_AES128_REKEY";
  private static final int KEY_LENGTH = AltsChannelCrypter.getKeyLength();

  private final AltsHandshakerStub handshakerStub;
  private final AltsHandshakerOptions handshakerOptions;
  private HandshakerResult result;
  private HandshakerStatus status;

  /** Starts a new handshake interacting with the handshaker service. */
  AltsHandshakerClient(HandshakerServiceStub stub, AltsHandshakerOptions options) {
    handshakerStub = new AltsHandshakerStub(stub);
    handshakerOptions = options;
  }

  @VisibleForTesting
  AltsHandshakerClient(AltsHandshakerStub handshakerStub, AltsHandshakerOptions options) {
    this.handshakerStub = handshakerStub;
    handshakerOptions = options;
  }

  static String getApplicationProtocol() {
    return APPLICATION_PROTOCOL;
  }

  static String getRecordProtocol() {
    return RECORD_PROTOCOL;
  }

  /** Sets the start client fields for the passed handshake request. */
  private void setStartClientFields(HandshakerReq.Builder req) {
    // Sets the default values.
    StartClientHandshakeReq.Builder startClientReq =
        StartClientHandshakeReq.newBuilder()
            .setHandshakeSecurityProtocol(HandshakeProtocol.ALTS)
            .addApplicationProtocols(APPLICATION_PROTOCOL)
            .addRecordProtocols(RECORD_PROTOCOL);
    // Sets handshaker options.
    if (handshakerOptions.getRpcProtocolVersions() != null) {
      startClientReq.setRpcVersions(handshakerOptions.getRpcProtocolVersions());
    }
    if (handshakerOptions instanceof AltsClientOptions) {
      AltsClientOptions clientOptions = (AltsClientOptions) handshakerOptions;
      if (!Strings.isNullOrEmpty(clientOptions.getTargetName())) {
        startClientReq.setTargetName(clientOptions.getTargetName());
      }
      for (String serviceAccount : clientOptions.getTargetServiceAccounts()) {
        startClientReq.addTargetIdentitiesBuilder().setServiceAccount(serviceAccount);
      }
    }
    req.setClientStart(startClientReq);
  }

  /** Sets the start server fields for the passed handshake request. */
  private void setStartServerFields(HandshakerReq.Builder req, ByteBuffer inBytes) {
    ServerHandshakeParameters serverParameters =
        ServerHandshakeParameters.newBuilder().addRecordProtocols(RECORD_PROTOCOL).build();
    StartServerHandshakeReq.Builder startServerReq =
        StartServerHandshakeReq.newBuilder()
            .addApplicationProtocols(APPLICATION_PROTOCOL)
            .putHandshakeParameters(HandshakeProtocol.ALTS.getNumber(), serverParameters)
            .setInBytes(ByteString.copyFrom(inBytes.duplicate()));
    if (handshakerOptions.getRpcProtocolVersions() != null) {
      startServerReq.setRpcVersions(handshakerOptions.getRpcProtocolVersions());
    }
    req.setServerStart(startServerReq);
  }

  /** Returns true if the handshake is complete. */
  public boolean isFinished() {
    // If we have a HandshakeResult, we are done.
    if (result != null) {
      return true;
    }
    // If we have an error status, we are done.
    if (status != null && status.getCode() != Status.Code.OK.value()) {
      return true;
    }
    return false;
  }

  /** Returns the handshake status. */
  public HandshakerStatus getStatus() {
    return status;
  }

  /** Returns the result data of the handshake, if the handshake is completed. */
  public HandshakerResult getResult() {
    return result;
  }

  /**
   * Returns the resulting key of the handshake, if the handshake is completed. Note that the key
   * data returned from the handshake may be more than the key length required for the record
   * protocol, thus we need to truncate to the right size.
   */
  public byte[] getKey() {
    if (result == null) {
      return null;
    }
    if (result.getKeyData().size() < KEY_LENGTH) {
      throw new IllegalStateException("Could not get enough key data from the handshake.");
    }
    byte[] key = new byte[KEY_LENGTH];
    result.getKeyData().copyTo(key, 0, 0, KEY_LENGTH);
    return key;
  }

  /**
   * Parses a handshake response, setting the status, result, and closing the handshaker, as needed.
   */
  private void handleResponse(HandshakerResp resp) throws GeneralSecurityException {
    status = resp.getStatus();
    if (resp.hasResult()) {
      result = resp.getResult();
      close();
    }
    if (status.getCode() != Status.Code.OK.value()) {
      String error = "Handshaker service error: " + status.getDetails();
      logger.log(Level.INFO, error);
      close();
      throw new GeneralSecurityException(error);
    }
  }

  /**
   * Starts a client handshake. A GeneralSecurityException is thrown if the handshaker service is
   * interrupted or fails. Note that isFinished() must be false before this function is called.
   *
   * @return the frame to give to the peer.
   * @throws GeneralSecurityException or IllegalStateException
   */
  public ByteBuffer startClientHandshake() throws GeneralSecurityException {
    Preconditions.checkState(!isFinished(), "Handshake has already finished.");
    HandshakerReq.Builder req = HandshakerReq.newBuilder();
    setStartClientFields(req);
    HandshakerResp resp;
    try {
      resp = handshakerStub.send(req.build());
    } catch (IOException | InterruptedException e) {
      throw new GeneralSecurityException(e);
    }
    handleResponse(resp);
    return resp.getOutFrames().asReadOnlyByteBuffer();
  }

  /**
   * Starts a server handshake. A GeneralSecurityException is thrown if the handshaker service is
   * interrupted or fails. Note that isFinished() must be false before this function is called.
   *
   * @param inBytes the bytes received from the peer.
   * @return the frame to give to the peer.
   * @throws GeneralSecurityException or IllegalStateException
   */
  public ByteBuffer startServerHandshake(ByteBuffer inBytes) throws GeneralSecurityException {
    Preconditions.checkState(!isFinished(), "Handshake has already finished.");
    HandshakerReq.Builder req = HandshakerReq.newBuilder();
    setStartServerFields(req, inBytes);
    HandshakerResp resp;
    try {
      resp = handshakerStub.send(req.build());
    } catch (IOException | InterruptedException e) {
      throw new GeneralSecurityException(e);
    }
    handleResponse(resp);
    inBytes.position(inBytes.position() + resp.getBytesConsumed());
    return resp.getOutFrames().asReadOnlyByteBuffer();
  }

  /**
   * Processes the next bytes in a handshake. A GeneralSecurityException is thrown if the handshaker
   * service is interrupted or fails. Note that isFinished() must be false before this function is
   * called.
   *
   * @param inBytes the bytes received from the peer.
   * @return the frame to give to the peer.
   * @throws GeneralSecurityException or IllegalStateException
   */
  public ByteBuffer next(ByteBuffer inBytes) throws GeneralSecurityException {
    Preconditions.checkState(!isFinished(), "Handshake has already finished.");
    HandshakerReq.Builder req =
        HandshakerReq.newBuilder()
            .setNext(
                NextHandshakeMessageReq.newBuilder()
                    .setInBytes(ByteString.copyFrom(inBytes.duplicate()))
                    .build());
    HandshakerResp resp;
    try {
      resp = handshakerStub.send(req.build());
    } catch (IOException | InterruptedException e) {
      throw new GeneralSecurityException(e);
    }
    handleResponse(resp);
    inBytes.position(inBytes.position() + resp.getBytesConsumed());
    return resp.getOutFrames().asReadOnlyByteBuffer();
  }

  /** Closes the connection. */
  public void close() {
    handshakerStub.close();
  }
}
