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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.protobuf.ByteString;
import java.nio.ByteBuffer;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentMatchers;

/** Unit tests for {@link AltsTsiHandshaker}. */
@RunWith(JUnit4.class)
public class AltsTsiHandshakerTest {
  private static final String TEST_KEY_DATA = "super secret 123";
  private static final String TEST_APPLICATION_PROTOCOL = "grpc";
  private static final String TEST_RECORD_PROTOCOL = "ALTSRP_GCM_AES128";
  private static final String TEST_CLIENT_SERVICE_ACCOUNT = "client@developer.gserviceaccount.com";
  private static final String TEST_SERVER_SERVICE_ACCOUNT = "server@developer.gserviceaccount.com";
  private static final int OUT_FRAME_SIZE = 100;
  private static final int TRANSPORT_BUFFER_SIZE = 200;
  private static final int TEST_MAX_RPC_VERSION_MAJOR = 3;
  private static final int TEST_MAX_RPC_VERSION_MINOR = 2;
  private static final int TEST_MIN_RPC_VERSION_MAJOR = 2;
  private static final int TEST_MIN_RPC_VERSION_MINOR = 1;
  private static final RpcProtocolVersions TEST_RPC_PROTOCOL_VERSIONS =
      RpcProtocolVersions.newBuilder()
          .setMaxRpcVersion(
              RpcProtocolVersions.Version.newBuilder()
                  .setMajor(TEST_MAX_RPC_VERSION_MAJOR)
                  .setMinor(TEST_MAX_RPC_VERSION_MINOR)
                  .build())
          .setMinRpcVersion(
              RpcProtocolVersions.Version.newBuilder()
                  .setMajor(TEST_MIN_RPC_VERSION_MAJOR)
                  .setMinor(TEST_MIN_RPC_VERSION_MINOR)
                  .build())
          .build();

  private AltsHandshakerClient mockClient;
  private AltsHandshakerClient mockServer;
  private AltsTsiHandshaker handshakerClient;
  private AltsTsiHandshaker handshakerServer;

  @Before
  public void setUp() throws Exception {
    mockClient = mock(AltsHandshakerClient.class);
    mockServer = mock(AltsHandshakerClient.class);
    handshakerClient = new AltsTsiHandshaker(true, mockClient);
    handshakerServer = new AltsTsiHandshaker(false, mockServer);
  }

  private HandshakerResult getHandshakerResult(boolean isClient) {
    HandshakerResult.Builder builder =
        HandshakerResult.newBuilder()
            .setApplicationProtocol(TEST_APPLICATION_PROTOCOL)
            .setRecordProtocol(TEST_RECORD_PROTOCOL)
            .setKeyData(ByteString.copyFromUtf8(TEST_KEY_DATA))
            .setPeerRpcVersions(TEST_RPC_PROTOCOL_VERSIONS);
    if (isClient) {
      builder.setPeerIdentity(
          Identity.newBuilder().setServiceAccount(TEST_SERVER_SERVICE_ACCOUNT).build());
      builder.setLocalIdentity(
          Identity.newBuilder().setServiceAccount(TEST_CLIENT_SERVICE_ACCOUNT).build());
    } else {
      builder.setPeerIdentity(
          Identity.newBuilder().setServiceAccount(TEST_CLIENT_SERVICE_ACCOUNT).build());
      builder.setLocalIdentity(
          Identity.newBuilder().setServiceAccount(TEST_SERVER_SERVICE_ACCOUNT).build());
    }
    return builder.build();
  }

  @Test
  public void processBytesFromPeerFalseStart() throws Exception {
    verify(mockClient, never()).startClientHandshake();
    verify(mockClient, never()).startServerHandshake(ArgumentMatchers.<ByteBuffer>any());
    verify(mockClient, never()).next(ArgumentMatchers.<ByteBuffer>any());

    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    assertTrue(handshakerClient.processBytesFromPeer(transportBuffer));
  }

