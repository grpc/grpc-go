/*
 * Copyright 2018, gRPC Authors All rights reserved.
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
import com.google.protobuf.ByteString;
import com.google.protobuf.Int64Value;
import com.google.protobuf.util.Timestamps;
import io.grpc.ConnectivityState;
import io.grpc.Status;
import io.grpc.channelz.v1.Address;
import io.grpc.channelz.v1.Address.OtherAddress;
import io.grpc.channelz.v1.Address.TcpIpAddress;
import io.grpc.channelz.v1.Address.UdsAddress;
import io.grpc.channelz.v1.Channel;
import io.grpc.channelz.v1.ChannelData;
import io.grpc.channelz.v1.ChannelData.State;
import io.grpc.channelz.v1.ChannelRef;
import io.grpc.channelz.v1.GetServerSocketsResponse;
import io.grpc.channelz.v1.GetServersResponse;
import io.grpc.channelz.v1.GetTopChannelsResponse;
import io.grpc.channelz.v1.Server;
import io.grpc.channelz.v1.ServerData;
import io.grpc.channelz.v1.ServerRef;
import io.grpc.channelz.v1.Socket;
import io.grpc.channelz.v1.SocketData;
import io.grpc.channelz.v1.SocketRef;
import io.grpc.channelz.v1.Subchannel;
import io.grpc.channelz.v1.SubchannelRef;
import io.grpc.internal.Channelz;
import io.grpc.internal.Channelz.ChannelStats;
import io.grpc.internal.Channelz.RootChannelList;
import io.grpc.internal.Channelz.ServerList;
import io.grpc.internal.Channelz.ServerSocketsList;
import io.grpc.internal.Channelz.ServerStats;
import io.grpc.internal.Channelz.SocketStats;
import io.grpc.internal.Channelz.TransportStats;
import io.grpc.internal.Instrumented;
import io.grpc.internal.WithLogId;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.ExecutionException;

/**
 * A static utility class for turning internal data structures into protos.
 */
final class ChannelzProtoUtil {
  private ChannelzProtoUtil() {
    // do not instantiate.
  }

  static ChannelRef toChannelRef(WithLogId obj) {
    return ChannelRef
        .newBuilder()
        .setChannelId(obj.getLogId().getId())
        .setName(obj.toString())
        .build();
  }

  static SubchannelRef toSubchannelRef(WithLogId obj) {
    return SubchannelRef
        .newBuilder()
        .setSubchannelId(obj.getLogId().getId())
        .setName(obj.toString())
        .build();
  }

  static ServerRef toServerRef(WithLogId obj) {
    return ServerRef
        .newBuilder()
        .setServerId(obj.getLogId().getId())
        .setName(obj.toString())
        .build();
  }

  static SocketRef toSocketRef(WithLogId obj) {
    return SocketRef
        .newBuilder()
        .setSocketId(obj.getLogId().getId())
        .setName(obj.toString())
        .build();
  }

  static Server toServer(Instrumented<ServerStats> obj) {
    ServerStats stats = getFuture(obj.getStats());
    return Server
        .newBuilder()
        .setRef(toServerRef(obj))
        .setData(toServerData(stats))
        .build();
  }

  static ServerData toServerData(ServerStats stats) {
    return ServerData
        .newBuilder()
        .setCallsStarted(stats.callsStarted)
        .setCallsSucceeded(stats.callsSucceeded)
        .setCallsFailed(stats.callsFailed)
        .setLastCallStartedTimestamp(Timestamps.fromMillis(stats.lastCallStartedMillis))
        .build();
  }

  static Socket toSocket(Instrumented<SocketStats> obj) {
    SocketStats socketStats = getFuture(obj.getStats());
    return Socket.newBuilder()
        .setRef(toSocketRef(obj))
        .setRemote(toAddress(socketStats.remote))
        .setLocal(toAddress(socketStats.local))
        .setData(toSocketData(socketStats.data))
        .build();
  }

