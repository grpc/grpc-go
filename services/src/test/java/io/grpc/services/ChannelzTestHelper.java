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

import com.google.common.base.MoreObjects;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.ConnectivityState;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.Security;
import io.grpc.InternalChannelz.ServerStats;
import io.grpc.InternalChannelz.SocketOptions;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalChannelz.TransportStats;
import io.grpc.InternalInstrumented;
import io.grpc.InternalLogId;
import io.grpc.InternalWithLogId;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.Collections;

/**
 * Test class definitions that will be used in the proto utils test as well as
 * channelz service test.
 */
final class ChannelzTestHelper {

  static final class TestSocket implements InternalInstrumented<SocketStats> {
    private final InternalLogId id = InternalLogId.allocate("socket", /*details=*/ null);
    TransportStats transportStats = new TransportStats(
        /*streamsStarted=*/ 1,
        /*lastLocalStreamCreatedTimeNanos=*/ 2,
        /*lastRemoteStreamCreatedTimeNanos=*/ 3,
        /*streamsSucceeded=*/ 4,
        /*streamsFailed=*/ 5,
        /*messagesSent=*/ 6,
        /*messagesReceived=*/ 7,
        /*keepAlivesSent=*/ 8,
        /*lastMessageSentTimeNanos=*/ 9,
        /*lastMessageReceivedTimeNanos=*/ 10,
        /*localFlowControlWindow=*/ 11,
        /*remoteFlowControlWindow=*/ 12);
    SocketAddress local = new InetSocketAddress("10.0.0.1", 1000);
    SocketAddress remote = new InetSocketAddress("10.0.0.2", 1000);
    InternalChannelz.SocketOptions socketOptions
        = new InternalChannelz.SocketOptions.Builder().build();
    Security security = null;

    @Override
    public ListenableFuture<SocketStats> getStats() {
      SettableFuture<SocketStats> ret = SettableFuture.create();
      ret.set(
          new SocketStats(
              transportStats,
              local,
              remote,
              socketOptions,
              security));
      return ret;
    }

    @Override
    public InternalLogId getLogId() {
      return id;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("logId", getLogId())
          .toString();
    }
  }

  static final class TestListenSocket implements InternalInstrumented<SocketStats> {
    private final InternalLogId id = InternalLogId.allocate("listensocket", /*details=*/ null);
    SocketAddress listenAddress = new InetSocketAddress("10.0.0.1", 1234);

    @Override
    public ListenableFuture<SocketStats> getStats() {
      SettableFuture<SocketStats> ret = SettableFuture.create();
      ret.set(
          new SocketStats(
              /*data=*/ null,
              listenAddress,
              /*remote=*/ null,
              new SocketOptions.Builder().addOption("listen_option", "listen_option_value").build(),
              /*security=*/ null));
      return ret;
    }

    @Override
    public InternalLogId getLogId() {
      return id;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("logId", getLogId())
          .toString();
    }
  }

  static final class TestServer implements InternalInstrumented<ServerStats> {
    private final InternalLogId id = InternalLogId.allocate("server", /*details=*/ null);
    ServerStats serverStats = new ServerStats(
        /*callsStarted=*/ 1,
        /*callsSucceeded=*/ 2,
        /*callsFailed=*/ 3,
        /*lastCallStartedNanos=*/ 4,
        Collections.<InternalInstrumented<SocketStats>>emptyList());

    @Override
    public ListenableFuture<ServerStats> getStats() {
      SettableFuture<ServerStats> ret = SettableFuture.create();
      ret.set(serverStats);
      return ret;
    }

    @Override
    public InternalLogId getLogId() {
      return id;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("logId", getLogId())
          .toString();
    }
  }

  static final class TestChannel implements InternalInstrumented<ChannelStats> {
    private final InternalLogId id =
        InternalLogId.allocate("channel-or-subchannel", /*details=*/ null);

    ChannelStats stats = new ChannelStats.Builder()
        .setTarget("sometarget")
        .setState(ConnectivityState.READY)
        .setCallsStarted(1)
        .setCallsSucceeded(2)
        .setCallsFailed(3)
        .setLastCallStartedNanos(4)
        .setSubchannels(Collections.<InternalWithLogId>emptyList())
        .setSockets(Collections.<InternalWithLogId>emptyList())
        .build();

    @Override
    public ListenableFuture<ChannelStats> getStats() {
      SettableFuture<ChannelStats> ret = SettableFuture.create();
      ret.set(stats);
      return ret;
    }

    @Override
    public InternalLogId getLogId() {
      return id;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("logId", getLogId())
          .toString();
    }
  }
}
