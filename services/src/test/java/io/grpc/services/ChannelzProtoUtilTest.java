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

package io.grpc.services;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.InternalChannelz.id;
import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import com.google.common.base.Charsets;
import com.google.common.collect.ImmutableList;
import com.google.common.collect.ImmutableMap;
import com.google.protobuf.Any;
import com.google.protobuf.ByteString;
import com.google.protobuf.Int64Value;
import com.google.protobuf.Message;
import com.google.protobuf.util.Durations;
import com.google.protobuf.util.Timestamps;
import io.grpc.ConnectivityState;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace.Event;
import io.grpc.InternalChannelz.ChannelTrace.Event.Severity;
import io.grpc.InternalChannelz.RootChannelList;
import io.grpc.InternalChannelz.ServerList;
import io.grpc.InternalChannelz.ServerSocketsList;
import io.grpc.InternalChannelz.ServerStats;
import io.grpc.InternalChannelz.SocketOptions;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalInstrumented;
import io.grpc.InternalWithLogId;
import io.grpc.channelz.v1.Address;
import io.grpc.channelz.v1.Address.OtherAddress;
import io.grpc.channelz.v1.Address.TcpIpAddress;
import io.grpc.channelz.v1.Address.UdsAddress;
import io.grpc.channelz.v1.Channel;
import io.grpc.channelz.v1.ChannelConnectivityState;
import io.grpc.channelz.v1.ChannelConnectivityState.State;
import io.grpc.channelz.v1.ChannelData;
import io.grpc.channelz.v1.ChannelRef;
import io.grpc.channelz.v1.ChannelTrace;
import io.grpc.channelz.v1.ChannelTraceEvent;
import io.grpc.channelz.v1.GetChannelRequest;
import io.grpc.channelz.v1.GetServerSocketsResponse;
import io.grpc.channelz.v1.GetServersResponse;
import io.grpc.channelz.v1.GetTopChannelsResponse;
import io.grpc.channelz.v1.Security;
import io.grpc.channelz.v1.Security.OtherSecurity;
import io.grpc.channelz.v1.Security.Tls;
import io.grpc.channelz.v1.Server;
import io.grpc.channelz.v1.ServerData;
import io.grpc.channelz.v1.ServerRef;
import io.grpc.channelz.v1.Socket;
import io.grpc.channelz.v1.SocketData;
import io.grpc.channelz.v1.SocketOption;
import io.grpc.channelz.v1.SocketOptionLinger;
import io.grpc.channelz.v1.SocketOptionTcpInfo;
import io.grpc.channelz.v1.SocketOptionTimeout;
import io.grpc.channelz.v1.SocketRef;
import io.grpc.channelz.v1.Subchannel;
import io.grpc.channelz.v1.SubchannelRef;
import io.grpc.services.ChannelzTestHelper.TestChannel;
import io.grpc.services.ChannelzTestHelper.TestListenSocket;
import io.grpc.services.ChannelzTestHelper.TestServer;
import io.grpc.services.ChannelzTestHelper.TestSocket;
import io.netty.channel.unix.DomainSocketAddress;
import java.net.Inet4Address;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.security.cert.Certificate;
import java.util.Arrays;
import java.util.Collections;
import java.util.Map.Entry;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class ChannelzProtoUtilTest {

  private final TestChannel channel = new TestChannel();
  private final ChannelRef channelRef = ChannelRef
      .newBuilder()
      .setName(channel.toString())
      .setChannelId(channel.getLogId().getId())
      .build();
  private final ChannelData channelData = ChannelData
      .newBuilder()
      .setTarget("sometarget")
      .setState(ChannelConnectivityState.newBuilder().setState(State.READY))
      .setCallsStarted(1)
      .setCallsSucceeded(2)
      .setCallsFailed(3)
      .setLastCallStartedTimestamp(Timestamps.fromNanos(4))
      .build();
  private final Channel channelProto = Channel
      .newBuilder()
      .setRef(channelRef)
      .setData(channelData)
      .build();

  private final TestChannel subchannel = new TestChannel();
  private final SubchannelRef subchannelRef = SubchannelRef
      .newBuilder()
      .setName(subchannel.toString())
      .setSubchannelId(subchannel.getLogId().getId())
      .build();
  private final ChannelData subchannelData = ChannelData
      .newBuilder()
      .setTarget("sometarget")
      .setState(ChannelConnectivityState.newBuilder().setState(State.READY))
      .setCallsStarted(1)
      .setCallsSucceeded(2)
      .setCallsFailed(3)
      .setLastCallStartedTimestamp(Timestamps.fromNanos(4))
      .build();
  private final Subchannel subchannelProto = Subchannel
        .newBuilder()
        .setRef(subchannelRef)
        .setData(subchannelData)
        .build();

  private final TestServer server = new TestServer();
  private final ServerRef serverRef = ServerRef
      .newBuilder()
      .setName(server.toString())
      .setServerId(server.getLogId().getId())
      .build();
  private final ServerData serverData = ServerData
      .newBuilder()
      .setCallsStarted(1)
      .setCallsSucceeded(2)
      .setCallsFailed(3)
      .setLastCallStartedTimestamp(Timestamps.fromNanos(4))
      .build();
  private final Server serverProto = Server
      .newBuilder()
      .setRef(serverRef)
      .setData(serverData)
      .build();

  private final SocketOption sockOptLingerDisabled = SocketOption
      .newBuilder()
      .setName("SO_LINGER")
      .setAdditional(
          Any.pack(SocketOptionLinger.getDefaultInstance()))
      .build();

  private final SocketOption sockOptlinger10s = SocketOption
      .newBuilder()
      .setName("SO_LINGER")
      .setAdditional(
          Any.pack(SocketOptionLinger
              .newBuilder()
              .setActive(true)
              .setDuration(Durations.fromSeconds(10))
              .build()))
      .build();

  private final SocketOption sockOptTimeout200ms = SocketOption
      .newBuilder()
      .setName("SO_TIMEOUT")
      .setAdditional(
          Any.pack(SocketOptionTimeout
          .newBuilder()
          .setDuration(Durations.fromMillis(200))
          .build())
      ).build();

  private final SocketOption sockOptAdditional = SocketOption
      .newBuilder()
      .setName("SO_MADE_UP_OPTION")
      .setValue("some-made-up-value")
      .build();

  private final InternalChannelz.TcpInfo channelzTcpInfo
      = new InternalChannelz.TcpInfo.Builder()
      .setState(70)
      .setCaState(71)
      .setRetransmits(72)
      .setProbes(73)
      .setBackoff(74)
      .setOptions(75)
      .setSndWscale(76)
      .setRcvWscale(77)
      .setRto(78)
      .setAto(79)
      .setSndMss(710)
      .setRcvMss(711)
      .setUnacked(712)
      .setSacked(713)
      .setLost(714)
      .setRetrans(715)
      .setFackets(716)
      .setLastDataSent(717)
      .setLastAckSent(718)
      .setLastDataRecv(719)
      .setLastAckRecv(720)
      .setPmtu(721)
      .setRcvSsthresh(722)
      .setRtt(723)
      .setRttvar(724)
      .setSndSsthresh(725)
      .setSndCwnd(726)
      .setAdvmss(727)
      .setReordering(728)
      .build();

  private final SocketOption socketOptionTcpInfo = SocketOption
      .newBuilder()
      .setName("TCP_INFO")
      .setAdditional(
          Any.pack(
              SocketOptionTcpInfo.newBuilder()
                  .setTcpiState(70)
                  .setTcpiCaState(71)
                  .setTcpiRetransmits(72)
                  .setTcpiProbes(73)
                  .setTcpiBackoff(74)
                  .setTcpiOptions(75)
                  .setTcpiSndWscale(76)
                  .setTcpiRcvWscale(77)
                  .setTcpiRto(78)
                  .setTcpiAto(79)
                  .setTcpiSndMss(710)
                  .setTcpiRcvMss(711)
                  .setTcpiUnacked(712)
                  .setTcpiSacked(713)
                  .setTcpiLost(714)
                  .setTcpiRetrans(715)
                  .setTcpiFackets(716)
                  .setTcpiLastDataSent(717)
                  .setTcpiLastAckSent(718)
                  .setTcpiLastDataRecv(719)
                  .setTcpiLastAckRecv(720)
                  .setTcpiPmtu(721)
                  .setTcpiRcvSsthresh(722)
                  .setTcpiRtt(723)
                  .setTcpiRttvar(724)
                  .setTcpiSndSsthresh(725)
                  .setTcpiSndCwnd(726)
                  .setTcpiAdvmss(727)
                  .setTcpiReordering(728)
                  .build()))
      .build();

  private final TestListenSocket listenSocket = new TestListenSocket();
  private final SocketRef listenSocketRef = SocketRef
      .newBuilder()
      .setName(listenSocket.toString())
      .setSocketId(id(listenSocket))
      .build();
  private final Address listenAddress = Address
      .newBuilder()
      .setTcpipAddress(
          TcpIpAddress
              .newBuilder()
              .setIpAddress(ByteString.copyFrom(
                  ((InetSocketAddress) listenSocket.listenAddress).getAddress().getAddress()))
              .setPort(1234))
      .build();

  private final TestSocket socket = new TestSocket();
  private final SocketRef socketRef = SocketRef
      .newBuilder()
      .setName(socket.toString())
      .setSocketId(socket.getLogId().getId())
      .build();
  private final SocketData socketDataWithDataNoSockOpts = SocketData
      .newBuilder()
      .setStreamsStarted(1)
      .setLastLocalStreamCreatedTimestamp(Timestamps.fromNanos(2))
      .setLastRemoteStreamCreatedTimestamp(Timestamps.fromNanos(3))
      .setStreamsSucceeded(4)
      .setStreamsFailed(5)
      .setMessagesSent(6)
      .setMessagesReceived(7)
      .setKeepAlivesSent(8)
      .setLastMessageSentTimestamp(Timestamps.fromNanos(9))
      .setLastMessageReceivedTimestamp(Timestamps.fromNanos(10))
      .setLocalFlowControlWindow(Int64Value.newBuilder().setValue(11))
      .setRemoteFlowControlWindow(Int64Value.newBuilder().setValue(12))
      .build();
  private final Address localAddress = Address
      .newBuilder()
      .setTcpipAddress(
          TcpIpAddress
              .newBuilder()
              .setIpAddress(ByteString.copyFrom(
                  ((InetSocketAddress) socket.local).getAddress().getAddress()))
              .setPort(1000))
      .build();
  private final Address remoteAddress = Address
      .newBuilder()
      .setTcpipAddress(
          TcpIpAddress
              .newBuilder()
              .setIpAddress(ByteString.copyFrom(
                  ((InetSocketAddress) socket.remote).getAddress().getAddress()))
              .setPort(1000))
      .build();

  private final ChannelTrace channelTrace = ChannelTrace
      .newBuilder()
      .setNumEventsLogged(1234)
      .setCreationTimestamp(Timestamps.fromNanos(1000))
      .build();

  @Test
  public void toChannelRef() {
    assertEquals(channelRef, ChannelzProtoUtil.toChannelRef(channel));
  }

  @Test
  public void toSubchannelRef() {
    assertEquals(subchannelRef, ChannelzProtoUtil.toSubchannelRef(subchannel));
  }

  @Test
  public void toServerRef() {
    assertEquals(serverRef, ChannelzProtoUtil.toServerRef(server));
  }

  @Test
  public void toSocketRef() {
    assertEquals(socketRef, ChannelzProtoUtil.toSocketRef(socket));
  }

  @Test
  public void toState() {
    for (ConnectivityState connectivityState : ConnectivityState.values()) {
      assertEquals(
          connectivityState.name(),
          ChannelzProtoUtil.toState(connectivityState).getValueDescriptor().getName());
    }
    assertEquals(State.UNKNOWN, ChannelzProtoUtil.toState(null));
  }

  @Test
  public void toSocket_withDataNoOptions() throws Exception {
    assertEquals(
        Socket
            .newBuilder()
            .setRef(socketRef)
            .setLocal(localAddress)
            .setRemote(remoteAddress)
            .setData(socketDataWithDataNoSockOpts)
            .build(),
        ChannelzProtoUtil.toSocket(socket));
  }

  @Test
  public void toSocket_noDataWithOptions() throws Exception {
    assertEquals(
        Socket
            .newBuilder()
            .setRef(listenSocketRef)
            .setLocal(listenAddress)
            .setData(
                SocketData
                    .newBuilder()
                    .addOption(
                        SocketOption
                            .newBuilder()
                            .setName("listen_option")
                            .setValue("listen_option_value")))
            .build(),
        ChannelzProtoUtil.toSocket(listenSocket));
  }

  @Test
  public void toSocket_withDataWithOptions() throws Exception {
    socket.socketOptions
        = new SocketOptions(null, null, null, ImmutableMap.of("test_name", "test_value"));
    assertEquals(
        Socket
            .newBuilder()
            .setRef(socketRef)
            .setLocal(localAddress)
            .setRemote(remoteAddress)
            .setData(
                SocketData
                    .newBuilder(socketDataWithDataNoSockOpts)
                    .addOption(
                        SocketOption.newBuilder()
                            .setName("test_name").setValue("test_value")))
            .build(),
        ChannelzProtoUtil.toSocket(socket));
  }

  @Test
  public void extractSocketData() throws Exception {
    // no options
    assertEquals(
        socketDataWithDataNoSockOpts,
        ChannelzProtoUtil.extractSocketData(socket.getStats().get()));

    // with options
    socket.socketOptions = toBuilder(socket.socketOptions)
        .setSocketOptionLingerSeconds(10)
        .setTcpInfo(channelzTcpInfo)
        .build();
    assertEquals(
        socketDataWithDataNoSockOpts
            .toBuilder()
            .addOption(sockOptlinger10s)
            .addOption(socketOptionTcpInfo)
            .build(),
        ChannelzProtoUtil.extractSocketData(socket.getStats().get()));
  }

  @Test
  public void toSocketData() throws Exception {
    assertEquals(
        socketDataWithDataNoSockOpts
            .toBuilder()
            .build(),
        ChannelzProtoUtil.extractSocketData(socket.getStats().get()));
  }

  @Test
  public void socketSecurityTls() throws Exception {
    Certificate local = mock(Certificate.class);
    Certificate remote = mock(Certificate.class);
    when(local.getEncoded()).thenReturn("localcert".getBytes(Charsets.UTF_8));
    when(remote.getEncoded()).thenReturn("remotecert".getBytes(Charsets.UTF_8));

    socket.security = new InternalChannelz.Security(
        new InternalChannelz.Tls("TLS_NULL_WITH_NULL_NULL", local, remote));
    assertEquals(
        Security.newBuilder().setTls(
            Tls.newBuilder()
            .setStandardName("TLS_NULL_WITH_NULL_NULL")
            .setLocalCertificate(ByteString.copyFrom("localcert", Charsets.UTF_8))
            .setRemoteCertificate(ByteString.copyFrom("remotecert", Charsets.UTF_8)))
        .build(),
        ChannelzProtoUtil.toSocket(socket).getSecurity());

    socket.security = new InternalChannelz.Security(
        new InternalChannelz.Tls("TLS_NULL_WITH_NULL_NULL", /*localCert=*/ null, remote));
    assertEquals(
        Security.newBuilder().setTls(
            Tls.newBuilder()
            .setStandardName("TLS_NULL_WITH_NULL_NULL")
            .setRemoteCertificate(ByteString.copyFrom("remotecert", Charsets.UTF_8)))
        .build(),
        ChannelzProtoUtil.toSocket(socket).getSecurity());

    socket.security = new InternalChannelz.Security(
        new InternalChannelz.Tls("TLS_NULL_WITH_NULL_NULL", local, /*remoteCert=*/ null));
    assertEquals(
        Security.newBuilder().setTls(
            Tls.newBuilder()
                .setStandardName("TLS_NULL_WITH_NULL_NULL")
                .setLocalCertificate(ByteString.copyFrom("localcert", Charsets.UTF_8)))
            .build(),
            ChannelzProtoUtil.toSocket(socket).getSecurity());
  }

  @Test
  public void socketSecurityOther() throws Exception {
    // what is packed here is not important, just pick some proto message
    Message contents = GetChannelRequest.newBuilder().setChannelId(1).build();
    Any packed = Any.pack(contents);
    socket.security
        = new InternalChannelz.Security(
            new InternalChannelz.OtherSecurity("other_security", packed));
    assertEquals(
        Security.newBuilder().setOther(
            OtherSecurity.newBuilder().setName("other_security").setValue(packed))
        .build(),
        ChannelzProtoUtil.toSocket(socket).getSecurity());
  }

  @Test
  public void toAddress_inet() throws Exception {
    InetSocketAddress inet4 = new InetSocketAddress(Inet4Address.getByName("10.0.0.1"), 1000);
    assertEquals(
        Address.newBuilder().setTcpipAddress(
            TcpIpAddress
                .newBuilder()
                .setIpAddress(ByteString.copyFrom(inet4.getAddress().getAddress()))
                .setPort(1000))
            .build(),
        ChannelzProtoUtil.toAddress(inet4));
  }

  @Test
  public void toAddress_uds() throws Exception {
    String path = "/tmp/foo";
    DomainSocketAddress uds = new DomainSocketAddress(path);
    assertEquals(
        Address.newBuilder().setUdsAddress(
            UdsAddress
                .newBuilder()
                .setFilename(path))
            .build(),
        ChannelzProtoUtil.toAddress(uds));
  }

  @Test
  public void toAddress_other() throws Exception {
    final String name = "my name";
    SocketAddress other = new SocketAddress() {
      @Override
      public String toString() {
        return name;
      }
    };
    assertEquals(
        Address.newBuilder().setOtherAddress(
            OtherAddress
                .newBuilder()
                .setName(name))
            .build(),
        ChannelzProtoUtil.toAddress(other));
  }

  @Test
  public void toServer() throws Exception {
    // no listen sockets
    assertEquals(serverProto, ChannelzProtoUtil.toServer(server));

    // 1 listen socket
    server.serverStats = toBuilder(server.serverStats)
        .addListenSockets(ImmutableList.<InternalInstrumented<SocketStats>>of(listenSocket))
        .build();
    assertEquals(
        serverProto
            .toBuilder()
            .addListenSocket(listenSocketRef)
            .build(),
        ChannelzProtoUtil.toServer(server));

    // multiple listen sockets
    TestListenSocket otherListenSocket = new TestListenSocket();
    SocketRef otherListenSocketRef = ChannelzProtoUtil.toSocketRef(otherListenSocket);
    server.serverStats = toBuilder(server.serverStats)
        .addListenSockets(
            ImmutableList.<InternalInstrumented<SocketStats>>of(otherListenSocket))
        .build();
    assertEquals(
        serverProto
            .toBuilder()
            .addListenSocket(listenSocketRef)
            .addListenSocket(otherListenSocketRef)
            .build(),
        ChannelzProtoUtil.toServer(server));
  }

  @Test
  public void toServerData() throws Exception {
    assertEquals(serverData, ChannelzProtoUtil.toServerData(server.serverStats));
  }

  @Test
  public void toChannel() throws Exception {
    assertEquals(channelProto, ChannelzProtoUtil.toChannel(channel));

    channel.stats = toBuilder(channel.stats)
        .setSubchannels(ImmutableList.<InternalWithLogId>of(subchannel))
        .build();

    assertEquals(
        channelProto
            .toBuilder()
            .addSubchannelRef(subchannelRef)
            .build(),
        ChannelzProtoUtil.toChannel(channel));

    TestChannel otherSubchannel = new TestChannel();
    channel.stats = toBuilder(channel.stats)
        .setSubchannels(ImmutableList.<InternalWithLogId>of(subchannel, otherSubchannel))
        .build();
    assertEquals(
        channelProto
            .toBuilder()
            .addSubchannelRef(subchannelRef)
            .addSubchannelRef(ChannelzProtoUtil.toSubchannelRef(otherSubchannel))
            .build(),
        ChannelzProtoUtil.toChannel(channel));
  }

  @Test
  public void extractChannelData() {
    assertEquals(channelData, ChannelzProtoUtil.extractChannelData(channel.stats));
  }

  @Test
  public void toSubchannel_noChildren() throws Exception {
    assertEquals(
        subchannelProto,
        ChannelzProtoUtil.toSubchannel(subchannel));
  }

  @Test
  public void toSubchannel_socketChildren() throws Exception {
    subchannel.stats = toBuilder(subchannel.stats)
        .setSockets(ImmutableList.<InternalWithLogId>of(socket))
        .build();

    assertEquals(
        subchannelProto.toBuilder()
            .addSocketRef(socketRef)
            .build(),
        ChannelzProtoUtil.toSubchannel(subchannel));

    TestSocket otherSocket = new TestSocket();
    subchannel.stats = toBuilder(subchannel.stats)
        .setSockets(ImmutableList.<InternalWithLogId>of(socket, otherSocket))
        .build();
    assertEquals(
        subchannelProto
            .toBuilder()
            .addSocketRef(socketRef)
            .addSocketRef(ChannelzProtoUtil.toSocketRef(otherSocket))
            .build(),
        ChannelzProtoUtil.toSubchannel(subchannel));
  }

  @Test
  public void toSubchannel_subchannelChildren() throws Exception {
    TestChannel subchannel1 = new TestChannel();
    subchannel.stats = toBuilder(subchannel.stats)
        .setSubchannels(ImmutableList.<InternalWithLogId>of(subchannel1))
        .build();
    assertEquals(
        subchannelProto.toBuilder()
            .addSubchannelRef(ChannelzProtoUtil.toSubchannelRef(subchannel1))
            .build(),
        ChannelzProtoUtil.toSubchannel(subchannel));

    TestChannel subchannel2 = new TestChannel();
    subchannel.stats = toBuilder(subchannel.stats)
        .setSubchannels(ImmutableList.<InternalWithLogId>of(subchannel1, subchannel2))
        .build();
    assertEquals(
        subchannelProto
            .toBuilder()
            .addSubchannelRef(ChannelzProtoUtil.toSubchannelRef(subchannel1))
            .addSubchannelRef(ChannelzProtoUtil.toSubchannelRef(subchannel2))
            .build(),
        ChannelzProtoUtil.toSubchannel(subchannel));
  }

  @Test
  public void toGetTopChannelsResponse() {
    // empty results
    assertEquals(
        GetTopChannelsResponse.newBuilder().setEnd(true).build(),
        ChannelzProtoUtil.toGetTopChannelResponse(
            new RootChannelList(
                Collections.<InternalInstrumented<ChannelStats>>emptyList(), true)));

    // 1 result, paginated
    assertEquals(
        GetTopChannelsResponse
            .newBuilder()
            .addChannel(channelProto)
            .build(),
        ChannelzProtoUtil.toGetTopChannelResponse(
            new RootChannelList(
                ImmutableList.<InternalInstrumented<ChannelStats>>of(channel), false)));

    // 1 result, end
    assertEquals(
        GetTopChannelsResponse
            .newBuilder()
            .addChannel(channelProto)
            .setEnd(true)
            .build(),
        ChannelzProtoUtil.toGetTopChannelResponse(
            new RootChannelList(
                ImmutableList.<InternalInstrumented<ChannelStats>>of(channel), true)));

    // 2 results, end
    TestChannel channel2 = new TestChannel();
    assertEquals(
        GetTopChannelsResponse
            .newBuilder()
            .addChannel(channelProto)
            .addChannel(ChannelzProtoUtil.toChannel(channel2))
            .setEnd(true)
            .build(),
        ChannelzProtoUtil.toGetTopChannelResponse(
            new RootChannelList(
                ImmutableList.<InternalInstrumented<ChannelStats>>of(channel, channel2), true)));
  }

  @Test
  public void toGetServersResponse() {
    // empty results
    assertEquals(
        GetServersResponse.getDefaultInstance(),
        ChannelzProtoUtil.toGetServersResponse(
            new ServerList(Collections.<InternalInstrumented<ServerStats>>emptyList(), false)));

    // 1 result, paginated
    assertEquals(
        GetServersResponse
            .newBuilder()
            .addServer(serverProto)
            .build(),
        ChannelzProtoUtil.toGetServersResponse(
            new ServerList(ImmutableList.<InternalInstrumented<ServerStats>>of(server), false)));

    // 1 result, end
    assertEquals(
        GetServersResponse
            .newBuilder()
            .addServer(serverProto)
            .setEnd(true)
            .build(),
        ChannelzProtoUtil.toGetServersResponse(
            new ServerList(ImmutableList.<InternalInstrumented<ServerStats>>of(server), true)));

    TestServer server2 = new TestServer();
    // 2 results, end
    assertEquals(
        GetServersResponse
            .newBuilder()
            .addServer(serverProto)
            .addServer(ChannelzProtoUtil.toServer(server2))
            .setEnd(true)
            .build(),
        ChannelzProtoUtil.toGetServersResponse(
            new ServerList(
                ImmutableList.<InternalInstrumented<ServerStats>>of(server, server2), true)));
  }

  @Test
  public void toGetServerSocketsResponse() {
    // empty results
    assertEquals(
        GetServerSocketsResponse.getDefaultInstance(),
        ChannelzProtoUtil.toGetServerSocketsResponse(
            new ServerSocketsList(Collections.<InternalWithLogId>emptyList(), false)));

    // 1 result, paginated
    assertEquals(
        GetServerSocketsResponse
            .newBuilder()
            .addSocketRef(socketRef)
            .build(),
        ChannelzProtoUtil.toGetServerSocketsResponse(
            new ServerSocketsList(ImmutableList.<InternalWithLogId>of(socket), false)));

    // 1 result, end
    assertEquals(
        GetServerSocketsResponse
            .newBuilder()
            .addSocketRef(socketRef)
            .setEnd(true)
            .build(),
        ChannelzProtoUtil.toGetServerSocketsResponse(
            new ServerSocketsList(ImmutableList.<InternalWithLogId>of(socket), true)));

    TestSocket socket2 = new TestSocket();
    // 2 results, end
    assertEquals(
        GetServerSocketsResponse
            .newBuilder()
            .addSocketRef(socketRef)
            .addSocketRef(ChannelzProtoUtil.toSocketRef(socket2))
            .setEnd(true)
            .build(),
        ChannelzProtoUtil.toGetServerSocketsResponse(
            new ServerSocketsList(ImmutableList.<InternalWithLogId>of(socket, socket2), true)));
  }

  @Test
  public void toSocketOptionLinger() {
    assertEquals(sockOptLingerDisabled, ChannelzProtoUtil.toSocketOptionLinger(-1));
    assertEquals(sockOptlinger10s, ChannelzProtoUtil.toSocketOptionLinger(10));
  }

  @Test
  public void toSocketOptionTimeout() {
    assertEquals(
        sockOptTimeout200ms, ChannelzProtoUtil.toSocketOptionTimeout("SO_TIMEOUT", 200));
  }

  @Test
  public void toSocketOptionAdditional() {
    assertEquals(
        sockOptAdditional,
        ChannelzProtoUtil.toSocketOptionAdditional("SO_MADE_UP_OPTION", "some-made-up-value"));
  }

  @Test
  public void toSocketOptionTcpInfo() {
    assertEquals(
        socketOptionTcpInfo,
        ChannelzProtoUtil.toSocketOptionTcpInfo(channelzTcpInfo));
  }

  @Test
  public void toSocketOptionsList() {
    assertThat(
        ChannelzProtoUtil.toSocketOptionsList(
            new InternalChannelz.SocketOptions.Builder().build()))
        .isEmpty();

    assertThat(
        ChannelzProtoUtil.toSocketOptionsList(
            new InternalChannelz.SocketOptions.Builder().setSocketOptionLingerSeconds(10).build()))
        .containsExactly(sockOptlinger10s);

    assertThat(
        ChannelzProtoUtil.toSocketOptionsList(
            new InternalChannelz.SocketOptions.Builder().setSocketOptionTimeoutMillis(200).build()))
        .containsExactly(sockOptTimeout200ms);

    assertThat(
        ChannelzProtoUtil.toSocketOptionsList(
            new InternalChannelz.SocketOptions
                .Builder()
                .addOption("SO_MADE_UP_OPTION", "some-made-up-value")
                .build()))
        .containsExactly(sockOptAdditional);

    SocketOption otherOption = SocketOption
        .newBuilder()
        .setName("SO_MADE_UP_OPTION2")
        .setValue("some-made-up-value2")
        .build();
    assertThat(
        ChannelzProtoUtil.toSocketOptionsList(
            new InternalChannelz.SocketOptions.Builder()
                .addOption("SO_MADE_UP_OPTION", "some-made-up-value")
                .addOption("SO_MADE_UP_OPTION2", "some-made-up-value2")
                .build()))
        .containsExactly(sockOptAdditional, otherOption);
  }

  @Test
  public void channelTrace_withoutEvents() {
    ChannelStats stats = toBuilder(channel.stats)
        .setChannelTrace(new InternalChannelz.ChannelTrace.Builder()
            .setNumEventsLogged(1234)
            .setCreationTimeNanos(1000)
            .build())
        .build();

    ChannelData protoStats = channelData.toBuilder().setTrace(channelTrace).build();
    assertEquals(ChannelzProtoUtil.extractChannelData(stats), protoStats);
  }

  @Test
  public void channelTrace_withEvents() {
    Event event1 = new Event.Builder()
        .setDescription("event1")
        .setSeverity(Severity.CT_ERROR)
        .setTimestampNanos(12)
        .setSubchannelRef(subchannel)
        .build();
    Event event2 = new Event.Builder()
        .setDescription("event2")
        .setTimestampNanos(34)
        .setSeverity(Severity.CT_INFO)
        .setChannelRef(channel)
        .build();

    ChannelStats stats =
        toBuilder(channel.stats)
            .setChannelTrace(
                new InternalChannelz.ChannelTrace.Builder()
                    .setNumEventsLogged(1234)
                    .setCreationTimeNanos(1000)
                    .setEvents(Arrays.asList(event1, event2))
                    .build())
            .build();

    ChannelTraceEvent protoEvent1 = ChannelTraceEvent
        .newBuilder()
        .setDescription("event1")
        .setTimestamp(Timestamps.fromNanos(12))
        .setSeverity(ChannelTraceEvent.Severity.CT_ERROR)
        .setSubchannelRef(subchannelRef)
        .build();
    ChannelTraceEvent protoEvent2 = ChannelTraceEvent
        .newBuilder()
        .setDescription("event2")
        .setTimestamp(Timestamps.fromNanos(34))
        .setSeverity(ChannelTraceEvent.Severity.CT_INFO)
        .setChannelRef(channelRef)
        .build();
    ChannelData protoStats = channelData
        .toBuilder()
        .setTrace(channelTrace
            .toBuilder()
            .addAllEvents(Arrays.asList(protoEvent1, protoEvent2))
            .build())
        .build();
    assertEquals(ChannelzProtoUtil.extractChannelData(stats), protoStats);
  }

  private static ChannelStats.Builder toBuilder(ChannelStats stats) {
    ChannelStats.Builder builder = new ChannelStats.Builder()
        .setTarget(stats.target)
        .setState(stats.state)
        .setCallsStarted(stats.callsStarted)
        .setCallsSucceeded(stats.callsSucceeded)
        .setCallsFailed(stats.callsFailed)
        .setLastCallStartedNanos(stats.lastCallStartedNanos);
    if (!stats.subchannels.isEmpty()) {
      builder.setSubchannels(stats.subchannels);
    }
    if (!stats.sockets.isEmpty()) {
      builder.setSockets(stats.sockets);
    }
    return builder;
  }


  private static SocketOptions.Builder toBuilder(SocketOptions options) {
    SocketOptions.Builder builder = new SocketOptions.Builder()
        .setSocketOptionTimeoutMillis(options.soTimeoutMillis)
        .setSocketOptionLingerSeconds(options.lingerSeconds);
    for (Entry<String, String> entry : options.others.entrySet()) {
      builder.addOption(entry.getKey(), entry.getValue());
    }
    return builder;
  }

  private static ServerStats.Builder toBuilder(ServerStats stats) {
    return new ServerStats.Builder()
        .setCallsStarted(stats.callsStarted)
        .setCallsSucceeded(stats.callsSucceeded)
        .setCallsFailed(stats.callsFailed)
        .setLastCallStartedNanos(stats.lastCallStartedNanos)
        .addListenSockets(stats.listenSockets);
  }
}
