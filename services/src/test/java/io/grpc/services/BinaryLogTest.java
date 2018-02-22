/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

package io.grpc.services;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.primitives.Bytes;
import com.google.protobuf.ByteString;
import io.grpc.Metadata;
import io.grpc.binarylog.GrpcLogEntry;
import io.grpc.binarylog.Message;
import io.grpc.binarylog.MetadataEntry;
import io.grpc.binarylog.Peer;
import io.grpc.binarylog.Peer.PeerType;
import io.grpc.binarylog.Uint128;
import io.grpc.services.BinaryLog.FactoryImpl;
import io.netty.channel.unix.DomainSocketAddress;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.nio.ByteBuffer;
import java.nio.charset.Charset;
import java.util.Arrays;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Tests for {@link BinaryLog}. */
@RunWith(JUnit4.class)
public final class BinaryLogTest {
  private static final Charset US_ASCII = Charset.forName("US-ASCII");
  private static final BinaryLog HEADER_FULL = new Builder().header(Integer.MAX_VALUE).build();
  private static final BinaryLog HEADER_256 = new Builder().header(256).build();
  private static final BinaryLog MSG_FULL = new Builder().msg(Integer.MAX_VALUE).build();
  private static final BinaryLog MSG_256 = new Builder().msg(256).build();
  private static final BinaryLog BOTH_256 = new Builder().header(256).msg(256).build();
  private static final BinaryLog BOTH_FULL =
      new Builder().header(Integer.MAX_VALUE).msg(Integer.MAX_VALUE).build();

  private static final String DATA_A = "aaaaaaaaa";
  private static final String DATA_B = "bbbbbbbbb";
  private static final String DATA_C = "ccccccccc";
  private static final Metadata.Key<String> KEY_A =
      Metadata.Key.of("a", Metadata.ASCII_STRING_MARSHALLER);
  private static final Metadata.Key<String> KEY_B =
      Metadata.Key.of("b", Metadata.ASCII_STRING_MARSHALLER);
  private static final Metadata.Key<String> KEY_C =
      Metadata.Key.of("c", Metadata.ASCII_STRING_MARSHALLER);
  private static final MetadataEntry ENTRY_A =
      MetadataEntry
            .newBuilder()
            .setKey(ByteString.copyFrom(KEY_A.name(), US_ASCII))
            .setValue(ByteString.copyFrom(DATA_A.getBytes(US_ASCII)))
            .build();
  private static final MetadataEntry ENTRY_B =
        MetadataEntry
            .newBuilder()
            .setKey(ByteString.copyFrom(KEY_B.name(), US_ASCII))
            .setValue(ByteString.copyFrom(DATA_B.getBytes(US_ASCII)))
            .build();
  private static final MetadataEntry ENTRY_C =
        MetadataEntry
            .newBuilder()
            .setKey(ByteString.copyFrom(KEY_C.name(), US_ASCII))
            .setValue(ByteString.copyFrom(DATA_C.getBytes(US_ASCII)))
            .build();
  private static final boolean IS_SERVER = true;
  private static final boolean IS_CLIENT = false;
  private static final boolean IS_COMPRESSED = true;
  private static final boolean IS_UNCOMPRESSED = false;
  private static final byte[] CALL_ID = new byte[] {
      0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
      0x19, 0x10, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f };
  private static final int HEADER_LIMIT = 10;
  private static final int MESSAGE_LIMIT = Integer.MAX_VALUE;

  private final Metadata metadata = new Metadata();
  private final BinaryLogSink sink = mock(BinaryLogSink.class);
  private final BinaryLog binaryLog = new BinaryLog(sink, HEADER_LIMIT, MESSAGE_LIMIT);
  private final ByteBuffer message = ByteBuffer.wrap(new byte[100]);

  @Before
  public void setUp() throws Exception {
    metadata.put(KEY_A, DATA_A);
    metadata.put(KEY_B, DATA_B);
    metadata.put(KEY_C, DATA_C);
  }