  @Test
  public void processBytesFromPeerStartServer() throws Exception {
    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    ByteBuffer outputFrame = ByteBuffer.allocate(OUT_FRAME_SIZE);
    verify(mockServer, never()).startClientHandshake();
    verify(mockServer, never()).next(ArgumentMatchers.<ByteBuffer>any());
    // Mock transport buffer all consumed by processBytesFromPeer and there is an output frame.
    transportBuffer.position(transportBuffer.limit());
    when(mockServer.startServerHandshake(transportBuffer)).thenReturn(outputFrame);
    when(mockServer.isFinished()).thenReturn(false);

    assertTrue(handshakerServer.processBytesFromPeer(transportBuffer));
  }

  @Test
  public void processBytesFromPeerStartServerEmptyOutput() throws Exception {
    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    ByteBuffer emptyOutputFrame = ByteBuffer.allocate(0);
    verify(mockServer, never()).startClientHandshake();
    verify(mockServer, never()).next(ArgumentMatchers.<ByteBuffer>any());
    // Mock transport buffer all consumed by processBytesFromPeer and output frame is empty.
    // Expect processBytesFromPeer return False, because more data are needed from the peer.
    transportBuffer.position(transportBuffer.limit());
    when(mockServer.startServerHandshake(transportBuffer)).thenReturn(emptyOutputFrame);
    when(mockServer.isFinished()).thenReturn(false);

    assertFalse(handshakerServer.processBytesFromPeer(transportBuffer));
  }

  @Test
  public void processBytesFromPeerStartServerFinished() throws Exception {
    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    ByteBuffer outputFrame = ByteBuffer.allocate(OUT_FRAME_SIZE);
    verify(mockServer, never()).startClientHandshake();
    verify(mockServer, never()).next(ArgumentMatchers.<ByteBuffer>any());
    // Mock handshake complete after processBytesFromPeer.
    when(mockServer.startServerHandshake(transportBuffer)).thenReturn(outputFrame);
    when(mockServer.isFinished()).thenReturn(true);

    assertTrue(handshakerServer.processBytesFromPeer(transportBuffer));
  }

  @Test
  public void processBytesFromPeerNoBytesConsumed() throws Exception {
    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    ByteBuffer emptyOutputFrame = ByteBuffer.allocate(0);
    verify(mockServer, never()).startClientHandshake();
    verify(mockServer, never()).next(ArgumentMatchers.<ByteBuffer>any());
    when(mockServer.startServerHandshake(transportBuffer)).thenReturn(emptyOutputFrame);
    when(mockServer.isFinished()).thenReturn(false);

    try {
      assertTrue(handshakerServer.processBytesFromPeer(transportBuffer));
      fail("Expected IllegalStateException");
    } catch (IllegalStateException expected) {
      assertEquals("Handshaker did not consume any bytes.", expected.getMessage());
    }
  }

  @Test
  public void processBytesFromPeerClientNext() throws Exception {
    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    ByteBuffer outputFrame = ByteBuffer.allocate(OUT_FRAME_SIZE);
    verify(mockClient, never()).startServerHandshake(ArgumentMatchers.<ByteBuffer>any());
    when(mockClient.startClientHandshake()).thenReturn(outputFrame);
    when(mockClient.next(transportBuffer)).thenReturn(outputFrame);
    when(mockClient.isFinished()).thenReturn(false);

    handshakerClient.getBytesToSendToPeer(transportBuffer);
    transportBuffer.position(transportBuffer.limit());
    assertFalse(handshakerClient.processBytesFromPeer(transportBuffer));
  }

  @Test
  public void processBytesFromPeerClientNextFinished() throws Exception {
    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    ByteBuffer outputFrame = ByteBuffer.allocate(OUT_FRAME_SIZE);
    verify(mockClient, never()).startServerHandshake(ArgumentMatchers.<ByteBuffer>any());
    when(mockClient.startClientHandshake()).thenReturn(outputFrame);
    when(mockClient.next(transportBuffer)).thenReturn(outputFrame);
    when(mockClient.isFinished()).thenReturn(true);

    handshakerClient.getBytesToSendToPeer(transportBuffer);
    assertTrue(handshakerClient.processBytesFromPeer(transportBuffer));
  }

