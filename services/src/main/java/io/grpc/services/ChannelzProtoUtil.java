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

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.protobuf.Any;
import com.google.protobuf.ByteString;
import com.google.protobuf.Int64Value;
import com.google.protobuf.util.Durations;
import com.google.protobuf.util.Timestamps;
import io.grpc.ConnectivityState;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace.Event;
import io.grpc.InternalChannelz.RootChannelList;
import io.grpc.InternalChannelz.ServerList;
import io.grpc.InternalChannelz.ServerSocketsList;
import io.grpc.InternalChannelz.ServerStats;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalChannelz.TransportStats;
import io.grpc.InternalInstrumented;
import io.grpc.InternalWithLogId;
import io.grpc.Status;
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
import io.grpc.channelz.v1.ChannelTraceEvent.Severity;
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
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.security.cert.CertificateEncodingException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map.Entry;
import java.util.concurrent.ExecutionException;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A static utility class for turning internal data structures into protos.
 */
final class ChannelzProtoUtil {
  private static final Logger logger = Logger.getLogger(ChannelzProtoUtil.class.getName());

  private ChannelzProtoUtil() {
    // do not instantiate.
  }

  static ChannelRef toChannelRef(InternalWithLogId obj) {
    return ChannelRef
        .newBuilder()
        .setChannelId(obj.getLogId().getId())
        .setName(obj.toString())
        .build();
  }

  static SubchannelRef toSubchannelRef(InternalWithLogId obj) {
    return SubchannelRef
        .newBuilder()
        .setSubchannelId(obj.getLogId().getId())
        .setName(obj.toString())
        .build();
  }

  static ServerRef toServerRef(InternalWithLogId obj) {
    return ServerRef
        .newBuilder()
        .setServerId(obj.getLogId().getId())
        .setName(obj.toString())
        .build();
  }

  static SocketRef toSocketRef(InternalWithLogId obj) {
    return SocketRef
        .newBuilder()
        .setSocketId(obj.getLogId().getId())
        .setName(obj.toString())
        .build();
  }

  static Server toServer(InternalInstrumented<ServerStats> obj) {
    ServerStats stats = getFuture(obj.getStats());
    Server.Builder builder = Server
        .newBuilder()
        .setRef(toServerRef(obj))
        .setData(toServerData(stats));
    for (InternalInstrumented<SocketStats> listenSocket : stats.listenSockets) {
      builder.addListenSocket(toSocketRef(listenSocket));
    }
    return builder.build();
  }

  static ServerData toServerData(ServerStats stats) {
    return ServerData
        .newBuilder()
        .setCallsStarted(stats.callsStarted)
        .setCallsSucceeded(stats.callsSucceeded)
        .setCallsFailed(stats.callsFailed)
        .setLastCallStartedTimestamp(Timestamps.fromNanos(stats.lastCallStartedNanos))
        .build();
  }

  static Security toSecurity(InternalChannelz.Security security) {
    Preconditions.checkNotNull(security);
    Preconditions.checkState(
        security.tls != null ^ security.other != null,
        "one of tls or othersecurity must be non null");
    if (security.tls != null) {
      Tls.Builder tlsBuilder
          = Tls.newBuilder().setStandardName(security.tls.cipherSuiteStandardName);
      try {
        if (security.tls.localCert != null) {
          tlsBuilder.setLocalCertificate(ByteString.copyFrom(
              security.tls.localCert.getEncoded()));
        }
        if (security.tls.remoteCert != null) {
          tlsBuilder.setRemoteCertificate(ByteString.copyFrom(
              security.tls.remoteCert.getEncoded()));
        }
      } catch (CertificateEncodingException e) {
        logger.log(Level.FINE, "Caught exception", e);
      }
      return Security.newBuilder().setTls(tlsBuilder).build();
    } else {
      OtherSecurity.Builder builder = OtherSecurity.newBuilder().setName(security.other.name);
      if (security.other.any != null) {
        builder.setValue((Any) security.other.any);
      }
      return Security.newBuilder().setOther(builder).build();
    }
  }