  @Test
  public void configBinLog_global() throws Exception {
    assertSameLimits(BOTH_FULL, makeLog("*", "p.s/m"));
    assertSameLimits(BOTH_FULL, makeLog("*{h;m}", "p.s/m"));
    assertSameLimits(HEADER_FULL, makeLog("*{h}", "p.s/m"));
    assertSameLimits(MSG_FULL, makeLog("*{m}", "p.s/m"));
    assertSameLimits(HEADER_256, makeLog("*{h:256}", "p.s/m"));
    assertSameLimits(MSG_256, makeLog("*{m:256}", "p.s/m"));
    assertSameLimits(BOTH_256, makeLog("*{h:256;m:256}", "p.s/m"));
    assertSameLimits(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        makeLog("*{h;m:256}", "p.s/m"));
    assertSameLimits(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        makeLog("*{h:256;m}", "p.s/m"));
  }

  @Test
  public void configBinLog_method() throws Exception {
    assertSameLimits(BOTH_FULL, makeLog("p.s/m", "p.s/m"));
    assertSameLimits(BOTH_FULL, makeLog("p.s/m{h;m}", "p.s/m"));
    assertSameLimits(HEADER_FULL, makeLog("p.s/m{h}", "p.s/m"));
    assertSameLimits(MSG_FULL, makeLog("p.s/m{m}", "p.s/m"));
    assertSameLimits(HEADER_256, makeLog("p.s/m{h:256}", "p.s/m"));
    assertSameLimits(MSG_256, makeLog("p.s/m{m:256}", "p.s/m"));
    assertSameLimits(BOTH_256, makeLog("p.s/m{h:256;m:256}", "p.s/m"));
    assertSameLimits(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        makeLog("p.s/m{h;m:256}", "p.s/m"));
    assertSameLimits(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        makeLog("p.s/m{h:256;m}", "p.s/m"));
  }

  @Test
  public void configBinLog_method_absent() throws Exception {
    assertNull(makeLog("p.s/m", "p.s/absent"));
  }

  @Test
  public void configBinLog_service() throws Exception {
    assertSameLimits(BOTH_FULL, makeLog("p.s/*", "p.s/m"));
    assertSameLimits(BOTH_FULL, makeLog("p.s/*{h;m}", "p.s/m"));
    assertSameLimits(HEADER_FULL, makeLog("p.s/*{h}", "p.s/m"));
    assertSameLimits(MSG_FULL, makeLog("p.s/*{m}", "p.s/m"));
    assertSameLimits(HEADER_256, makeLog("p.s/*{h:256}", "p.s/m"));
    assertSameLimits(MSG_256, makeLog("p.s/*{m:256}", "p.s/m"));
    assertSameLimits(BOTH_256, makeLog("p.s/*{h:256;m:256}", "p.s/m"));
    assertSameLimits(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        makeLog("p.s/*{h;m:256}", "p.s/m"));
    assertSameLimits(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        makeLog("p.s/*{h:256;m}", "p.s/m"));
  }

  @Test
  public void configBinLog_service_absent() throws Exception {
    assertNull(makeLog("p.s/*", "p.other/m"));
  }

  @Test
  public void createLogFromOptionString() throws Exception {
    assertSameLimits(BOTH_FULL, makeLog(null));
    assertSameLimits(HEADER_FULL, makeLog("{h}"));
    assertSameLimits(MSG_FULL, makeLog("{m}"));
    assertSameLimits(HEADER_256, makeLog("{h:256}"));
    assertSameLimits(MSG_256, makeLog("{m:256}"));
    assertSameLimits(BOTH_256, makeLog("{h:256;m:256}"));
    assertSameLimits(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        makeLog("{h;m:256}"));
    assertSameLimits(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        makeLog("{h:256;m}"));
  }

