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
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableList;
import com.google.protobuf.ByteString;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.ArgumentMatchers;

/** Unit tests for {@link AltsHandshakerClient}. */
@RunWith(JUnit4.class)
public class AltsHandshakerClientTest {
  private static final int IN_BYTES_SIZE = 100;
  private static final int BYTES_CONSUMED = 30;
  private static final int PREFIX_POSITION = 20;
  private static final String TEST_TARGET_NAME = "target name";
  private static final String TEST_TARGET_SERVICE_ACCOUNT = "peer service account";

  private AltsHandshakerStub mockStub;
  private AltsHandshakerClient handshaker;
  private AltsClientOptions clientOptions;

  @Before
  public void setUp() {
    mockStub = mock(AltsHandshakerStub.class);
    clientOptions =
        new AltsClientOptions.Builder()
            .setTargetName(TEST_TARGET_NAME)
            .setTargetServiceAccounts(ImmutableList.of(TEST_TARGET_SERVICE_ACCOUNT))
            .build();
    handshaker = new AltsHandshakerClient(mockStub, clientOptions);
  }

  @Test
  public void startClientHandshakeFailure() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getErrorResponse());

    try {
      handshaker.startClientHandshake();
      fail("Exception expected");
    } catch (GeneralSecurityException ex) {
      assertThat(ex).hasMessageThat().contains(MockAltsHandshakerResp.getTestErrorDetails());
    }
  }

  @Test
  public void startClientHandshakeSuccess() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getOkResponse(0));

    ByteBuffer outFrame = handshaker.startClientHandshake();

    assertEquals(ByteString.copyFrom(outFrame), MockAltsHandshakerResp.getOutFrame());
    assertFalse(handshaker.isFinished());
    assertNull(handshaker.getResult());
    assertNull(handshaker.getKey());
  }

  @Test
  public void startClientHandshakeWithOptions() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getOkResponse(0));

    ByteBuffer outFrame = handshaker.startClientHandshake();
    assertEquals(ByteString.copyFrom(outFrame), MockAltsHandshakerResp.getOutFrame());

    HandshakerReq req =
        HandshakerReq.newBuilder()
            .setClientStart(
                StartClientHandshakeReq.newBuilder()
                    .setHandshakeSecurityProtocol(HandshakeProtocol.ALTS)
                    .addApplicationProtocols(AltsHandshakerClient.getApplicationProtocol())
                    .addRecordProtocols(AltsHandshakerClient.getRecordProtocol())
                    .setTargetName(TEST_TARGET_NAME)
                    .addTargetIdentities(
                        Identity.newBuilder().setServiceAccount(TEST_TARGET_SERVICE_ACCOUNT))
                    .build())
            .build();
    verify(mockStub).send(req);
  }

  @Test
  public void startServerHandshakeFailure() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getErrorResponse());

    try {
      ByteBuffer inBytes = ByteBuffer.allocate(IN_BYTES_SIZE);
      handshaker.startServerHandshake(inBytes);
      fail("Exception expected");
    } catch (GeneralSecurityException ex) {
      assertThat(ex).hasMessageThat().contains(MockAltsHandshakerResp.getTestErrorDetails());
    }
  }

  @Test
  public void startServerHandshakeSuccess() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getOkResponse(BYTES_CONSUMED));

    ByteBuffer inBytes = ByteBuffer.allocate(IN_BYTES_SIZE);
    ByteBuffer outFrame = handshaker.startServerHandshake(inBytes);

    assertEquals(ByteString.copyFrom(outFrame), MockAltsHandshakerResp.getOutFrame());
    assertFalse(handshaker.isFinished());
    assertNull(handshaker.getResult());
    assertNull(handshaker.getKey());
    assertEquals(IN_BYTES_SIZE - BYTES_CONSUMED, inBytes.remaining());
  }

  @Test
  public void startServerHandshakeEmptyOutFrame() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getEmptyOutFrameResponse(BYTES_CONSUMED));

    ByteBuffer inBytes = ByteBuffer.allocate(IN_BYTES_SIZE);
    ByteBuffer outFrame = handshaker.startServerHandshake(inBytes);

    assertEquals(0, outFrame.remaining());
    assertFalse(handshaker.isFinished());
    assertNull(handshaker.getResult());
    assertNull(handshaker.getKey());
    assertEquals(IN_BYTES_SIZE - BYTES_CONSUMED, inBytes.remaining());
  }

  @Test
  public void startServerHandshakeWithPrefixBuffer() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getOkResponse(BYTES_CONSUMED));

    ByteBuffer inBytes = ByteBuffer.allocate(IN_BYTES_SIZE);
    inBytes.position(PREFIX_POSITION);
    ByteBuffer outFrame = handshaker.startServerHandshake(inBytes);

    assertEquals(ByteString.copyFrom(outFrame), MockAltsHandshakerResp.getOutFrame());
    assertFalse(handshaker.isFinished());
    assertNull(handshaker.getResult());
    assertNull(handshaker.getKey());
    assertEquals(PREFIX_POSITION + BYTES_CONSUMED, inBytes.position());
    assertEquals(IN_BYTES_SIZE - BYTES_CONSUMED - PREFIX_POSITION, inBytes.remaining());
  }

  @Test
  public void nextFailure() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getErrorResponse());

    try {
      ByteBuffer inBytes = ByteBuffer.allocate(IN_BYTES_SIZE);
      handshaker.next(inBytes);
      fail("Exception expected");
    } catch (GeneralSecurityException ex) {
      assertThat(ex).hasMessageThat().contains(MockAltsHandshakerResp.getTestErrorDetails());
    }
  }

  @Test
  public void nextSuccess() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getOkResponse(BYTES_CONSUMED));

    ByteBuffer inBytes = ByteBuffer.allocate(IN_BYTES_SIZE);
    ByteBuffer outFrame = handshaker.next(inBytes);

    assertEquals(ByteString.copyFrom(outFrame), MockAltsHandshakerResp.getOutFrame());
    assertFalse(handshaker.isFinished());
    assertNull(handshaker.getResult());
    assertNull(handshaker.getKey());
    assertEquals(IN_BYTES_SIZE - BYTES_CONSUMED, inBytes.remaining());
  }

  @Test
  public void nextEmptyOutFrame() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getEmptyOutFrameResponse(BYTES_CONSUMED));

    ByteBuffer inBytes = ByteBuffer.allocate(IN_BYTES_SIZE);
    ByteBuffer outFrame = handshaker.next(inBytes);

    assertEquals(0, outFrame.remaining());
    assertFalse(handshaker.isFinished());
    assertNull(handshaker.getResult());
    assertNull(handshaker.getKey());
    assertEquals(IN_BYTES_SIZE - BYTES_CONSUMED, inBytes.remaining());
  }

  @Test
  public void nextFinished() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getFinishedResponse(BYTES_CONSUMED));

    ByteBuffer inBytes = ByteBuffer.allocate(IN_BYTES_SIZE);
    ByteBuffer outFrame = handshaker.next(inBytes);

    assertEquals(ByteString.copyFrom(outFrame), MockAltsHandshakerResp.getOutFrame());
    assertTrue(handshaker.isFinished());
    assertArrayEquals(handshaker.getKey(), MockAltsHandshakerResp.getTestKeyData());
    assertEquals(IN_BYTES_SIZE - BYTES_CONSUMED, inBytes.remaining());
  }

  @Test
  public void setRpcVersions() throws Exception {
    when(mockStub.send(ArgumentMatchers.<HandshakerReq>any()))
        .thenReturn(MockAltsHandshakerResp.getOkResponse(0));

    RpcProtocolVersions rpcVersions =
        RpcProtocolVersions.newBuilder()
            .setMinRpcVersion(
                RpcProtocolVersions.Version.newBuilder().setMajor(3).setMinor(4).build())
            .setMaxRpcVersion(
                RpcProtocolVersions.Version.newBuilder().setMajor(5).setMinor(6).build())
            .build();
    clientOptions =
        new AltsClientOptions.Builder()
            .setTargetName(TEST_TARGET_NAME)
            .setTargetServiceAccounts(ImmutableList.of(TEST_TARGET_SERVICE_ACCOUNT))
            .setRpcProtocolVersions(rpcVersions)
            .build();
    handshaker = new AltsHandshakerClient(mockStub, clientOptions);

    handshaker.startClientHandshake();

    ArgumentCaptor<HandshakerReq> reqCaptor = ArgumentCaptor.forClass(HandshakerReq.class);
    verify(mockStub).send(reqCaptor.capture());
    assertEquals(rpcVersions, reqCaptor.getValue().getClientStart().getRpcVersions());
  }
}