  @Test
  public void extractPeerFailure() throws Exception {
    when(mockClient.isFinished()).thenReturn(false);

    try {
      handshakerClient.extractPeer();
      fail("Expected IllegalStateException");
    } catch (IllegalStateException expected) {
      assertEquals("Handshake is not complete.", expected.getMessage());
    }
  }

  @Test
  public void extractPeerObjectFailure() throws Exception {
    when(mockClient.isFinished()).thenReturn(false);

    try {
      handshakerClient.extractPeerObject();
      fail("Expected IllegalStateException");
    } catch (IllegalStateException expected) {
      assertEquals("Handshake is not complete.", expected.getMessage());
    }
  }

  @Test
  public void extractClientPeerSuccess() throws Exception {
    ByteBuffer outputFrame = ByteBuffer.allocate(OUT_FRAME_SIZE);
    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    when(mockClient.startClientHandshake()).thenReturn(outputFrame);
    when(mockClient.isFinished()).thenReturn(true);
    when(mockClient.getResult()).thenReturn(getHandshakerResult(/* isClient = */ true));

    handshakerClient.getBytesToSendToPeer(transportBuffer);
    TsiPeer clientPeer = handshakerClient.extractPeer();

    assertEquals(1, clientPeer.getProperties().size());
    assertEquals(
        TEST_SERVER_SERVICE_ACCOUNT,
        clientPeer.getProperty(AltsTsiHandshaker.TSI_SERVICE_ACCOUNT_PEER_PROPERTY).getValue());

    AltsAuthContext clientContext = (AltsAuthContext) handshakerClient.extractPeerObject();
    assertEquals(TEST_APPLICATION_PROTOCOL, clientContext.getApplicationProtocol());
    assertEquals(TEST_RECORD_PROTOCOL, clientContext.getRecordProtocol());
    assertEquals(TEST_SERVER_SERVICE_ACCOUNT, clientContext.getPeerServiceAccount());
    assertEquals(TEST_CLIENT_SERVICE_ACCOUNT, clientContext.getLocalServiceAccount());
    assertEquals(TEST_RPC_PROTOCOL_VERSIONS, clientContext.getPeerRpcVersions());
  }

  @Test
  public void extractServerPeerSuccess() throws Exception {
    ByteBuffer outputFrame = ByteBuffer.allocate(OUT_FRAME_SIZE);
    ByteBuffer transportBuffer = ByteBuffer.allocate(TRANSPORT_BUFFER_SIZE);
    when(mockServer.startServerHandshake(ArgumentMatchers.<ByteBuffer>any()))
        .thenReturn(outputFrame);
    when(mockServer.isFinished()).thenReturn(true);
    when(mockServer.getResult()).thenReturn(getHandshakerResult(/* isClient = */ false));

    handshakerServer.processBytesFromPeer(transportBuffer);
    handshakerServer.getBytesToSendToPeer(transportBuffer);
    TsiPeer serverPeer = handshakerServer.extractPeer();

    assertEquals(1, serverPeer.getProperties().size());
    assertEquals(
        TEST_CLIENT_SERVICE_ACCOUNT,
        serverPeer.getProperty(AltsTsiHandshaker.TSI_SERVICE_ACCOUNT_PEER_PROPERTY).getValue());

    AltsAuthContext serverContext = (AltsAuthContext) handshakerServer.extractPeerObject();
    assertEquals(TEST_APPLICATION_PROTOCOL, serverContext.getApplicationProtocol());
    assertEquals(TEST_RECORD_PROTOCOL, serverContext.getRecordProtocol());
    assertEquals(TEST_CLIENT_SERVICE_ACCOUNT, serverContext.getPeerServiceAccount());
    assertEquals(TEST_SERVER_SERVICE_ACCOUNT, serverContext.getLocalServiceAccount());
    assertEquals(TEST_RPC_PROTOCOL_VERSIONS, serverContext.getPeerRpcVersions());
  }
}