  @Test
  public void createLogFromOptionString_malformed() throws Exception {
    assertNull(makeLog("bad"));
    assertNull(makeLog("{bad}"));
    assertNull(makeLog("{x;y}"));
    assertNull(makeLog("{h:abc}"));
    assertNull(makeLog("{2}"));
    assertNull(makeLog("{2;2}"));
    // The grammar specifies that if both h and m are present, h comes before m
    assertNull(makeLog("{m:123;h:123}"));
    // NumberFormatException
    assertNull(makeLog("{h:99999999999999}"));
  }

  @Test
  public void configBinLog_multiConfig_withGlobal() throws Exception {
    String configStr =
        "*{h},"
        + "package.both256/*{h:256;m:256},"
        + "package.service1/both128{h:128;m:128},"
        + "package.service2/method_messageOnly{m}";
    assertSameLimits(HEADER_FULL, makeLog(configStr, "otherpackage.service/method"));

    assertSameLimits(BOTH_256, makeLog(configStr, "package.both256/method1"));
    assertSameLimits(BOTH_256, makeLog(configStr, "package.both256/method2"));
    assertSameLimits(BOTH_256, makeLog(configStr, "package.both256/method3"));

    assertSameLimits(
        new Builder().header(128).msg(128).build(), makeLog(configStr, "package.service1/both128"));
    // the global config is in effect
    assertSameLimits(HEADER_FULL, makeLog(configStr, "package.service1/absent"));

    assertSameLimits(MSG_FULL, makeLog(configStr, "package.service2/method_messageOnly"));
    // the global config is in effect
    assertSameLimits(HEADER_FULL, makeLog(configStr, "package.service2/absent"));
  }