  static Address toAddress(SocketAddress address) {
    Address.Builder builder = Address.newBuilder();
    if (address instanceof InetSocketAddress) {
      InetSocketAddress inetAddress = (InetSocketAddress) address;
      builder.setTcpipAddress(
          TcpIpAddress
              .newBuilder()
              .setIpAddress(
                  ByteString.copyFrom(inetAddress.getAddress().getAddress()))
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

  static SocketData toSocketData(TransportStats s) {
    return SocketData
        .newBuilder()
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
            Int64Value.newBuilder().setValue(s.localFlowControlWindow).build())
        .setRemoteFlowControlWindow(
            Int64Value.newBuilder().setValue(s.remoteFlowControlWindow).build())
        .build();
  }

  static Channel toChannel(Instrumented<ChannelStats> channel) {
    ChannelStats stats = getFuture(channel.getStats());
    Channel.Builder channelBuilder = Channel
        .newBuilder()
        .setRef(toChannelRef(channel))
        .setData(extractChannelData(stats));
    for (WithLogId subchannel : stats.subchannels) {
      channelBuilder.addSubchannelRef(toSubchannelRef(subchannel));
    }

    return channelBuilder.build();
  }

  static ChannelData extractChannelData(Channelz.ChannelStats stats) {
    return ChannelData
        .newBuilder()
        .setTarget(stats.target)
        .setState(toState(stats.state))
        .setCallsStarted(stats.callsStarted)
        .setCallsSucceeded(stats.callsSucceeded)
        .setCallsFailed(stats.callsFailed)
        .setLastCallStartedTimestamp(Timestamps.fromMillis(stats.lastCallStartedMillis))
        .build();
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

  static Subchannel toSubchannel(Instrumented<ChannelStats> subchannel) {
    ChannelStats stats = getFuture(subchannel.getStats());
    Subchannel.Builder subchannelBuilder = Subchannel
        .newBuilder()
        .setRef(toSubchannelRef(subchannel))
        .setData(extractChannelData(stats));
    Preconditions.checkState(stats.sockets.isEmpty() || stats.subchannels.isEmpty());
    for (WithLogId childSocket : stats.sockets) {
      subchannelBuilder.addSocketRef(toSocketRef(childSocket));
    }
    for (WithLogId childSubchannel : stats.subchannels) {
      subchannelBuilder.addSubchannelRef(toSubchannelRef(childSubchannel));
    }
    return subchannelBuilder.build();
  }

  static GetTopChannelsResponse toGetTopChannelResponse(RootChannelList rootChannels) {
    GetTopChannelsResponse.Builder responseBuilder = GetTopChannelsResponse
        .newBuilder()
        .setEnd(rootChannels.end);
    for (Instrumented<ChannelStats> c : rootChannels.channels) {
      responseBuilder.addChannel(ChannelzProtoUtil.toChannel(c));
    }
    return responseBuilder.build();
  }

  static GetServersResponse toGetServersResponse(ServerList servers) {
    GetServersResponse.Builder responseBuilder = GetServersResponse
        .newBuilder()
        .setEnd(servers.end);
    for (Instrumented<ServerStats> s : servers.servers) {
      responseBuilder.addServer(ChannelzProtoUtil.toServer(s));
    }
    return responseBuilder.build();
  }

  static GetServerSocketsResponse toGetServerSocketsResponse(ServerSocketsList serverSockets) {
    GetServerSocketsResponse.Builder responseBuilder = GetServerSocketsResponse
        .newBuilder()
        .setEnd(serverSockets.end);
    for (WithLogId s : serverSockets.sockets) {
      responseBuilder.addSocketRef(ChannelzProtoUtil.toSocketRef(s));
    }
    return responseBuilder.build();
  }

  private static <T> T getFuture(ListenableFuture<T> future) {
    try {
      return future.get();
    } catch (InterruptedException e) {
      throw Status.INTERNAL.withCause(e).asRuntimeException();
    } catch (ExecutionException e) {
      throw Status.INTERNAL.withCause(e).asRuntimeException();
    }
  }
}