  static Socket toSocket(InternalInstrumented<SocketStats> obj) {
    SocketStats socketStats = getFuture(obj.getStats());
    Socket.Builder builder = Socket.newBuilder()
        .setRef(toSocketRef(obj))
        .setLocal(toAddress(socketStats.local));
    if (socketStats.security != null) {
      builder.setSecurity(toSecurity(socketStats.security));
    }
    // listen sockets do not have remote nor data
    if (socketStats.remote != null) {
      builder.setRemote(toAddress(socketStats.remote));
    }
    builder.setData(extractSocketData(socketStats));
    return builder.build();
  }

  static Address toAddress(SocketAddress address) {
    Preconditions.checkNotNull(address);
    Address.Builder builder = Address.newBuilder();
    if (address instanceof InetSocketAddress) {
      InetSocketAddress inetAddress = (InetSocketAddress) address;
      builder.setTcpipAddress(
          TcpIpAddress
              .newBuilder()
              .setIpAddress(
                  ByteString.copyFrom(inetAddress.getAddress().getAddress()))
              .setPort(inetAddress.getPort())
              .build());
    } else if (address.getClass().getName().endsWith("io.netty.channel.unix.DomainSocketAddress")) {
      builder.setUdsAddress(
          UdsAddress
              .newBuilder()
              .setFilename(address.toString()) // DomainSocketAddress.toString returns filename
              .build());
    } else {
      builder.setOtherAddress(OtherAddress.newBuilder().setName(address.toString()).build());
    }
    return builder.build();
  }

  static SocketData extractSocketData(SocketStats socketStats) {
    SocketData.Builder builder = SocketData.newBuilder();
    if (socketStats.data != null) {
      TransportStats s = socketStats.data;
      builder
          .setStreamsStarted(s.streamsStarted)
          .setStreamsSucceeded(s.streamsSucceeded)
          .setStreamsFailed(s.streamsFailed)
          .setMessagesSent(s.messagesSent)
          .setMessagesReceived(s.messagesReceived)
          .setKeepAlivesSent(s.keepAlivesSent)
          .setLastLocalStreamCreatedTimestamp(
              Timestamps.fromNanos(s.lastLocalStreamCreatedTimeNanos))
          .setLastRemoteStreamCreatedTimestamp(
              Timestamps.fromNanos(s.lastRemoteStreamCreatedTimeNanos))
          .setLastMessageSentTimestamp(
              Timestamps.fromNanos(s.lastMessageSentTimeNanos))
          .setLastMessageReceivedTimestamp(
              Timestamps.fromNanos(s.lastMessageReceivedTimeNanos))
          .setLocalFlowControlWindow(
              Int64Value.of(s.localFlowControlWindow))
          .setRemoteFlowControlWindow(
              Int64Value.of(s.remoteFlowControlWindow));
    }
    builder.addAllOption(toSocketOptionsList(socketStats.socketOptions));
    return builder.build();
  }

  public static final String SO_LINGER = "SO_LINGER";
  public static final String SO_TIMEOUT = "SO_TIMEOUT";
  public static final String TCP_INFO = "TCP_INFO";

  static SocketOption toSocketOptionLinger(int lingerSeconds) {
    final SocketOptionLinger lingerOpt;
    if (lingerSeconds >= 0) {
      lingerOpt = SocketOptionLinger
          .newBuilder()
          .setActive(true)
          .setDuration(Durations.fromSeconds(lingerSeconds))
          .build();
    } else {
      lingerOpt = SocketOptionLinger.getDefaultInstance();
    }
    return SocketOption
        .newBuilder()
        .setName(SO_LINGER)
        .setAdditional(Any.pack(lingerOpt))
        .build();
  }

  static SocketOption toSocketOptionTimeout(String name, int timeoutMillis) {
    Preconditions.checkNotNull(name);
    return SocketOption
        .newBuilder()
        .setName(name)
        .setAdditional(
            Any.pack(
                SocketOptionTimeout
                    .newBuilder()
                    .setDuration(Durations.fromMillis(timeoutMillis))
                    .build()))
        .build();
  }