  @Test
  public void configBinLog_multiConfig_noGlobal() throws Exception {
    String configStr =
        "package.both256/*{h:256;m:256},"
        + "package.service1/both128{h:128;m:128},"
        + "package.service2/method_messageOnly{m}";
    assertNull(makeLog(configStr, "otherpackage.service/method"));

    assertSameLimits(BOTH_256, makeLog(configStr, "package.both256/method1"));
    assertSameLimits(BOTH_256, makeLog(configStr, "package.both256/method2"));
    assertSameLimits(BOTH_256, makeLog(configStr, "package.both256/method3"));

    assertSameLimits(
        new Builder().header(128).msg(128).build(), makeLog(configStr, "package.service1/both128"));
    // no global config in effect
    assertNull(makeLog(configStr, "package.service1/absent"));

    assertSameLimits(MSG_FULL, makeLog(configStr, "package.service2/method_messageOnly"));
    // no global config in effect
    assertNull(makeLog(configStr, "package.service2/absent"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_global() throws Exception {
    String configStr = "*{h},p.s/m,*{h:256}";
    // The duplicate
    assertSameLimits(HEADER_FULL, makeLog(configStr, "p.other1/m"));
    assertSameLimits(HEADER_FULL, makeLog(configStr, "p.other2/m"));
    // Other
    assertSameLimits(BOTH_FULL, makeLog(configStr, "p.s/m"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_service() throws Exception {
    String configStr = "p.s/*,*{h:256},p.s/*{h}";
    // The duplicate
    assertSameLimits(BOTH_FULL, makeLog(configStr, "p.s/m1"));
    assertSameLimits(BOTH_FULL, makeLog(configStr, "p.s/m2"));
    // Other
    assertSameLimits(HEADER_256, makeLog(configStr, "p.other1/m"));
    assertSameLimits(HEADER_256, makeLog(configStr, "p.other2/m"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_method() throws Exception {
    String configStr = "p.s/m,*{h:256},p.s/m{h}";
    // The duplicate
    assertSameLimits(BOTH_FULL, makeLog(configStr, "p.s/m"));
    // Other
    assertSameLimits(HEADER_256, makeLog(configStr, "p.other1/m"));
    assertSameLimits(HEADER_256, makeLog(configStr, "p.other2/m"));
  }

  @Test
  public void callIdToProto() {
    byte[] callId = new byte[] {
      0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
      0x19, 0x10, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f };
    assertEquals(
        Uint128
            .newBuilder()
            .setHigh(0x1112131415161718L)
            .setLow(0x19101a1b1c1d1e1fL)
            .build(),
        BinaryLog.callIdToProto(callId));

  }

  @Test
  public void callIdToProto_invalid_shorter_len() {
    try {
      BinaryLog.callIdToProto(new byte[14]);
      fail();
    } catch (IllegalArgumentException expected) {
      assertTrue(
          expected.getMessage().startsWith("can only convert from 16 byte input, actual length"));
    }
  }

  @Test
  public void callIdToProto_invalid_longer_len() {
    try {
      BinaryLog.callIdToProto(new byte[18]);
      fail();
    } catch (IllegalArgumentException expected) {
      assertTrue(
          expected.getMessage().startsWith("can only convert from 16 byte input, actual length"));
    }
  }

  @Test
  public void socketToProto_ipv4() throws Exception {
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    byte[] addressBytes = address.getAddress();
    byte[] portBytes = ByteBuffer.allocate(4).putInt(port).array();
    byte[] portUnsignedBytes = Arrays.copyOfRange(portBytes, 2, 4);
    assertEquals(
        Peer
            .newBuilder()
            .setPeerType(Peer.PeerType.PEER_IPV4)
            .setPeer(ByteString.copyFrom(Bytes.concat(addressBytes, portUnsignedBytes)))
            .build(),
        BinaryLog.socketToProto(socketAddress));
  }

  @Test
  public void socketToProto_ipv6() throws Exception {
    // this is a ipv6 link local address
    InetAddress address = InetAddress.getByName("fe:80:12:34:56:78:90:ab");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    byte[] addressBytes = address.getAddress();
    byte[] portBytes = ByteBuffer.allocate(4).putInt(port).array();
    byte[] portUnsignedBytes = Arrays.copyOfRange(portBytes, 2, 4);
    assertEquals(
        Peer
            .newBuilder()
            .setPeerType(Peer.PeerType.PEER_IPV6)
            .setPeer(ByteString.copyFrom(Bytes.concat(addressBytes, portUnsignedBytes)))
            .build(),
        BinaryLog.socketToProto(socketAddress));
  }

  @Test
  public void socketToProto_unix() throws Exception {
    String path = "/some/path";
    DomainSocketAddress socketAddress = new DomainSocketAddress(path);
    assertEquals(
        Peer
            .newBuilder()
            .setPeerType(Peer.PeerType.PEER_UNIX)
            .setPeer(ByteString.copyFrom(path.getBytes(US_ASCII)))
            .build(),
        BinaryLog.socketToProto(socketAddress)
    );
  }

  @Test
  public void socketToProto_unknown() throws Exception {
    SocketAddress unknownSocket = new SocketAddress() { };
    assertEquals(
        Peer.newBuilder()
            .setPeerType(PeerType.UNKNOWN_PEERTYPE)
            .setPeer(ByteString.copyFrom(unknownSocket.toString(), US_ASCII))
            .build(),
        BinaryLog.socketToProto(unknownSocket));
  }

  @Test
  public void metadataToProto() throws Exception {
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .addEntry(ENTRY_C)
            .build(),
        BinaryLog.metadataToProto(metadata, Integer.MAX_VALUE));
  }

  @Test
  public void metadataToProto_truncated() throws Exception {
    // 0 byte limit not enough for any metadata
    assertEquals(
        io.grpc.binarylog.Metadata.getDefaultInstance(),
        BinaryLog.metadataToProto(metadata, 0));
    // not enough bytes for first key value
    assertEquals(
        io.grpc.binarylog.Metadata.getDefaultInstance(),
        BinaryLog.metadataToProto(metadata, 9));
    // enough for first key value
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .build(),
        BinaryLog.metadataToProto(metadata, 10));
    // Test edge cases for >= 2 key values
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .build(),
        BinaryLog.metadataToProto(metadata, 19));
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .build(),
        BinaryLog.metadataToProto(metadata, 20));
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .build(),
        BinaryLog.metadataToProto(metadata, 29));
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .addEntry(ENTRY_C)
            .build(),
        BinaryLog.metadataToProto(metadata, 30));
  }

  @Test
  public void messageToProto() throws Exception {
    ByteBuffer bytes = ByteBuffer.wrap(
        "this is a long message: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA".getBytes(US_ASCII));
    int remains = bytes.remaining();
    Message message = BinaryLog.messageToProto(bytes, false, Integer.MAX_VALUE);
    assertEquals(
        Message
            .newBuilder()
            .setData(ByteString.copyFrom(bytes.duplicate()))
            .setFlags(0)
            .setLength(bytes.remaining())
            .build(),
        message);
    assertEquals(remains, bytes.remaining());
  }

  @Test
  public void messageToProto_offsetByteBuffer() throws Exception {
    String padding = "aaaaa";
    String body = "the actual message";
    String truncatedRemainder = "zzzz";
    ByteBuffer bytes = ByteBuffer.wrap(
        (padding + body + truncatedRemainder).getBytes(US_ASCII));
    bytes.position(padding.length());
    int remains = bytes.remaining();
    Message message = BinaryLog.messageToProto(bytes, false, body.length());
    assertEquals(
        Message
            .newBuilder()
            .setData(ByteString.copyFrom(body.getBytes(US_ASCII)))
            .setFlags(0)
            .setLength(body.length() + truncatedRemainder.length())
            .build(),
        message);
    assertEquals(remains, bytes.remaining());
  }

  @Test
  public void messageToProto_truncated() throws Exception {
    ByteBuffer bytes = ByteBuffer.wrap(
        "this is a long message: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA".getBytes(US_ASCII));
    assertEquals(
        Message
            .newBuilder()
            .setFlags(0)
            .setLength(bytes.remaining())
            .build(),
        BinaryLog.messageToProto(bytes, false, 0));

    int limit = 10;
    String truncatedMessage = "this is a ";
    assertEquals(
        Message
            .newBuilder()
            .setData(ByteString.copyFrom(truncatedMessage.getBytes(US_ASCII)))
            .setFlags(0)
            .setLength(bytes.remaining())
            .build(),
        BinaryLog.messageToProto(bytes, false, limit));
  }

  @Test
  public void toFlag() throws Exception {
    assertEquals(0, BinaryLog.flagsForMessage(IS_UNCOMPRESSED));
    assertEquals(1, BinaryLog.flagsForMessage(IS_COMPRESSED));
  }

  @Test
  public void logSendInitialMetadata_server() throws Exception {
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    binaryLog.logSendInitialMetadata(metadata, IS_SERVER, CALL_ID, socketAddress);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_INITIAL_METADATA)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setPeer(BinaryLog.socketToProto(socketAddress))
            .setMetadata(BinaryLog.metadataToProto(metadata, 10))
            .build());
  }

  @Test
  public void logSendInitialMetadata_client() throws Exception {
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    binaryLog.logSendInitialMetadata(metadata, IS_CLIENT, CALL_ID, socketAddress);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_INITIAL_METADATA)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setPeer(BinaryLog.socketToProto(socketAddress))
            .setMetadata(BinaryLog.metadataToProto(metadata, 10))
            .build());
  }

  @Test
  public void logRecvInitialMetadata_server() throws Exception {
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    binaryLog.logRecvInitialMetadata(metadata, IS_SERVER, CALL_ID, socketAddress);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_INITIAL_METADATA)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setPeer(BinaryLog.socketToProto(socketAddress))
            .setMetadata(BinaryLog.metadataToProto(metadata, 10))
            .build());
  }

  @Test
  public void logRecvInitialMetadata_client() throws Exception {
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    binaryLog.logRecvInitialMetadata(metadata, IS_CLIENT, CALL_ID, socketAddress);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_INITIAL_METADATA)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setPeer(BinaryLog.socketToProto(socketAddress))
            .setMetadata(BinaryLog.metadataToProto(metadata, 10))
            .build());
  }

  @Test
  public void logTrailingMetadata_server() throws Exception {
    binaryLog.logTrailingMetadata(metadata, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_TRAILING_METADATA)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMetadata(BinaryLog.metadataToProto(metadata, 10))
            .build());
  }

  @Test
  public void logTrailingMetadata_client() throws Exception {
    binaryLog.logTrailingMetadata(metadata, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_TRAILING_METADATA)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMetadata(BinaryLog.metadataToProto(metadata, 10))
            .build());
  }

  @Test
  public void logOutboundMessage_server() throws Exception {
    binaryLog.logOutboundMessage(message, IS_COMPRESSED, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMessage(BinaryLog.messageToProto(message, IS_COMPRESSED, MESSAGE_LIMIT))
            .build());

    binaryLog.logOutboundMessage(message, IS_UNCOMPRESSED, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMessage(BinaryLog.messageToProto(message, IS_UNCOMPRESSED, MESSAGE_LIMIT))
            .build());
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void logOutboundMessage_client() throws Exception {
    binaryLog.logOutboundMessage(message, IS_COMPRESSED, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMessage(BinaryLog.messageToProto(message, IS_COMPRESSED, MESSAGE_LIMIT))
            .build());

    binaryLog.logOutboundMessage(message, IS_UNCOMPRESSED, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMessage(BinaryLog.messageToProto(message, IS_UNCOMPRESSED, MESSAGE_LIMIT))
            .build());
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void logInboundMessage_server() throws Exception {
    binaryLog.logInboundMessage(message, IS_COMPRESSED, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMessage(BinaryLog.messageToProto(message, IS_COMPRESSED, MESSAGE_LIMIT))
            .build());

    binaryLog.logInboundMessage(message, IS_UNCOMPRESSED, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMessage(BinaryLog.messageToProto(message, IS_UNCOMPRESSED, MESSAGE_LIMIT))
            .build());
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void logInboundMessage_client() throws Exception {
    binaryLog.logInboundMessage(message, IS_COMPRESSED, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMessage(BinaryLog.messageToProto(message, IS_COMPRESSED, MESSAGE_LIMIT))
            .build());

    binaryLog.logInboundMessage(message, IS_UNCOMPRESSED, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinaryLog.callIdToProto(CALL_ID))
            .setMessage(BinaryLog.messageToProto(message, IS_UNCOMPRESSED, MESSAGE_LIMIT))
            .build());
    verifyNoMoreInteractions(sink);
  }

  /** A builder class to make unit test code more readable. */
  private static final class Builder {
    int maxHeaderBytes = 0;
    int maxMessageBytes = 0;

    Builder header(int bytes) {
      maxHeaderBytes = bytes;
      return this;
    }

    Builder msg(int bytes) {
      maxMessageBytes = bytes;
      return this;
    }

    BinaryLog build() {
      return new BinaryLog(mock(BinaryLogSink.class), maxHeaderBytes, maxMessageBytes);
    }
  }

  private static void assertSameLimits(BinaryLog a, BinaryLog b) {
    assertEquals(a.maxMessageBytes, b.maxMessageBytes);
    assertEquals(a.maxHeaderBytes, b.maxHeaderBytes);
  }

  private BinaryLog makeLog(String factoryConfigStr, String lookup) {
    return new BinaryLog.FactoryImpl(sink, factoryConfigStr).getLog(lookup);
  }

  private BinaryLog makeLog(String logConfigStr) {
    return FactoryImpl.createBinaryLog(sink, logConfigStr);
  }
}
