/*
 * Copyright 2017 The gRPC Authors
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

import static io.grpc.BinaryLogProvider.BYTEARRAY_MARSHALLER;
import static io.grpc.services.BinlogHelper.DUMMY_SOCKET;
import static io.grpc.services.BinlogHelper.getPeerSocket;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.primitives.Bytes;
import com.google.protobuf.ByteString;
import io.grpc.Attributes;
import io.grpc.BinaryLog.CallId;
import io.grpc.BinaryLogProvider;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.Grpc;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.Status;
import io.grpc.binarylog.GrpcLogEntry;
import io.grpc.binarylog.Message;
import io.grpc.binarylog.MetadataEntry;
import io.grpc.binarylog.Peer;
import io.grpc.binarylog.Peer.PeerType;
import io.grpc.binarylog.Uint128;
import io.grpc.internal.NoopClientCall;
import io.grpc.internal.NoopServerCall;
import io.grpc.services.BinlogHelper.FactoryImpl;
import io.grpc.services.BinlogHelper.SinkWriter;
import io.grpc.services.BinlogHelper.SinkWriterImpl;
import io.netty.channel.unix.DomainSocketAddress;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.nio.ByteBuffer;
import java.nio.charset.Charset;
import java.util.Arrays;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Tests for {@link BinlogHelper}. */
@RunWith(JUnit4.class)
public final class BinlogHelperTest {
  private static final Charset US_ASCII = Charset.forName("US-ASCII");
  private static final BinlogHelper HEADER_FULL = new Builder().header(Integer.MAX_VALUE).build();
  private static final BinlogHelper HEADER_256 = new Builder().header(256).build();
  private static final BinlogHelper MSG_FULL = new Builder().msg(Integer.MAX_VALUE).build();
  private static final BinlogHelper MSG_256 = new Builder().msg(256).build();
  private static final BinlogHelper BOTH_256 = new Builder().header(256).msg(256).build();
  private static final BinlogHelper BOTH_FULL =
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
  // TODO(zpencer): rename this to callId, since byte[] is mutable
  private static final CallId CALL_ID =
      new CallId(0x1112131415161718L, 0x19101a1b1c1d1e1fL);
  private static final int HEADER_LIMIT = 10;
  private static final int MESSAGE_LIMIT = Integer.MAX_VALUE;

  private final Metadata nonEmptyMetadata = new Metadata();
  private final BinaryLogSink sink = mock(BinaryLogSink.class);
  private final SinkWriter sinkWriterImpl =
      new SinkWriterImpl(sink, HEADER_LIMIT, MESSAGE_LIMIT);
  private final SinkWriter mockSinkWriter = mock(SinkWriter.class);
  private final byte[] message = new byte[100];
  private SocketAddress peer;

