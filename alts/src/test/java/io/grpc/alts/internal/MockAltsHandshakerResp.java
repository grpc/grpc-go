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

import com.google.protobuf.ByteString;
import io.grpc.Status;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.security.SecureRandom;
import java.util.Random;

/** A class for mocking ALTS Handshaker Responses. */
class MockAltsHandshakerResp {
  private static final String TEST_ERROR_DETAILS = "handshake error";
  private static final String TEST_APPLICATION_PROTOCOL = "grpc";
  private static final String TEST_RECORD_PROTOCOL = "ALTSRP_GCM_AES128";
  private static final String TEST_OUT_FRAME = "output frame";
  private static final String TEST_LOCAL_ACCOUNT = "local@developer.gserviceaccount.com";
  private static final String TEST_PEER_ACCOUNT = "peer@developer.gserviceaccount.com";
  private static final byte[] TEST_KEY_DATA = initializeTestKeyData();
  private static final int FRAME_HEADER_SIZE = 4;

  static String getTestErrorDetails() {
    return TEST_ERROR_DETAILS;
  }

  static String getTestPeerAccount() {
    return TEST_PEER_ACCOUNT;
  }

  private static byte[] initializeTestKeyData() {
    Random random = new SecureRandom();
    byte[] randombytes = new byte[AltsChannelCrypter.getKeyLength()];
    random.nextBytes(randombytes);
    return randombytes;
  }

  static byte[] getTestKeyData() {
    return TEST_KEY_DATA;
  }

  /** Returns a mock output frame. */
  static ByteString getOutFrame() {
    int frameSize = TEST_OUT_FRAME.length();
    ByteBuffer buffer = ByteBuffer.allocate(FRAME_HEADER_SIZE + frameSize);
    buffer.order(ByteOrder.LITTLE_ENDIAN);
    buffer.putInt(frameSize);
    buffer.put(TEST_OUT_FRAME.getBytes(UTF_8));
    buffer.flip();
    return ByteString.copyFrom(buffer);
  }

  /** Returns a mock error handshaker response. */
  static HandshakerResp getErrorResponse() {
    HandshakerResp.Builder resp = HandshakerResp.newBuilder();
    resp.setStatus(
        HandshakerStatus.newBuilder()
            .setCode(Status.Code.UNKNOWN.value())
            .setDetails(TEST_ERROR_DETAILS)
            .build());
    return resp.build();
  }

  /** Returns a mock normal handshaker response. */
  static HandshakerResp getOkResponse(int bytesConsumed) {
    HandshakerResp.Builder resp = HandshakerResp.newBuilder();
    resp.setOutFrames(getOutFrame());
    resp.setBytesConsumed(bytesConsumed);
    resp.setStatus(HandshakerStatus.newBuilder().setCode(Status.Code.OK.value()).build());
    return resp.build();
  }

  /** Returns a mock normal handshaker response. */
  static HandshakerResp getEmptyOutFrameResponse(int bytesConsumed) {
    HandshakerResp.Builder resp = HandshakerResp.newBuilder();
    resp.setBytesConsumed(bytesConsumed);
    resp.setStatus(HandshakerStatus.newBuilder().setCode(Status.Code.OK.value()).build());
    return resp.build();
  }

  /** Returns a mock final handshaker response with handshake result. */
  static HandshakerResp getFinishedResponse(int bytesConsumed) {
    HandshakerResp.Builder resp = HandshakerResp.newBuilder();
    HandshakerResult.Builder result =
        HandshakerResult.newBuilder()
            .setApplicationProtocol(TEST_APPLICATION_PROTOCOL)
            .setRecordProtocol(TEST_RECORD_PROTOCOL)
            .setPeerIdentity(Identity.newBuilder().setServiceAccount(TEST_PEER_ACCOUNT).build())
            .setLocalIdentity(Identity.newBuilder().setServiceAccount(TEST_LOCAL_ACCOUNT).build())
            .setKeyData(ByteString.copyFrom(TEST_KEY_DATA));
    resp.setOutFrames(getOutFrame());
    resp.setBytesConsumed(bytesConsumed);
    resp.setStatus(HandshakerStatus.newBuilder().setCode(Status.Code.OK.value()).build());
    resp.setResult(result.build());
    return resp.build();
  }
}
