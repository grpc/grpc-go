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

import com.google.common.primitives.Bytes;
import com.google.protobuf.ByteString;
import io.grpc.Metadata;
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
  private static final boolean IS_COMPRESSED = true;
  private static final boolean IS_UNCOMPRESSED = false;

  private final Metadata metadata = new Metadata();

  @Before
  public void setUp() throws Exception {
    metadata.put(KEY_A, DATA_A);
    metadata.put(KEY_B, DATA_B);
    metadata.put(KEY_C, DATA_C);
  }

  @Test
  public void configBinLog_global() throws Exception {
    assertEquals(BOTH_FULL, new FactoryImpl("*").getLog("p.s/m"));
    assertEquals(BOTH_FULL, new FactoryImpl("*{h;m}").getLog("p.s/m"));
    assertEquals(HEADER_FULL, new FactoryImpl("*{h}").getLog("p.s/m"));
    assertEquals(MSG_FULL, new FactoryImpl("*{m}").getLog("p.s/m"));
    assertEquals(HEADER_256, new FactoryImpl("*{h:256}").getLog("p.s/m"));
    assertEquals(MSG_256, new FactoryImpl("*{m:256}").getLog("p.s/m"));
    assertEquals(BOTH_256, new FactoryImpl("*{h:256;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        new FactoryImpl("*{h;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        new FactoryImpl("*{h:256;m}").getLog("p.s/m"));
  }

  @Test
  public void configBinLog_method() throws Exception {
    assertEquals(BOTH_FULL, new FactoryImpl("p.s/m").getLog("p.s/m"));
    assertEquals(BOTH_FULL, new FactoryImpl("p.s/m{h;m}").getLog("p.s/m"));
    assertEquals(HEADER_FULL, new FactoryImpl("p.s/m{h}").getLog("p.s/m"));
    assertEquals(MSG_FULL, new FactoryImpl("p.s/m{m}").getLog("p.s/m"));
    assertEquals(HEADER_256, new FactoryImpl("p.s/m{h:256}").getLog("p.s/m"));
    assertEquals(MSG_256, new FactoryImpl("p.s/m{m:256}").getLog("p.s/m"));
    assertEquals(BOTH_256, new FactoryImpl("p.s/m{h:256;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        new FactoryImpl("p.s/m{h;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        new FactoryImpl("p.s/m{h:256;m}").getLog("p.s/m"));
  }

  @Test
  public void configBinLog_method_absent() throws Exception {
    assertNull(new FactoryImpl("p.s/m").getLog("p.s/absent"));
  }

  @Test
  public void configBinLog_service() throws Exception {
    assertEquals(BOTH_FULL, new FactoryImpl("p.s/*").getLog("p.s/m"));
    assertEquals(BOTH_FULL, new FactoryImpl("p.s/*{h;m}").getLog("p.s/m"));
    assertEquals(HEADER_FULL, new FactoryImpl("p.s/*{h}").getLog("p.s/m"));
    assertEquals(MSG_FULL, new FactoryImpl("p.s/*{m}").getLog("p.s/m"));
    assertEquals(HEADER_256, new FactoryImpl("p.s/*{h:256}").getLog("p.s/m"));
    assertEquals(MSG_256, new FactoryImpl("p.s/*{m:256}").getLog("p.s/m"));
    assertEquals(BOTH_256, new FactoryImpl("p.s/*{h:256;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        new FactoryImpl("p.s/*{h;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        new FactoryImpl("p.s/*{h:256;m}").getLog("p.s/m"));
  }

  @Test
  public void configBinLog_service_absent() throws Exception {
    assertNull(new FactoryImpl("p.s/*").getLog("p.other/m"));
  }

  @Test
  public void createLogFromOptionString() throws Exception {
    assertEquals(BOTH_FULL, FactoryImpl.createBinaryLog(/*logConfig=*/ null));
    assertEquals(HEADER_FULL, FactoryImpl.createBinaryLog("{h}"));
    assertEquals(MSG_FULL, FactoryImpl.createBinaryLog("{m}"));
    assertEquals(HEADER_256, FactoryImpl.createBinaryLog("{h:256}"));
    assertEquals(MSG_256, FactoryImpl.createBinaryLog("{m:256}"));
    assertEquals(BOTH_256, FactoryImpl.createBinaryLog("{h:256;m:256}"));
    assertEquals(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        FactoryImpl.createBinaryLog("{h;m:256}"));
    assertEquals(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        FactoryImpl.createBinaryLog("{h:256;m}"));
  }

  @Test
  public void createLogFromOptionString_malformed() throws Exception {
    assertNull(FactoryImpl.createBinaryLog("bad"));
    assertNull(FactoryImpl.createBinaryLog("{bad}"));
    assertNull(FactoryImpl.createBinaryLog("{x;y}"));
    assertNull(FactoryImpl.createBinaryLog("{h:abc}"));
    assertNull(FactoryImpl.createBinaryLog("{2}"));
    assertNull(FactoryImpl.createBinaryLog("{2;2}"));
    // The grammar specifies that if both h and m are present, h comes before m
    assertNull(FactoryImpl.createBinaryLog("{m:123;h:123}"));
    // NumberFormatException
    assertNull(FactoryImpl.createBinaryLog("{h:99999999999999}"));
  }

  @Test
  public void configBinLog_multiConfig_withGlobal() throws Exception {
    FactoryImpl factory = new FactoryImpl(
        "*{h},"
        + "package.both256/*{h:256;m:256},"
        + "package.service1/both128{h:128;m:128},"
        + "package.service2/method_messageOnly{m}");
    assertEquals(HEADER_FULL, factory.getLog("otherpackage.service/method"));

    assertEquals(BOTH_256, factory.getLog("package.both256/method1"));
    assertEquals(BOTH_256, factory.getLog("package.both256/method2"));
    assertEquals(BOTH_256, factory.getLog("package.both256/method3"));

    assertEquals(
        new Builder().header(128).msg(128).build(), factory.getLog("package.service1/both128"));
    // the global config is in effect
    assertEquals(HEADER_FULL, factory.getLog("package.service1/absent"));

    assertEquals(MSG_FULL, factory.getLog("package.service2/method_messageOnly"));
    // the global config is in effect
    assertEquals(HEADER_FULL, factory.getLog("package.service2/absent"));
  }

  @Test
  public void configBinLog_multiConfig_noGlobal() throws Exception {
    FactoryImpl factory = new FactoryImpl(
        "package.both256/*{h:256;m:256},"
        + "package.service1/both128{h:128;m:128},"
        + "package.service2/method_messageOnly{m}");
    assertNull(factory.getLog("otherpackage.service/method"));

    assertEquals(BOTH_256, factory.getLog("package.both256/method1"));
    assertEquals(BOTH_256, factory.getLog("package.both256/method2"));
    assertEquals(BOTH_256, factory.getLog("package.both256/method3"));

    assertEquals(
        new Builder().header(128).msg(128).build(), factory.getLog("package.service1/both128"));
    // no global config in effect
    assertNull(factory.getLog("package.service1/absent"));

    assertEquals(MSG_FULL, factory.getLog("package.service2/method_messageOnly"));
    // no global config in effect
    assertNull(factory.getLog("package.service2/absent"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_global() throws Exception {
    FactoryImpl factory = new FactoryImpl("*{h},p.s/m,*{h:256}");
    // The duplicate
    assertEquals(HEADER_FULL, factory.getLog("p.other1/m"));
    assertEquals(HEADER_FULL, factory.getLog("p.other2/m"));
    // Other
    assertEquals(BOTH_FULL, factory.getLog("p.s/m"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_service() throws Exception {
    FactoryImpl factory = new FactoryImpl("p.s/*,*{h:256},p.s/*{h}");
    // The duplicate
    assertEquals(BOTH_FULL, factory.getLog("p.s/m1"));
    assertEquals(BOTH_FULL, factory.getLog("p.s/m2"));
    // Other
    assertEquals(HEADER_256, factory.getLog("p.other1/m"));
    assertEquals(HEADER_256, factory.getLog("p.other2/m"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_method() throws Exception {
    FactoryImpl factory = new FactoryImpl("p.s/m,*{h:256},p.s/m{h}");
    // The duplicate
    assertEquals(BOTH_FULL, factory.getLog("p.s/m"));
    // Other
    assertEquals(HEADER_256, factory.getLog("p.other1/m"));
    assertEquals(HEADER_256, factory.getLog("p.other2/m"));
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
    byte[] bytes = "this is a long message: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
        .getBytes(US_ASCII);
    Message message = BinaryLog.messageToProto(bytes, false, Integer.MAX_VALUE);
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
    byte[] bytes = "this is a long message: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
        .getBytes(US_ASCII);
    assertEquals(
        Message
            .newBuilder()
            .setFlags(0)
            .setLength(bytes.length)
            .build(),
        BinaryLog.messageToProto(bytes, false, 0));

    int limit = 10;
    assertEquals(
        Message
            .newBuilder()
            .setData(ByteString.copyFrom(bytes, 0, limit))
            .setFlags(0)
            .setLength(bytes.length)
            .build(),
        BinaryLog.messageToProto(bytes, false, limit));
  }

  @Test
  public void toFlag() throws Exception {
    assertEquals(0, BinaryLog.flagsForMessage(IS_UNCOMPRESSED));
    assertEquals(1, BinaryLog.flagsForMessage(IS_COMPRESSED));
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
      return new BinaryLog(maxHeaderBytes, maxMessageBytes);
    }
  }
}