  @Before
  public void setUp() throws Exception {
    nonEmptyMetadata.put(KEY_A, DATA_A);
    nonEmptyMetadata.put(KEY_B, DATA_B);
    nonEmptyMetadata.put(KEY_C, DATA_C);
    peer = new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 1234);
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
    CallId callId = new CallId(0x1112131415161718L, 0x19101a1b1c1d1e1fL);
    assertEquals(
        Uint128
            .newBuilder()
            .setHigh(0x1112131415161718L)
            .setLow(0x19101a1b1c1d1e1fL)
            .build(),
        BinlogHelper.callIdToProto(callId));
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
        BinlogHelper.socketToProto(socketAddress));
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
        BinlogHelper.socketToProto(socketAddress));
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
        BinlogHelper.socketToProto(socketAddress)
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
        BinlogHelper.socketToProto(unknownSocket));
  }

  @Test
  public void metadataToProto_empty() throws Exception {
    assertEquals(
        io.grpc.binarylog.Metadata.getDefaultInstance(),
        BinlogHelper.metadataToProto(new Metadata(), Integer.MAX_VALUE));
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
        BinlogHelper.metadataToProto(nonEmptyMetadata, Integer.MAX_VALUE));
  }

  @Test
  public void metadataToProto_truncated() throws Exception {
    // 0 byte limit not enough for any metadata
    assertEquals(
        io.grpc.binarylog.Metadata.getDefaultInstance(),
        BinlogHelper.metadataToProto(nonEmptyMetadata, 0));
    // not enough bytes for first key value
    assertEquals(
        io.grpc.binarylog.Metadata.getDefaultInstance(),
        BinlogHelper.metadataToProto(nonEmptyMetadata, 9));
    // enough for first key value
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .build(),
        BinlogHelper.metadataToProto(nonEmptyMetadata, 10));
    // Test edge cases for >= 2 key values
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .build(),
        BinlogHelper.metadataToProto(nonEmptyMetadata, 19));
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .build(),
        BinlogHelper.metadataToProto(nonEmptyMetadata, 20));
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .build(),
        BinlogHelper.metadataToProto(nonEmptyMetadata, 29));
    assertEquals(
        io.grpc.binarylog.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .addEntry(ENTRY_C)
            .build(),
        BinlogHelper.metadataToProto(nonEmptyMetadata, 30));
  }

  @Test
  public void messageToProto() throws Exception {
    byte[] bytes
        = "this is a long message: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA".getBytes(US_ASCII);
    Message message = BinlogHelper.messageToProto(bytes, false, Integer.MAX_VALUE);
    assertEquals(
        Message
            .newBuilder()
            .setData(ByteString.copyFrom(bytes))
            .setFlags(0)
            .setLength(bytes.length)
            .build(),
        message);
  }

  @Test
  public void messageToProto_truncated() throws Exception {
    byte[] bytes
        = "this is a long message: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA".getBytes(US_ASCII);
    assertEquals(
        Message
            .newBuilder()
            .setFlags(0)
            .setLength(bytes.length)
            .build(),
        BinlogHelper.messageToProto(bytes, false, 0));

    int limit = 10;
    String truncatedMessage = "this is a ";
    assertEquals(
        Message
            .newBuilder()
            .setData(ByteString.copyFrom(truncatedMessage.getBytes(US_ASCII)))
            .setFlags(0)
            .setLength(bytes.length)
            .build(),
        BinlogHelper.messageToProto(bytes, false, limit));
  }

  @Test
  public void toFlag() throws Exception {
    assertEquals(0, BinlogHelper.flagsForMessage(IS_UNCOMPRESSED));
    assertEquals(1, BinlogHelper.flagsForMessage(IS_COMPRESSED));
  }

  @Test
  public void logSendInitialMetadata_server() throws Exception {
    sinkWriterImpl.logSendInitialMetadata(nonEmptyMetadata, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_INITIAL_METADATA)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMetadata(BinlogHelper.metadataToProto(nonEmptyMetadata, 10))
            .build());
  }

  @Test
  public void logSendInitialMetadata_client() throws Exception {
    sinkWriterImpl.logSendInitialMetadata(nonEmptyMetadata, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_INITIAL_METADATA)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMetadata(BinlogHelper.metadataToProto(nonEmptyMetadata, 10))
            .build());
  }

  @Test
  public void logRecvInitialMetadata_server() throws Exception {
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    sinkWriterImpl.logRecvInitialMetadata(nonEmptyMetadata, IS_SERVER, CALL_ID, socketAddress);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_INITIAL_METADATA)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setPeer(BinlogHelper.socketToProto(socketAddress))
            .setMetadata(BinlogHelper.metadataToProto(nonEmptyMetadata, 10))
            .build());
  }

  @Test
  public void logRecvInitialMetadata_client() throws Exception {
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    sinkWriterImpl.logRecvInitialMetadata(nonEmptyMetadata, IS_CLIENT, CALL_ID, socketAddress);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_INITIAL_METADATA)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setPeer(BinlogHelper.socketToProto(socketAddress))
            .setMetadata(BinlogHelper.metadataToProto(nonEmptyMetadata, 10))
            .build());
  }

  @Test
  public void logTrailingMetadata_server() throws Exception {
    sinkWriterImpl.logTrailingMetadata(nonEmptyMetadata, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_TRAILING_METADATA)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMetadata(BinlogHelper.metadataToProto(nonEmptyMetadata, 10))
            .build());
  }

  @Test
  public void logTrailingMetadata_client() throws Exception {
    sinkWriterImpl.logTrailingMetadata(nonEmptyMetadata, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_TRAILING_METADATA)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMetadata(BinlogHelper.metadataToProto(nonEmptyMetadata, 10))
            .build());
  }

  @Test
  public void logOutboundMessage_server() throws Exception {
    sinkWriterImpl.logOutboundMessage(
        BYTEARRAY_MARSHALLER, message, IS_COMPRESSED, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMessage(BinlogHelper.messageToProto(message, IS_COMPRESSED, MESSAGE_LIMIT))
            .build());

    sinkWriterImpl.logOutboundMessage(
        BYTEARRAY_MARSHALLER, message, IS_UNCOMPRESSED, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMessage(
                BinlogHelper.messageToProto(message, IS_UNCOMPRESSED, MESSAGE_LIMIT))
            .build());
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void logOutboundMessage_client() throws Exception {
    sinkWriterImpl.logOutboundMessage(
        BYTEARRAY_MARSHALLER, message, IS_COMPRESSED, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMessage(BinlogHelper.messageToProto(message, IS_COMPRESSED, MESSAGE_LIMIT))
            .build());

    sinkWriterImpl.logOutboundMessage(
        BYTEARRAY_MARSHALLER, message, IS_UNCOMPRESSED, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.SEND_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMessage(
                BinlogHelper.messageToProto(message, IS_UNCOMPRESSED, MESSAGE_LIMIT))
            .build());
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void logInboundMessage_server() throws Exception {
    sinkWriterImpl.logInboundMessage(
        BYTEARRAY_MARSHALLER, message, IS_COMPRESSED, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMessage(BinlogHelper.messageToProto(message, IS_COMPRESSED, MESSAGE_LIMIT))
            .build());

    sinkWriterImpl.logInboundMessage(
        BYTEARRAY_MARSHALLER, message, IS_UNCOMPRESSED, IS_SERVER, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.SERVER)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMessage(
                BinlogHelper.messageToProto(message, IS_UNCOMPRESSED, MESSAGE_LIMIT))
            .build());
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void logInboundMessage_client() throws Exception {
    sinkWriterImpl.logInboundMessage(
        BYTEARRAY_MARSHALLER, message, IS_COMPRESSED, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMessage(BinlogHelper.messageToProto(message, IS_COMPRESSED, MESSAGE_LIMIT))
            .build());

    sinkWriterImpl.logInboundMessage(
        BYTEARRAY_MARSHALLER, message, IS_UNCOMPRESSED, IS_CLIENT, CALL_ID);
    verify(sink).write(
        GrpcLogEntry
            .newBuilder()
            .setType(GrpcLogEntry.Type.RECV_MESSAGE)
            .setLogger(GrpcLogEntry.Logger.CLIENT)
            .setCallId(BinlogHelper.callIdToProto(CALL_ID))
            .setMessage(
                BinlogHelper.messageToProto(message, IS_UNCOMPRESSED, MESSAGE_LIMIT))
            .build());
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void getPeerSocketTest() {
    assertSame(DUMMY_SOCKET, getPeerSocket(Attributes.EMPTY));
    assertSame(
        peer,
        getPeerSocket(Attributes.newBuilder().set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, peer).build()));
  }

  @Test
  @SuppressWarnings({"rawtypes", "unchecked"})
  public void clientInterceptor() throws Exception {
    final AtomicReference<ClientCall.Listener> interceptedListener =
        new AtomicReference<ClientCall.Listener>();
    // capture these manually because ClientCall can not be mocked
    final AtomicReference<Metadata> actualClientInitial = new AtomicReference<Metadata>();
    final AtomicReference<Object> actualRequest = new AtomicReference<Object>();

    Channel channel = new Channel() {
      @Override
      public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
          MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions) {
        return new NoopClientCall<RequestT, ResponseT>() {
          @Override
          public void start(Listener<ResponseT> responseListener, Metadata headers) {
            interceptedListener.set(responseListener);
            actualClientInitial.set(headers);
          }

          @Override
          public void sendMessage(RequestT message) {
            actualRequest.set(message);
          }

          @Override
          public Attributes getAttributes() {
            return Attributes.newBuilder().set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, peer).build();
          }
        };
      }

      @Override
      public String authority() {
        throw new UnsupportedOperationException();
      }
    };

    ClientCall.Listener<byte[]> mockListener = mock(ClientCall.Listener.class);

    MethodDescriptor<byte[], byte[]> method =
        MethodDescriptor.<byte[], byte[]>newBuilder()
            .setType(MethodType.UNKNOWN)
            .setFullMethodName("service/method")
            .setRequestMarshaller(BYTEARRAY_MARSHALLER)
            .setResponseMarshaller(BYTEARRAY_MARSHALLER)
            .build();
    ClientCall<byte[], byte[]> interceptedCall =
        new BinlogHelper(mockSinkWriter)
            .getClientInterceptor(CALL_ID)
            .interceptCall(
                method,
                CallOptions.DEFAULT.withOption(
                    BinaryLogProvider.CLIENT_CALL_ID_CALLOPTION_KEY, CALL_ID),
                channel);

    // send initial metadata
    {
      Metadata clientInitial = new Metadata();
      interceptedCall.start(mockListener, clientInitial);
      verify(mockSinkWriter).logSendInitialMetadata(
          same(clientInitial),
          eq(IS_CLIENT),
          same(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(clientInitial, actualClientInitial.get());
    }

    // receive initial metadata
    {
      Metadata serverInitial = new Metadata();
      interceptedListener.get().onHeaders(serverInitial);
      verify(mockSinkWriter).logRecvInitialMetadata(same(serverInitial),
          eq(IS_CLIENT),
          same(CALL_ID),
          same(peer));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onHeaders(same(serverInitial));
    }

    // send request
    {
      byte[] request = "this is a request".getBytes(US_ASCII);
      interceptedCall.sendMessage(request);
      verify(mockSinkWriter).logOutboundMessage(
          same(BYTEARRAY_MARSHALLER),
          same(request),
          eq(BinlogHelper.DUMMY_IS_COMPRESSED),
          eq(IS_CLIENT),
          same(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(request, actualRequest.get());
    }

    // receive response
    {
      byte[] response = "this is a response".getBytes(US_ASCII);
      interceptedListener.get().onMessage(response);
      verify(mockSinkWriter).logInboundMessage(
          same(BYTEARRAY_MARSHALLER),
          eq(response),
          eq(BinlogHelper.DUMMY_IS_COMPRESSED),
          eq(IS_CLIENT),
          same(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onMessage(same(response));
    }

    // receive trailers
    {
      Status status = Status.INTERNAL.withDescription("some description");
      Metadata trailers = new Metadata();

      interceptedListener.get().onClose(status, trailers);
      verify(mockSinkWriter).logTrailingMetadata(
          same(trailers),
          eq(IS_CLIENT),
          same(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onClose(same(status), same(trailers));
    }
  }

  @Test
  @SuppressWarnings({"rawtypes", "unchecked"})
  public void serverInterceptor() throws Exception {
    final AtomicReference<ServerCall> interceptedCall =
        new AtomicReference<ServerCall>();
    ServerCall.Listener<byte[]> capturedListener;
    final ServerCall.Listener mockListener = mock(ServerCall.Listener.class);
    // capture these manually because ServerCall can not be mocked
    final AtomicReference<Metadata> actualServerInitial = new AtomicReference<Metadata>();
    final AtomicReference<byte[]> actualResponse = new AtomicReference<byte[]>();
    final AtomicReference<Status> actualStatus = new AtomicReference<Status>();
    final AtomicReference<Metadata> actualTrailers = new AtomicReference<Metadata>();

    // begin call and receive initial metadata
    {
      Metadata clientInitial = new Metadata();
      final MethodDescriptor<byte[], byte[]> method =
          MethodDescriptor.<byte[], byte[]>newBuilder()
              .setType(MethodType.UNKNOWN)
              .setFullMethodName("service/method")
              .setRequestMarshaller(BYTEARRAY_MARSHALLER)
              .setResponseMarshaller(BYTEARRAY_MARSHALLER)
              .build();
      capturedListener =
          new BinlogHelper(mockSinkWriter)
              .getServerInterceptor(CALL_ID)
              .interceptCall(
                  new NoopServerCall<byte[], byte[]>() {
                    @Override
                    public void sendHeaders(Metadata headers) {
                      actualServerInitial.set(headers);
                    }

                    @Override
                    public void sendMessage(byte[] message) {
                      actualResponse.set(message);
                    }

                    @Override
                    public void close(Status status, Metadata trailers) {
                      actualStatus.set(status);
                      actualTrailers.set(trailers);
                    }

                    @Override
                    public MethodDescriptor<byte[], byte[]> getMethodDescriptor() {
                      return method;
                    }

                    @Override
                    public Attributes getAttributes() {
                      return Attributes
                          .newBuilder()
                          .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, peer)
                          .build();
                    }
                  },
                  clientInitial,
                  new ServerCallHandler<byte[], byte[]>() {
                    @Override
                    public ServerCall.Listener<byte[]> startCall(
                        ServerCall<byte[], byte[]> call,
                        Metadata headers) {
                      interceptedCall.set(call);
                      return mockListener;
                    }
                  });
      verify(mockSinkWriter).logRecvInitialMetadata(
          same(clientInitial),
          eq(IS_SERVER),
          same(CALL_ID),
          same(peer));
      verifyNoMoreInteractions(mockSinkWriter);
    }

    // send initial metadata
    {
      Metadata serverInital = new Metadata();
      interceptedCall.get().sendHeaders(serverInital);
      verify(mockSinkWriter).logSendInitialMetadata(
          same(serverInital),
          eq(IS_SERVER),
          same(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(serverInital, actualServerInitial.get());
    }

    // receive request
    {
      byte[] request = "this is a request".getBytes(US_ASCII);
      capturedListener.onMessage(request);
      verify(mockSinkWriter).logInboundMessage(
          same(BYTEARRAY_MARSHALLER),
          same(request),
          eq(BinlogHelper.DUMMY_IS_COMPRESSED),
          eq(IS_SERVER),
          same(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onMessage(same(request));
    }

    // send response
    {
      byte[] response = "this is a response".getBytes(US_ASCII);
      interceptedCall.get().sendMessage(response);
      verify(mockSinkWriter).logOutboundMessage(
          same(BYTEARRAY_MARSHALLER),
          same(response),
          eq(BinlogHelper.DUMMY_IS_COMPRESSED),
          eq(IS_SERVER),
          same(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(response, actualResponse.get());
    }

    // send trailers
    {
      Status status = Status.INTERNAL.withDescription("some description");
      Metadata trailers = new Metadata();
      interceptedCall.get().close(status, trailers);
      verify(mockSinkWriter).logTrailingMetadata(
          same(trailers),
          eq(IS_SERVER),
          same(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(status, actualStatus.get());
      assertSame(trailers, actualTrailers.get());
    }
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

    BinlogHelper build() {
      return new BinlogHelper(
          new SinkWriterImpl(mock(BinaryLogSink.class), maxHeaderBytes, maxMessageBytes));
    }
  }

  private static void assertSameLimits(BinlogHelper a, BinlogHelper b) {
    assertEquals(a.writer.getMaxMessageBytes(), b.writer.getMaxMessageBytes());
    assertEquals(a.writer.getMaxHeaderBytes(), b.writer.getMaxHeaderBytes());
  }

  private BinlogHelper makeLog(String factoryConfigStr, String lookup) {
    return new BinlogHelper.FactoryImpl(sink, factoryConfigStr).getLog(lookup);
  }

  private BinlogHelper makeLog(String logConfigStr) {
    return FactoryImpl.createBinaryLog(sink, logConfigStr);
  }
}