  static SocketOption toSocketOptionTcpInfo(InternalChannelz.TcpInfo i) {
    SocketOptionTcpInfo tcpInfo = SocketOptionTcpInfo.newBuilder()
        .setTcpiState(i.state)
        .setTcpiCaState(i.caState)
        .setTcpiRetransmits(i.retransmits)
        .setTcpiProbes(i.probes)
        .setTcpiBackoff(i.backoff)
        .setTcpiOptions(i.options)
        .setTcpiSndWscale(i.sndWscale)
        .setTcpiRcvWscale(i.rcvWscale)
        .setTcpiRto(i.rto)
        .setTcpiAto(i.ato)
        .setTcpiSndMss(i.sndMss)
        .setTcpiRcvMss(i.rcvMss)
        .setTcpiUnacked(i.unacked)
        .setTcpiSacked(i.sacked)
        .setTcpiLost(i.lost)
        .setTcpiRetrans(i.retrans)
        .setTcpiFackets(i.fackets)
        .setTcpiLastDataSent(i.lastDataSent)
        .setTcpiLastAckSent(i.lastAckSent)
        .setTcpiLastDataRecv(i.lastDataRecv)
        .setTcpiLastAckRecv(i.lastAckRecv)
        .setTcpiPmtu(i.pmtu)
        .setTcpiRcvSsthresh(i.rcvSsthresh)
        .setTcpiRtt(i.rtt)
        .setTcpiRttvar(i.rttvar)
        .setTcpiSndSsthresh(i.sndSsthresh)
        .setTcpiSndCwnd(i.sndCwnd)
        .setTcpiAdvmss(i.advmss)
        .setTcpiReordering(i.reordering)
        .build();
    return SocketOption
        .newBuilder()
        .setName(TCP_INFO)
        .setAdditional(Any.pack(tcpInfo))
        .build();
  }

  static SocketOption toSocketOptionAdditional(String name, String value) {
    Preconditions.checkNotNull(name);
    Preconditions.checkNotNull(value);
    return SocketOption.newBuilder().setName(name).setValue(value).build();
  }

  static List<SocketOption> toSocketOptionsList(InternalChannelz.SocketOptions options) {
    Preconditions.checkNotNull(options);
    List<SocketOption> ret = new ArrayList<>();
    if (options.lingerSeconds != null) {
      ret.add(toSocketOptionLinger(options.lingerSeconds));
    }
    if (options.soTimeoutMillis != null) {
      ret.add(toSocketOptionTimeout(SO_TIMEOUT, options.soTimeoutMillis));
    }
    if (options.tcpInfo != null) {
      ret.add(toSocketOptionTcpInfo(options.tcpInfo));
    }
    for (Entry<String, String> entry : options.others.entrySet()) {
      ret.add(toSocketOptionAdditional(entry.getKey(), entry.getValue()));
    }
    return ret;
  }

  static Channel toChannel(InternalInstrumented<ChannelStats> channel) {
    ChannelStats stats = getFuture(channel.getStats());
    Channel.Builder channelBuilder = Channel
        .newBuilder()
        .setRef(toChannelRef(channel))
        .setData(extractChannelData(stats));
    for (InternalWithLogId subchannel : stats.subchannels) {
      channelBuilder.addSubchannelRef(toSubchannelRef(subchannel));
    }

    return channelBuilder.build();
  }

  static ChannelData extractChannelData(InternalChannelz.ChannelStats stats) {
    ChannelData.Builder builder = ChannelData.newBuilder();
    builder.setTarget(stats.target)
        .setState(toChannelConnectivityState(stats.state))
        .setCallsStarted(stats.callsStarted)
        .setCallsSucceeded(stats.callsSucceeded)
        .setCallsFailed(stats.callsFailed)
        .setLastCallStartedTimestamp(Timestamps.fromNanos(stats.lastCallStartedNanos));
    if (stats.channelTrace != null) {
      builder.setTrace(toChannelTrace(stats.channelTrace));
    }
    return builder.build();
  }

  static ChannelConnectivityState toChannelConnectivityState(ConnectivityState s) {
    return ChannelConnectivityState.newBuilder().setState(toState(s)).build();
  }

