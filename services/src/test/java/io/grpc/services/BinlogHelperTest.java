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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.services.BinaryLogProvider.BYTEARRAY_MARSHALLER;
import static io.grpc.services.BinlogHelper.createMetadataProto;
import static io.grpc.services.BinlogHelper.getPeerSocket;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.ArgumentMatchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.collect.Iterables;
import com.google.common.util.concurrent.SettableFuture;
import com.google.protobuf.Any;
import com.google.protobuf.ByteString;
import com.google.protobuf.Duration;
import com.google.protobuf.StringValue;
import com.google.protobuf.Timestamp;
import com.google.protobuf.util.Durations;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.Context;
import io.grpc.Deadline;
import io.grpc.Grpc;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.binarylog.v1.Address;
import io.grpc.binarylog.v1.Address.Type;
import io.grpc.binarylog.v1.ClientHeader;
import io.grpc.binarylog.v1.GrpcLogEntry;
import io.grpc.binarylog.v1.GrpcLogEntry.EventType;
import io.grpc.binarylog.v1.GrpcLogEntry.Logger;
import io.grpc.binarylog.v1.Message;
import io.grpc.binarylog.v1.MetadataEntry;
import io.grpc.binarylog.v1.ServerHeader;
import io.grpc.binarylog.v1.Trailer;
import io.grpc.internal.NoopClientCall;
import io.grpc.internal.NoopServerCall;
import io.grpc.protobuf.StatusProto;
import io.grpc.services.BinlogHelper.FactoryImpl;
import io.grpc.services.BinlogHelper.MaybeTruncated;
import io.grpc.services.BinlogHelper.SinkWriter;
import io.grpc.services.BinlogHelper.SinkWriterImpl;
import io.grpc.services.BinlogHelper.TimeProvider;
import io.netty.channel.unix.DomainSocketAddress;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.nio.charset.Charset;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.AdditionalMatchers;
import org.mockito.ArgumentCaptor;
import org.mockito.ArgumentMatchers;

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
            .setKey(KEY_A.name())
            .setValue(ByteString.copyFrom(DATA_A.getBytes(US_ASCII)))
            .build();
  private static final MetadataEntry ENTRY_B =
        MetadataEntry
            .newBuilder()
            .setKey(KEY_B.name())
            .setValue(ByteString.copyFrom(DATA_B.getBytes(US_ASCII)))
            .build();
  private static final MetadataEntry ENTRY_C =
        MetadataEntry
            .newBuilder()
            .setKey(KEY_C.name())
            .setValue(ByteString.copyFrom(DATA_C.getBytes(US_ASCII)))
            .build();
  private static final long CALL_ID = 0x1112131415161718L;
  private static final int HEADER_LIMIT = 10;
  private static final int MESSAGE_LIMIT = Integer.MAX_VALUE;

  private final Metadata nonEmptyMetadata = new Metadata();
  private final BinaryLogSink sink = mock(BinaryLogSink.class);
  private final Timestamp timestamp
      = Timestamp.newBuilder().setSeconds(9876).setNanos(54321).build();
  private final BinlogHelper.TimeProvider timeProvider = new TimeProvider() {
    @Override
    public long currentTimeNanos() {
      return TimeUnit.SECONDS.toNanos(9876) + 54321;
    }
  };
  private final SinkWriter sinkWriterImpl =
      new SinkWriterImpl(
          sink,
          timeProvider,
          HEADER_LIMIT,
          MESSAGE_LIMIT);
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
    assertSameLimits(BOTH_FULL, makeOptions(null));
    assertSameLimits(HEADER_FULL, makeOptions("h"));
    assertSameLimits(MSG_FULL, makeOptions("m"));
    assertSameLimits(HEADER_256, makeOptions("h:256"));
    assertSameLimits(MSG_256, makeOptions("m:256"));
    assertSameLimits(BOTH_256, makeOptions("h:256;m:256"));
    assertSameLimits(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        makeOptions("h;m:256"));
    assertSameLimits(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        makeOptions("h:256;m"));
  }

  private void assertIllegalPatternDetected(String perSvcOrMethodConfig) {
    try {
      FactoryImpl.createBinaryLog(sink, perSvcOrMethodConfig);
      fail();
    } catch (IllegalArgumentException expected) {
      assertThat(expected).hasMessageThat().startsWith("Illegal log config pattern");
    }
  }

  @Test
  public void badFactoryConfigStrDetected() throws Exception {
    try {
      new FactoryImpl(sink, "obviouslybad{");
      fail();
    } catch (IllegalArgumentException expected) {
      assertThat(expected).hasMessageThat().startsWith("Illegal log config pattern");
    }
  }

  @Test
  public void badFactoryConfigStrDetected_empty() throws Exception {
    try {
      new FactoryImpl(sink, "*,");
      fail();
    } catch (IllegalArgumentException expected) {
      assertThat(expected).hasMessageThat().startsWith("Illegal log config pattern");
    }
  }

  @Test
  public void createLogFromOptionString_malformed() throws Exception {
    assertIllegalPatternDetected("");
    assertIllegalPatternDetected("bad");
    assertIllegalPatternDetected("mad");
    assertIllegalPatternDetected("x;y");
    assertIllegalPatternDetected("h:abc");
    assertIllegalPatternDetected("h:1e8");
    assertIllegalPatternDetected("2");
    assertIllegalPatternDetected("2;2");
    // The grammar specifies that if both h and m are present, h comes before m
    assertIllegalPatternDetected("m:123;h:123");
    // NumberFormatException
    assertIllegalPatternDetected("h:99999999999999");
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
  public void configBinLog_blacklist() {
    assertNull(makeLog("*,-p.s/blacklisted", "p.s/blacklisted"));
    assertNull(makeLog("-p.s/blacklisted,*", "p.s/blacklisted"));
    assertNotNull(makeLog("-p.s/method,*", "p.s/allowed"));

    assertNull(makeLog("p.s/*,-p.s/blacklisted", "p.s/blacklisted"));
    assertNull(makeLog("-p.s/blacklisted,p.s/*", "p.s/blacklisted"));
    assertNotNull(makeLog("-p.s/blacklisted,p.s/*", "p.s/allowed"));
  }

  private void assertDuplicatelPatternDetected(String factoryConfigStr) {
    try {
      new BinlogHelper.FactoryImpl(sink, factoryConfigStr);
      fail();
    } catch (IllegalStateException expected) {
      assertThat(expected).hasMessageThat().startsWith("Duplicate entry");
    }
  }

  @Test
  public void configBinLog_duplicates_global() throws Exception {
    assertDuplicatelPatternDetected("*{h},*{h:256}");
  }

  @Test
  public void configBinLog_duplicates_service() throws Exception {
    assertDuplicatelPatternDetected("p.s/*,p.s/*{h}");

  }

  @Test
  public void configBinLog_duplicates_method() throws Exception {
    assertDuplicatelPatternDetected("p.s/*,p.s/*{h:1;m:2}");
    assertDuplicatelPatternDetected("p.s/m,-p.s/m");
    assertDuplicatelPatternDetected("-p.s/m,p.s/m");
    assertDuplicatelPatternDetected("-p.s/m,-p.s/m");
  }

  @Test
  public void socketToProto_ipv4() throws Exception {
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    assertEquals(
        Address
            .newBuilder()
            .setType(Type.TYPE_IPV4)
            .setAddress("127.0.0.1")
            .setIpPort(12345)
            .build(),
        BinlogHelper.socketToProto(socketAddress));
  }

  @Test
  public void socketToProto_ipv6() throws Exception {
    // this is a ipv6 link local address
    InetAddress address = InetAddress.getByName("2001:db8:0:0:0:0:2:1");
    int port = 12345;
    InetSocketAddress socketAddress = new InetSocketAddress(address, port);
    assertEquals(
        Address
            .newBuilder()
            .setType(Type.TYPE_IPV6)
            .setAddress("2001:db8::2:1") // RFC 5952 section 4: ipv6 canonical form required
            .setIpPort(12345)
            .build(),
        BinlogHelper.socketToProto(socketAddress));
  }

  @Test
  public void socketToProto_unix() throws Exception {
    String path = "/some/path";
    DomainSocketAddress socketAddress = new DomainSocketAddress(path);
    assertEquals(
        Address
            .newBuilder()
            .setType(Type.TYPE_UNIX)
            .setAddress("/some/path")
            .build(),
        BinlogHelper.socketToProto(socketAddress)
    );
  }

  @Test
  public void socketToProto_unknown() throws Exception {
    SocketAddress unknownSocket = new SocketAddress() {
      @Override
      public String toString() {
        return "some-socket-address";
      }
    };
    assertEquals(
        Address.newBuilder()
            .setType(Type.TYPE_UNKNOWN)
            .setAddress("some-socket-address")
            .build(),
        BinlogHelper.socketToProto(unknownSocket));
  }

  @Test
  public void metadataToProto_empty() throws Exception {
    assertEquals(
        GrpcLogEntry.newBuilder()
            .setType(EventType.EVENT_TYPE_CLIENT_HEADER)
            .setClientHeader(
                ClientHeader.newBuilder().setMetadata(
                    io.grpc.binarylog.v1.Metadata.getDefaultInstance()))
            .build(),
        metadataToProtoTestHelper(
            EventType.EVENT_TYPE_CLIENT_HEADER, new Metadata(), Integer.MAX_VALUE));
  }

  @Test
  public void metadataToProto() throws Exception {
    assertEquals(
        GrpcLogEntry.newBuilder()
            .setType(EventType.EVENT_TYPE_CLIENT_HEADER)
            .setClientHeader(
                ClientHeader.newBuilder().setMetadata(
                io.grpc.binarylog.v1.Metadata
                    .newBuilder()
                    .addEntry(ENTRY_A)
                    .addEntry(ENTRY_B)
                    .addEntry(ENTRY_C)
                    .build()))
            .build(),
        metadataToProtoTestHelper(
            EventType.EVENT_TYPE_CLIENT_HEADER, nonEmptyMetadata, Integer.MAX_VALUE));
  }

  @Test
  public void metadataToProto_setsTruncated() throws Exception {
    assertTrue(BinlogHelper.createMetadataProto(nonEmptyMetadata, 0).truncated);
  }

  @Test
  public void metadataToProto_truncated() throws Exception {
    // 0 byte limit not enough for any metadata
    assertEquals(
        io.grpc.binarylog.v1.Metadata.getDefaultInstance(),
        BinlogHelper.createMetadataProto(nonEmptyMetadata, 0).proto.build());
    // not enough bytes for first key value
    assertEquals(
        io.grpc.binarylog.v1.Metadata.getDefaultInstance(),
        BinlogHelper.createMetadataProto(nonEmptyMetadata, 9).proto.build());
    // enough for first key value
    assertEquals(
        io.grpc.binarylog.v1.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .build(),
        BinlogHelper.createMetadataProto(nonEmptyMetadata, 10).proto.build());
    // Test edge cases for >= 2 key values
    assertEquals(
        io.grpc.binarylog.v1.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .build(),
        BinlogHelper.createMetadataProto(nonEmptyMetadata, 19).proto.build());
    assertEquals(
        io.grpc.binarylog.v1.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .build(),
        BinlogHelper.createMetadataProto(nonEmptyMetadata, 20).proto.build());
    assertEquals(
        io.grpc.binarylog.v1.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .build(),
        BinlogHelper.createMetadataProto(nonEmptyMetadata, 29).proto.build());

    // not truncated: enough for all keys
    assertEquals(
        io.grpc.binarylog.v1.Metadata
            .newBuilder()
            .addEntry(ENTRY_A)
            .addEntry(ENTRY_B)
            .addEntry(ENTRY_C)
            .build(),
        BinlogHelper.createMetadataProto(nonEmptyMetadata, 30).proto.build());
  }

  @Test
  public void messageToProto() throws Exception {
    byte[] bytes
        = "this is a long message: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA".getBytes(US_ASCII);
    assertEquals(
        GrpcLogEntry.newBuilder()
            .setMessage(
                Message
                    .newBuilder()
                    .setData(ByteString.copyFrom(bytes))
                    .setLength(bytes.length)
                    .build())
            .build(),
        messageToProtoTestHelper(bytes, Integer.MAX_VALUE));
  }

  @Test
  public void messageToProto_truncated() throws Exception {
    byte[] bytes
        = "this is a long message: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA".getBytes(US_ASCII);
    assertEquals(
        GrpcLogEntry.newBuilder()
            .setMessage(
                Message
                    .newBuilder()
                    .setLength(bytes.length)
                    .build())
            .setPayloadTruncated(true)
            .build(),
        messageToProtoTestHelper(bytes, 0));

    int limit = 10;
    String truncatedMessage = "this is a ";
    assertEquals(
        GrpcLogEntry.newBuilder()
            .setMessage(
                Message
                    .newBuilder()
                    .setData(ByteString.copyFrom(truncatedMessage.getBytes(US_ASCII)))
                    .setLength(bytes.length)
                    .build())
            .setPayloadTruncated(true)
            .build(),
        messageToProtoTestHelper(bytes, limit));
  }

  @Test
  public void logClientHeader() throws Exception {
    long seq = 1;
    String authority = "authority";
    String methodName = "service/method";
    Duration timeout = Durations.fromMillis(1234);
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress peerAddress = new InetSocketAddress(address, port);
    long callId = 1000;

    GrpcLogEntry.Builder builder =
        metadataToProtoTestHelper(EventType.EVENT_TYPE_CLIENT_HEADER, nonEmptyMetadata, 10)
            .toBuilder()
            .setTimestamp(timestamp)
            .setSequenceIdWithinCall(seq)
            .setLogger(Logger.LOGGER_CLIENT)
            .setCallId(callId);
    builder.getClientHeaderBuilder()
        .setMethodName("/" + methodName)
        .setAuthority(authority)
        .setTimeout(timeout);
    GrpcLogEntry base = builder.build();
    {
      sinkWriterImpl.logClientHeader(
          seq,
          methodName,
          authority,
          timeout,
          nonEmptyMetadata,
          Logger.LOGGER_CLIENT,
          callId,
          /*peerAddress=*/ null);
      verify(sink).write(base);
    }

    // logger is server
    {
      sinkWriterImpl.logClientHeader(
          seq,
          methodName,
          authority,
          timeout,
          nonEmptyMetadata,
          Logger.LOGGER_SERVER,
          callId,
          peerAddress);
      verify(sink).write(
          base.toBuilder()
              .setPeer(BinlogHelper.socketToProto(peerAddress))
              .setLogger(Logger.LOGGER_SERVER)
              .build());
    }

    // authority is null
    {
      sinkWriterImpl.logClientHeader(
          seq,
          methodName,
          /*authority=*/ null,
          timeout,
          nonEmptyMetadata,
          Logger.LOGGER_CLIENT,
          callId,
          /*peerAddress=*/ null);

      verify(sink).write(
          base.toBuilder()
              .setClientHeader(builder.getClientHeader().toBuilder().clearAuthority().build())
              .build());
    }

    // timeout is null
    {
      sinkWriterImpl.logClientHeader(
          seq,
          methodName,
          authority,
          /*timeout=*/ null,
          nonEmptyMetadata,
          Logger.LOGGER_CLIENT,
          callId,
          /*peerAddress=*/ null);

      verify(sink).write(
          base.toBuilder()
              .setClientHeader(builder.getClientHeader().toBuilder().clearTimeout().build())
              .build());
    }

    // peerAddress is non null (error for client side)
    try {
      sinkWriterImpl.logClientHeader(
          seq,
          methodName,
          authority,
          timeout,
          nonEmptyMetadata,
          Logger.LOGGER_CLIENT,
          callId,
          peerAddress);
      fail();
    } catch (IllegalArgumentException expected) {
      // noop
    }
  }

  @Test
  public void logServerHeader() throws Exception {
    long seq = 1;
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress peerAddress = new InetSocketAddress(address, port);
    long callId = 1000;

    GrpcLogEntry.Builder builder =
        metadataToProtoTestHelper(EventType.EVENT_TYPE_SERVER_HEADER, nonEmptyMetadata, 10)
            .toBuilder()
            .setTimestamp(timestamp)
            .setSequenceIdWithinCall(seq)
            .setLogger(Logger.LOGGER_CLIENT)
            .setCallId(callId)
            .setPeer(BinlogHelper.socketToProto(peerAddress));

    {
      sinkWriterImpl.logServerHeader(
          seq,
          nonEmptyMetadata,
          Logger.LOGGER_CLIENT,
          callId,
          peerAddress);
      verify(sink).write(builder.build());
    }

    // logger is server
    // null peerAddress is required for server side
    {
      sinkWriterImpl.logServerHeader(
          seq,
          nonEmptyMetadata,
          Logger.LOGGER_SERVER,
          callId,
          /*peerAddress=*/ null);
      verify(sink).write(
          builder
              .setLogger(Logger.LOGGER_SERVER)
              .clearPeer()
              .build());
    }

    // logger is server
    // non null peerAddress is an error
    try {
      sinkWriterImpl.logServerHeader(
          seq,
          nonEmptyMetadata,
          Logger.LOGGER_SERVER,
          callId,
          peerAddress);
      fail();
    } catch (IllegalArgumentException expected) {
      // noop
    }
  }

  @Test
  public void logTrailer() throws Exception {
    long seq = 1;
    InetAddress address = InetAddress.getByName("127.0.0.1");
    int port = 12345;
    InetSocketAddress peerAddress = new InetSocketAddress(address, port);
    long callId = 1000;
    Status statusDescription = Status.INTERNAL.withDescription("my description");

    GrpcLogEntry.Builder builder =
        metadataToProtoTestHelper(EventType.EVENT_TYPE_SERVER_TRAILER, nonEmptyMetadata, 10)
            .toBuilder()
            .setTimestamp(timestamp)
            .setSequenceIdWithinCall(seq)
            .setLogger(Logger.LOGGER_CLIENT)
            .setCallId(callId)
            .setPeer(BinlogHelper.socketToProto(peerAddress));

    builder.getTrailerBuilder()
        .setStatusCode(Status.INTERNAL.getCode().value())
        .setStatusMessage("my description");
    GrpcLogEntry base = builder.build();

    {
      sinkWriterImpl.logTrailer(
          seq,
          statusDescription,
          nonEmptyMetadata,
          Logger.LOGGER_CLIENT,
          callId,
          peerAddress);
      verify(sink).write(base);
    }

    // logger is server
    {
      sinkWriterImpl.logTrailer(
          seq,
          statusDescription,
          nonEmptyMetadata,
          Logger.LOGGER_SERVER,
          callId,
          /*peerAddress=*/ null);
      verify(sink).write(
          base.toBuilder()
              .clearPeer()
              .setLogger(Logger.LOGGER_SERVER)
              .build());
    }

    // peerAddress is null
    {
      sinkWriterImpl.logTrailer(
          seq,
          statusDescription,
          nonEmptyMetadata,
          Logger.LOGGER_CLIENT,
          callId,
          /*peerAddress=*/ null);
      verify(sink).write(
          base.toBuilder()
              .clearPeer()
              .build());
    }

    // status code is present but description is null
    {
      sinkWriterImpl.logTrailer(
          seq,
          statusDescription.getCode().toStatus(), // strip the description
          nonEmptyMetadata,
          Logger.LOGGER_CLIENT,
          callId,
          peerAddress);
      verify(sink).write(
          base.toBuilder()
              .setTrailer(base.getTrailer().toBuilder().clearStatusMessage())
              .build());
    }

    // status proto always logged if present (com.google.rpc.Status),
    {
      int zeroHeaderBytes = 0;
      SinkWriterImpl truncatingWriter = new SinkWriterImpl(
          sink, timeProvider, zeroHeaderBytes, MESSAGE_LIMIT);
      com.google.rpc.Status statusProto = com.google.rpc.Status.newBuilder()
          .addDetails(
              Any.pack(StringValue.newBuilder().setValue("arbitrarypayload").build()))
          .setCode(Status.INTERNAL.getCode().value())
          .setMessage("status detail string")
          .build();
      StatusException statusException
          = StatusProto.toStatusException(statusProto, nonEmptyMetadata);
      truncatingWriter.logTrailer(
          seq,
          statusException.getStatus(),
          statusException.getTrailers(),
          Logger.LOGGER_CLIENT,
          callId,
          peerAddress);
      verify(sink).write(
          base.toBuilder()
              .setTrailer(
                  builder.getTrailerBuilder()
                      .setStatusMessage("status detail string")
                      .setStatusDetails(ByteString.copyFrom(statusProto.toByteArray()))
                      .setMetadata(io.grpc.binarylog.v1.Metadata.getDefaultInstance()))
              .build());
    }
  }

  @Test
  public void alwaysLoggedMetadata_grpcTraceBin() throws Exception {
    Metadata.Key<byte[]> key
        = Metadata.Key.of("grpc-trace-bin", Metadata.BINARY_BYTE_MARSHALLER);
    Metadata metadata = new Metadata();
    metadata.put(key, new byte[1]);
    int zeroHeaderBytes = 0;
    MaybeTruncated<io.grpc.binarylog.v1.Metadata.Builder> pair =
        createMetadataProto(metadata, zeroHeaderBytes);
    assertEquals(
        key.name(),
        Iterables.getOnlyElement(pair.proto.getEntryBuilderList()).getKey());
    assertFalse(pair.truncated);
  }

  @Test
  public void neverLoggedMetadata_grpcStatusDetilsBin() throws Exception {
    Metadata.Key<byte[]> key
        = Metadata.Key.of("grpc-status-details-bin", Metadata.BINARY_BYTE_MARSHALLER);
    Metadata metadata = new Metadata();
    metadata.put(key, new byte[1]);
    int unlimitedHeaderBytes = Integer.MAX_VALUE;
    MaybeTruncated<io.grpc.binarylog.v1.Metadata.Builder> pair
        = createMetadataProto(metadata, unlimitedHeaderBytes);
    assertThat(pair.proto.getEntryBuilderList()).isEmpty();
    assertFalse(pair.truncated);
  }

  @Test
  public void logRpcMessage() throws Exception {
    long seq = 1;
    long callId = 1000;
    GrpcLogEntry base = messageToProtoTestHelper(message, MESSAGE_LIMIT).toBuilder()
        .setTimestamp(timestamp)
        .setType(EventType.EVENT_TYPE_CLIENT_MESSAGE)
        .setLogger(Logger.LOGGER_CLIENT)
        .setSequenceIdWithinCall(1)
        .setCallId(callId)
        .build();
    {
      sinkWriterImpl.logRpcMessage(
          seq,
          EventType.EVENT_TYPE_CLIENT_MESSAGE,
          BYTEARRAY_MARSHALLER,
          message,
          Logger.LOGGER_CLIENT,
          callId);
      verify(sink).write(base);
    }

    // server messsage
    {
      sinkWriterImpl.logRpcMessage(
          seq,
          EventType.EVENT_TYPE_SERVER_MESSAGE,
          BYTEARRAY_MARSHALLER,
          message,
          Logger.LOGGER_CLIENT,
          callId);
      verify(sink).write(
          base.toBuilder()
              .setType(EventType.EVENT_TYPE_SERVER_MESSAGE)
              .build());
    }

    // logger is server
    {
      sinkWriterImpl.logRpcMessage(
          seq,
          EventType.EVENT_TYPE_CLIENT_MESSAGE,
          BYTEARRAY_MARSHALLER,
          message,
          Logger.LOGGER_SERVER,
          callId);
      verify(sink).write(
          base.toBuilder()
              .setLogger(Logger.LOGGER_SERVER)
              .build());
    }
  }

  @Test
  public void getPeerSocketTest() {
    assertNull(getPeerSocket(Attributes.EMPTY));
    assertSame(
        peer,
        getPeerSocket(Attributes.newBuilder().set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, peer).build()));
  }

  @Test
  @SuppressWarnings({"rawtypes", "unchecked"})
  public void serverDeadlineLogged() {
    final AtomicReference<ServerCall> interceptedCall =
        new AtomicReference<>();
    final ServerCall.Listener mockListener = mock(ServerCall.Listener.class);

    final MethodDescriptor<byte[], byte[]> method =
        MethodDescriptor.<byte[], byte[]>newBuilder()
            .setType(MethodType.UNKNOWN)
            .setFullMethodName("service/method")
            .setRequestMarshaller(BYTEARRAY_MARSHALLER)
            .setResponseMarshaller(BYTEARRAY_MARSHALLER)
            .build();

    // We expect the contents of the "grpc-timeout" header to be installed the context
    Context.current()
        .withDeadlineAfter(1, TimeUnit.SECONDS, Executors.newSingleThreadScheduledExecutor())
        .run(new Runnable() {
          @Override
          public void run() {
            ServerCall.Listener<byte[]> unused =
                new BinlogHelper(mockSinkWriter)
                    .getServerInterceptor(CALL_ID)
                    .interceptCall(
                        new NoopServerCall<byte[], byte[]>() {
                          @Override
                          public MethodDescriptor<byte[], byte[]> getMethodDescriptor() {
                            return method;
                          }
                        },
                        new Metadata(),
                        new ServerCallHandler<byte[], byte[]>() {
                          @Override
                          public ServerCall.Listener<byte[]> startCall(
                              ServerCall<byte[], byte[]> call,
                              Metadata headers) {
                            interceptedCall.set(call);
                            return mockListener;
                          }
                        });
          }
        });
    ArgumentCaptor<Duration> timeoutCaptor = ArgumentCaptor.forClass(Duration.class);
    verify(mockSinkWriter).logClientHeader(
        /*seq=*/ eq(1L),
        eq("service/method"),
        ArgumentMatchers.<String>isNull(),
        timeoutCaptor.capture(),
        any(Metadata.class),
        eq(Logger.LOGGER_SERVER),
        eq(CALL_ID),
        ArgumentMatchers.<SocketAddress>isNull());
    verifyNoMoreInteractions(mockSinkWriter);
    Duration timeout = timeoutCaptor.getValue();
    assertThat(TimeUnit.SECONDS.toNanos(1) - Durations.toNanos(timeout))
        .isAtMost(TimeUnit.MILLISECONDS.toNanos(250));
  }

  @Test
  @SuppressWarnings({"unchecked"})
  public void clientDeadlineLogged_deadlineSetViaCallOption() {
    MethodDescriptor<byte[], byte[]> method =
        MethodDescriptor.<byte[], byte[]>newBuilder()
            .setType(MethodType.UNKNOWN)
            .setFullMethodName("service/method")
            .setRequestMarshaller(BYTEARRAY_MARSHALLER)
            .setResponseMarshaller(BYTEARRAY_MARSHALLER)
            .build();
    ClientCall.Listener<byte[]> mockListener = mock(ClientCall.Listener.class);

    ClientCall<byte[], byte[]> call =
        new BinlogHelper(mockSinkWriter)
            .getClientInterceptor(CALL_ID)
            .interceptCall(
                method,
                CallOptions.DEFAULT.withDeadlineAfter(1, TimeUnit.SECONDS),
                new Channel() {
                  @Override
                  public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
                      MethodDescriptor<RequestT, ResponseT> methodDescriptor,
                      CallOptions callOptions) {
                    return new NoopClientCall<>();
                  }

                  @Override
                  public String authority() {
                    return null;
                  }
                });
    call.start(mockListener, new Metadata());
    ArgumentCaptor<Duration> callOptTimeoutCaptor = ArgumentCaptor.forClass(Duration.class);
    verify(mockSinkWriter)
        .logClientHeader(
            anyLong(),
            AdditionalMatchers.or(ArgumentMatchers.<String>isNull(), anyString()),
            AdditionalMatchers.or(ArgumentMatchers.<String>isNull(), anyString()),
            callOptTimeoutCaptor.capture(),
            any(Metadata.class),
            any(GrpcLogEntry.Logger.class),
            anyLong(),
            AdditionalMatchers.or(ArgumentMatchers.<SocketAddress>isNull(),
                ArgumentMatchers.<SocketAddress>any()));
    Duration timeout = callOptTimeoutCaptor.getValue();
    assertThat(TimeUnit.SECONDS.toNanos(1) - Durations.toNanos(timeout))
        .isAtMost(TimeUnit.MILLISECONDS.toNanos(250));
  }

  @Test
  @SuppressWarnings({"unchecked"})
  public void clientDeadlineLogged_deadlineSetViaContext() throws Exception {
    // important: deadline is read from the ctx where call was created
    final SettableFuture<ClientCall<byte[], byte[]>> callFuture = SettableFuture.create();
    Context.current()
        .withDeadline(
            Deadline.after(1, TimeUnit.SECONDS), Executors.newSingleThreadScheduledExecutor())
        .run(new Runnable() {
          @Override
          public void run() {
            MethodDescriptor<byte[], byte[]> method =
                MethodDescriptor.<byte[], byte[]>newBuilder()
                    .setType(MethodType.UNKNOWN)
                    .setFullMethodName("service/method")
                    .setRequestMarshaller(BYTEARRAY_MARSHALLER)
                    .setResponseMarshaller(BYTEARRAY_MARSHALLER)
                    .build();

            callFuture.set(new BinlogHelper(mockSinkWriter)
                .getClientInterceptor(CALL_ID)
                .interceptCall(
                    method,
                    CallOptions.DEFAULT.withDeadlineAfter(1, TimeUnit.SECONDS),
                    new Channel() {
                      @Override
                      public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
                          MethodDescriptor<RequestT, ResponseT> methodDescriptor,
                          CallOptions callOptions) {
                        return new NoopClientCall<>();
                      }

                      @Override
                      public String authority() {
                        return null;
                      }
                    }));
          }
        });
    ClientCall.Listener<byte[]> mockListener = mock(ClientCall.Listener.class);
    callFuture.get().start(mockListener, new Metadata());
    ArgumentCaptor<Duration> callOptTimeoutCaptor = ArgumentCaptor.forClass(Duration.class);
    verify(mockSinkWriter)
        .logClientHeader(
            anyLong(),
            anyString(),
            ArgumentMatchers.<String>any(),
            callOptTimeoutCaptor.capture(),
            any(Metadata.class),
            any(GrpcLogEntry.Logger.class),
            anyLong(),
            AdditionalMatchers.or(ArgumentMatchers.<SocketAddress>isNull(),
                ArgumentMatchers.<SocketAddress>any()));
    Duration timeout = callOptTimeoutCaptor.getValue();
    assertThat(TimeUnit.SECONDS.toNanos(1) - Durations.toNanos(timeout))
        .isAtMost(TimeUnit.MILLISECONDS.toNanos(250));
  }

  @Test
  @SuppressWarnings({"rawtypes", "unchecked"})
  public void clientInterceptor() throws Exception {
    final AtomicReference<ClientCall.Listener> interceptedListener =
        new AtomicReference<>();
    // capture these manually because ClientCall can not be mocked
    final AtomicReference<Metadata> actualClientInitial = new AtomicReference<>();
    final AtomicReference<Object> actualRequest = new AtomicReference<>();

    final SettableFuture<Void> halfCloseCalled = SettableFuture.create();
    final SettableFuture<Void> cancelCalled = SettableFuture.create();
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
          public void cancel(String message, Throwable cause) {
            cancelCalled.set(null);
          }

          @Override
          public void halfClose() {
            halfCloseCalled.set(null);
          }

          @Override
          public Attributes getAttributes() {
            return Attributes.newBuilder().set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, peer).build();
          }
        };
      }

      @Override
      public String authority() {
        return "the-authority";
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
                CallOptions.DEFAULT,
                channel);

    // send client header
    {
      Metadata clientInitial = new Metadata();
      interceptedCall.start(mockListener, clientInitial);
      verify(mockSinkWriter).logClientHeader(
          /*seq=*/ eq(1L),
          eq("service/method"),
          eq("the-authority"),
          ArgumentMatchers.<Duration>isNull(),
          same(clientInitial),
          eq(Logger.LOGGER_CLIENT),
          eq(CALL_ID),
          ArgumentMatchers.<SocketAddress>isNull());
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(clientInitial, actualClientInitial.get());
    }

    // receive server header
    {
      Metadata serverInitial = new Metadata();
      interceptedListener.get().onHeaders(serverInitial);
      verify(mockSinkWriter).logServerHeader(
          /*seq=*/ eq(2L),
          same(serverInitial),
          eq(Logger.LOGGER_CLIENT),
          eq(CALL_ID),
          same(peer));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onHeaders(same(serverInitial));
    }

    // send client msg
    {
      byte[] request = "this is a request".getBytes(US_ASCII);
      interceptedCall.sendMessage(request);
      verify(mockSinkWriter).logRpcMessage(
          /*seq=*/ eq(3L),
          eq(EventType.EVENT_TYPE_CLIENT_MESSAGE),
          same(BYTEARRAY_MARSHALLER),
          same(request),
          eq(Logger.LOGGER_CLIENT),
          eq(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(request, actualRequest.get());
    }

    // client half close
    {
      interceptedCall.halfClose();
      verify(mockSinkWriter).logHalfClose(
          /*seq=*/ eq(4L),
          eq(Logger.LOGGER_CLIENT),
          eq(CALL_ID));
      halfCloseCalled.get(1, TimeUnit.SECONDS);
      verifyNoMoreInteractions(mockSinkWriter);
    }

    // receive server msg
    {
      byte[] response = "this is a response".getBytes(US_ASCII);
      interceptedListener.get().onMessage(response);
      verify(mockSinkWriter).logRpcMessage(
          /*seq=*/ eq(5L),
          eq(EventType.EVENT_TYPE_SERVER_MESSAGE),
          same(BYTEARRAY_MARSHALLER),
          same(response),
          eq(Logger.LOGGER_CLIENT),
          eq(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onMessage(same(response));
    }

    // receive trailer
    {
      Status status = Status.INTERNAL.withDescription("some description");
      Metadata trailers = new Metadata();

      interceptedListener.get().onClose(status, trailers);
      verify(mockSinkWriter).logTrailer(
          /*seq=*/ eq(6L),
          same(status),
          same(trailers),
          eq(Logger.LOGGER_CLIENT),
          eq(CALL_ID),
          ArgumentMatchers.<SocketAddress>isNull());
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onClose(same(status), same(trailers));
    }

    // cancel
    {
      interceptedCall.cancel(null, null);
      verify(mockSinkWriter).logCancel(
          /*seq=*/ eq(7L),
          eq(Logger.LOGGER_CLIENT),
          eq(CALL_ID));
      cancelCalled.get(1, TimeUnit.SECONDS);
    }
  }

  @Test
  @SuppressWarnings({"rawtypes", "unchecked"})
  public void clientInterceptor_trailersOnlyResponseLogsPeerAddress() throws Exception {
    final AtomicReference<ClientCall.Listener> interceptedListener =
        new AtomicReference<>();
    // capture these manually because ClientCall can not be mocked
    final AtomicReference<Metadata> actualClientInitial = new AtomicReference<>();
    final AtomicReference<Object> actualRequest = new AtomicReference<>();

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
        return "the-authority";
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
                CallOptions.DEFAULT.withDeadlineAfter(1, TimeUnit.SECONDS),
                channel);
    Metadata clientInitial = new Metadata();
    interceptedCall.start(mockListener, clientInitial);
    verify(mockSinkWriter).logClientHeader(
        /*seq=*/ eq(1L),
        anyString(),
        anyString(),
        any(Duration.class),
        any(Metadata.class),
        eq(Logger.LOGGER_CLIENT),
        eq(CALL_ID),
        ArgumentMatchers.<SocketAddress>isNull());
    verifyNoMoreInteractions(mockSinkWriter);

    // trailer only response
    {
      Status status = Status.INTERNAL.withDescription("some description");
      Metadata trailers = new Metadata();

      interceptedListener.get().onClose(status, trailers);
      verify(mockSinkWriter).logTrailer(
          /*seq=*/ eq(2L),
          same(status),
          same(trailers),
          eq(Logger.LOGGER_CLIENT),
          eq(CALL_ID),
          same(peer));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onClose(same(status), same(trailers));
    }
  }

  @Test
  @SuppressWarnings({"rawtypes", "unchecked"})
  public void serverInterceptor() throws Exception {
    final AtomicReference<ServerCall> interceptedCall =
        new AtomicReference<>();
    ServerCall.Listener<byte[]> capturedListener;
    final ServerCall.Listener mockListener = mock(ServerCall.Listener.class);
    // capture these manually because ServerCall can not be mocked
    final AtomicReference<Metadata> actualServerInitial = new AtomicReference<>();
    final AtomicReference<byte[]> actualResponse = new AtomicReference<>();
    final AtomicReference<Status> actualStatus = new AtomicReference<>();
    final AtomicReference<Metadata> actualTrailers = new AtomicReference<>();

    // begin call and receive client header
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

                    @Override
                    public String getAuthority() {
                      return "the-authority";
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
      verify(mockSinkWriter).logClientHeader(
          /*seq=*/ eq(1L),
          eq("service/method"),
          eq("the-authority"),
          ArgumentMatchers.<Duration>isNull(),
          same(clientInitial),
          eq(Logger.LOGGER_SERVER),
          eq(CALL_ID),
          same(peer));
      verifyNoMoreInteractions(mockSinkWriter);
    }

    // send server header
    {
      Metadata serverInital = new Metadata();
      interceptedCall.get().sendHeaders(serverInital);
      verify(mockSinkWriter).logServerHeader(
          /*seq=*/ eq(2L),
          same(serverInital),
          eq(Logger.LOGGER_SERVER),
          eq(CALL_ID),
          ArgumentMatchers.<SocketAddress>isNull());
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(serverInital, actualServerInitial.get());
    }

    // receive client msg
    {
      byte[] request = "this is a request".getBytes(US_ASCII);
      capturedListener.onMessage(request);
      verify(mockSinkWriter).logRpcMessage(
          /*seq=*/ eq(3L),
          eq(EventType.EVENT_TYPE_CLIENT_MESSAGE),
          same(BYTEARRAY_MARSHALLER),
          same(request),
          eq(Logger.LOGGER_SERVER),
          eq(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onMessage(same(request));
    }

    // client half close
    {
      capturedListener.onHalfClose();
      verify(mockSinkWriter).logHalfClose(
          eq(4L),
          eq(Logger.LOGGER_SERVER),
          eq(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      verify(mockListener).onHalfClose();
    }

    // send server msg
    {
      byte[] response = "this is a response".getBytes(US_ASCII);
      interceptedCall.get().sendMessage(response);
      verify(mockSinkWriter).logRpcMessage(
          /*seq=*/ eq(5L),
          eq(EventType.EVENT_TYPE_SERVER_MESSAGE),
          same(BYTEARRAY_MARSHALLER),
          same(response),
          eq(Logger.LOGGER_SERVER),
          eq(CALL_ID));
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(response, actualResponse.get());
    }

    // send trailer
    {
      Status status = Status.INTERNAL.withDescription("some description");
      Metadata trailers = new Metadata();
      interceptedCall.get().close(status, trailers);
      verify(mockSinkWriter).logTrailer(
          /*seq=*/ eq(6L),
          same(status),
          same(trailers),
          eq(Logger.LOGGER_SERVER),
          eq(CALL_ID),
          ArgumentMatchers.<SocketAddress>isNull());
      verifyNoMoreInteractions(mockSinkWriter);
      assertSame(status, actualStatus.get());
      assertSame(trailers, actualTrailers.get());
    }

    // cancel
    {
      capturedListener.onCancel();
      verify(mockSinkWriter).logCancel(
          /*seq=*/ eq(7L),
          eq(Logger.LOGGER_SERVER),
          eq(CALL_ID));
      verify(mockListener).onCancel();
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
          new SinkWriterImpl(mock(BinaryLogSink.class), null, maxHeaderBytes, maxMessageBytes));
    }
  }

  private static void assertSameLimits(BinlogHelper a, BinlogHelper b) {
    assertEquals(a.writer.getMaxMessageBytes(), b.writer.getMaxMessageBytes());
    assertEquals(a.writer.getMaxHeaderBytes(), b.writer.getMaxHeaderBytes());
  }

  private BinlogHelper makeLog(String factoryConfigStr, String lookup) {
    return new BinlogHelper.FactoryImpl(sink, factoryConfigStr).getLog(lookup);
  }

  private BinlogHelper makeOptions(String logConfigStr) {
    return FactoryImpl.createBinaryLog(sink, logConfigStr);
  }

  private static GrpcLogEntry metadataToProtoTestHelper(
      EventType type, Metadata metadata, int maxHeaderBytes) {
    GrpcLogEntry.Builder builder = GrpcLogEntry.newBuilder();
    MaybeTruncated<io.grpc.binarylog.v1.Metadata.Builder> pair
        = BinlogHelper.createMetadataProto(metadata, maxHeaderBytes);
    switch (type) {
      case EVENT_TYPE_CLIENT_HEADER:
        builder.setClientHeader(ClientHeader.newBuilder().setMetadata(pair.proto));
        break;
      case EVENT_TYPE_SERVER_HEADER:
        builder.setServerHeader(ServerHeader.newBuilder().setMetadata(pair.proto));
        break;
      case EVENT_TYPE_SERVER_TRAILER:
        builder.setTrailer(Trailer.newBuilder().setMetadata(pair.proto));
        break;
      default:
        throw new IllegalArgumentException();
    }
    builder.setType(type).setPayloadTruncated(pair.truncated);
    return builder.build();
  }

  private static GrpcLogEntry messageToProtoTestHelper(
      byte[] message, int maxMessageBytes) {
    GrpcLogEntry.Builder builder = GrpcLogEntry.newBuilder();
    MaybeTruncated<Message.Builder> pair
        = BinlogHelper.createMessageProto(message, maxMessageBytes);
    builder.setMessage(pair.proto).setPayloadTruncated(pair.truncated);
    return builder.build();
  }
}