  private static ChannelTrace toChannelTrace(InternalChannelz.ChannelTrace channelTrace) {
    return ChannelTrace.newBuilder()
        .setNumEventsLogged(channelTrace.numEventsLogged)
        .setCreationTimestamp(Timestamps.fromNanos(channelTrace.creationTimeNanos))
        .addAllEvents(toChannelTraceEvents(channelTrace.events))
        .build();
  }

  private static List<ChannelTraceEvent> toChannelTraceEvents(List<Event> events) {
    List<ChannelTraceEvent> channelTraceEvents = new ArrayList<>();
    for (Event event : events) {
      ChannelTraceEvent.Builder builder = ChannelTraceEvent.newBuilder()
          .setDescription(event.description)
          .setSeverity(Severity.valueOf(event.severity.name()))
          .setTimestamp(Timestamps.fromNanos(event.timestampNanos));
      if (event.channelRef != null) {
        builder.setChannelRef(toChannelRef(event.channelRef));
      }
      if (event.subchannelRef != null) {
        builder.setSubchannelRef(toSubchannelRef(event.subchannelRef));
      }
      channelTraceEvents.add(builder.build());
    }
    return Collections.unmodifiableList(channelTraceEvents);
  }

  static State toState(ConnectivityState state) {
    if (state == null) {
      return State.UNKNOWN;
    }
    try {
      return Enum.valueOf(State.class, state.name());
    } catch (IllegalArgumentException e) {
      return State.UNKNOWN;
    }
  }

  static Subchannel toSubchannel(InternalInstrumented<ChannelStats> subchannel) {
    ChannelStats stats = getFuture(subchannel.getStats());
    Subchannel.Builder subchannelBuilder = Subchannel
        .newBuilder()
        .setRef(toSubchannelRef(subchannel))
        .setData(extractChannelData(stats));
    Preconditions.checkState(stats.sockets.isEmpty() || stats.subchannels.isEmpty());
    for (InternalWithLogId childSocket : stats.sockets) {
      subchannelBuilder.addSocketRef(toSocketRef(childSocket));
    }
    for (InternalWithLogId childSubchannel : stats.subchannels) {
      subchannelBuilder.addSubchannelRef(toSubchannelRef(childSubchannel));
    }
    return subchannelBuilder.build();
  }

  static GetTopChannelsResponse toGetTopChannelResponse(RootChannelList rootChannels) {
    GetTopChannelsResponse.Builder responseBuilder = GetTopChannelsResponse
        .newBuilder()
        .setEnd(rootChannels.end);
    for (InternalInstrumented<ChannelStats> c : rootChannels.channels) {
      responseBuilder.addChannel(ChannelzProtoUtil.toChannel(c));
    }
    return responseBuilder.build();
  }

  static GetServersResponse toGetServersResponse(ServerList servers) {
    GetServersResponse.Builder responseBuilder = GetServersResponse
        .newBuilder()
        .setEnd(servers.end);
    for (InternalInstrumented<ServerStats> s : servers.servers) {
      responseBuilder.addServer(ChannelzProtoUtil.toServer(s));
    }
    return responseBuilder.build();
  }

  static GetServerSocketsResponse toGetServerSocketsResponse(ServerSocketsList serverSockets) {
    GetServerSocketsResponse.Builder responseBuilder = GetServerSocketsResponse
        .newBuilder()
        .setEnd(serverSockets.end);
    for (InternalWithLogId s : serverSockets.sockets) {
      responseBuilder.addSocketRef(ChannelzProtoUtil.toSocketRef(s));
    }
    return responseBuilder.build();
  }

  private static <T> T getFuture(ListenableFuture<T> future) {
    try {
      T ret = future.get();
      if (ret == null) {
        throw Status.UNIMPLEMENTED
            .withDescription("The entity's stats can not be retrieved. "
                + "If this is an InProcessTransport this is expected.")
            .asRuntimeException();
      }
      return ret;
    } catch (InterruptedException e) {
      throw Status.INTERNAL.withCause(e).asRuntimeException();
    } catch (ExecutionException e) {
      throw Status.INTERNAL.withCause(e).asRuntimeException();
    }
  }
}
