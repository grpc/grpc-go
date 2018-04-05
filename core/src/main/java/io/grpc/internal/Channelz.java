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

package io.grpc.internal;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.ConnectivityState;
import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.concurrent.ConcurrentNavigableMap;
import java.util.concurrent.ConcurrentSkipListMap;
import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;

public final class Channelz {
  private static final Channelz INSTANCE = new Channelz();

  private final ConcurrentNavigableMap<Long, Instrumented<ServerStats>> servers
      = new ConcurrentSkipListMap<Long, Instrumented<ServerStats>>();
  private final ConcurrentNavigableMap<Long, Instrumented<ChannelStats>> rootChannels
      = new ConcurrentSkipListMap<Long, Instrumented<ChannelStats>>();
  private final ConcurrentMap<Long, Instrumented<ChannelStats>> subchannels
      = new ConcurrentHashMap<Long, Instrumented<ChannelStats>>();
  // An InProcessTransport can appear in both otherSockets and perServerSockets simultaneously
  private final ConcurrentMap<Long, Instrumented<SocketStats>> otherSockets
      = new ConcurrentHashMap<Long, Instrumented<SocketStats>>();
  private final ConcurrentMap<Long, ServerSocketMap> perServerSockets
      = new ConcurrentHashMap<Long, ServerSocketMap>();

  // A convenience class to avoid deeply nested types.
  private static final class ServerSocketMap
      extends ConcurrentSkipListMap<Long, Instrumented<SocketStats>> {
    private static final long serialVersionUID = -7883772124944661414L;
  }

  @VisibleForTesting
  public Channelz() {
  }

  public static Channelz instance() {
    return INSTANCE;
  }

  /** Adds a server. */
  public void addServer(Instrumented<ServerStats> server) {
    ServerSocketMap prev = perServerSockets.put(id(server), new ServerSocketMap());
    assert prev == null;
    add(servers, server);
  }

  /** Adds a subchannel. */
  public void addSubchannel(Instrumented<ChannelStats> subchannel) {
    add(subchannels, subchannel);
  }

  /** Adds a root channel. */
  public void addRootChannel(Instrumented<ChannelStats> rootChannel) {
    add(rootChannels, rootChannel);
  }

  /** Adds a socket. */
  public void addClientSocket(Instrumented<SocketStats> socket) {
    add(otherSockets, socket);
  }

  public void addListenSocket(Instrumented<SocketStats> socket) {
    add(otherSockets, socket);
  }

  /** Adds a server socket. */
  public void addServerSocket(Instrumented<ServerStats> server, Instrumented<SocketStats> socket) {
    ServerSocketMap serverSockets = perServerSockets.get(id(server));
    assert serverSockets != null;
    add(serverSockets, socket);
  }

  /** Removes a server. */
  public void removeServer(Instrumented<ServerStats> server) {
    remove(servers, server);
    ServerSocketMap prev = perServerSockets.remove(id(server));
    assert prev != null;
    assert prev.isEmpty();
  }

  public void removeSubchannel(Instrumented<ChannelStats> subchannel) {
    remove(subchannels, subchannel);
  }

  public void removeRootChannel(Instrumented<ChannelStats> channel) {
    remove(rootChannels, channel);
  }

  public void removeClientSocket(Instrumented<SocketStats> socket) {
    remove(otherSockets, socket);
  }

  public void removeListenSocket(Instrumented<SocketStats> socket) {
    remove(otherSockets, socket);
  }

  /** Removes a server socket. */
  public void removeServerSocket(
      Instrumented<ServerStats> server, Instrumented<SocketStats> socket) {
    ServerSocketMap socketsOfServer = perServerSockets.get(id(server));
    assert socketsOfServer != null;
    remove(socketsOfServer, socket);
  }

  /** Returns a {@link RootChannelList}. */
  public RootChannelList getRootChannels(long fromId, int maxPageSize) {
    List<Instrumented<ChannelStats>> channelList = new ArrayList<Instrumented<ChannelStats>>();
    Iterator<Instrumented<ChannelStats>> iterator
        = rootChannels.tailMap(fromId).values().iterator();

    while (iterator.hasNext() && channelList.size() < maxPageSize) {
      channelList.add(iterator.next());
    }
    return new RootChannelList(channelList, !iterator.hasNext());
  }

  /** Returns a channel. */
  @Nullable
  public Instrumented<ChannelStats> getChannel(long id) {
    return rootChannels.get(id);
  }

  /** Returns a subchannel. */
  @Nullable
  public Instrumented<ChannelStats> getSubchannel(long id) {
    return subchannels.get(id);
  }

  /** Returns a server list. */
  public ServerList getServers(long fromId, int maxPageSize) {
    List<Instrumented<ServerStats>> serverList
        = new ArrayList<Instrumented<ServerStats>>(maxPageSize);
    Iterator<Instrumented<ServerStats>> iterator = servers.tailMap(fromId).values().iterator();

    while (iterator.hasNext() && serverList.size() < maxPageSize) {
      serverList.add(iterator.next());
    }
    return new ServerList(serverList, !iterator.hasNext());
  }

  /** Returns socket refs for a server. */
  @Nullable
  public ServerSocketsList getServerSockets(long serverId, long fromId, int maxPageSize) {
    ServerSocketMap serverSockets = perServerSockets.get(serverId);
    if (serverSockets == null) {
      return null;
    }
    List<WithLogId> socketList = new ArrayList<WithLogId>(maxPageSize);
    Iterator<Instrumented<SocketStats>> iterator
        = serverSockets.tailMap(fromId).values().iterator();
    while (socketList.size() < maxPageSize && iterator.hasNext()) {
      socketList.add(iterator.next());
    }
    return new ServerSocketsList(socketList, !iterator.hasNext());
  }

  /** Returns a socket. */
  @Nullable
  public Instrumented<SocketStats> getSocket(long id) {
    Instrumented<SocketStats> clientSocket = otherSockets.get(id);
    if (clientSocket != null) {
      return clientSocket;
    }
    return getServerSocket(id);
  }

  private Instrumented<SocketStats> getServerSocket(long id) {
    for (ServerSocketMap perServerSockets : perServerSockets.values()) {
      Instrumented<SocketStats> serverSocket = perServerSockets.get(id);
      if (serverSocket != null) {
        return serverSocket;
      }
    }
    return null;
  }

  @VisibleForTesting
  public boolean containsServer(LogId serverRef) {
    return contains(servers, serverRef);
  }

  @VisibleForTesting
  public boolean containsSubchannel(LogId subchannelRef) {
    return contains(subchannels, subchannelRef);
  }

  public Instrumented<ChannelStats> getRootChannel(long id) {
    return rootChannels.get(id);
  }

  @VisibleForTesting
  public boolean containsClientSocket(LogId transportRef) {
    return contains(otherSockets, transportRef);
  }

  private static <T extends Instrumented<?>> void add(Map<Long, T> map, T object) {
    T prev = map.put(object.getLogId().getId(), object);
    assert prev == null;
  }

  private static <T extends Instrumented<?>> void remove(Map<Long, T> map, T object) {
    T prev = map.remove(id(object));
    assert prev != null;
  }

  private static <T extends Instrumented<?>> boolean contains(Map<Long, T> map, LogId id) {
    return map.containsKey(id.getId());
  }

  public static final class RootChannelList {
    public final List<Instrumented<ChannelStats>> channels;
    public final boolean end;

    /** Creates an instance. */
    public RootChannelList(List<Instrumented<ChannelStats>> channels, boolean end) {
      this.channels = Preconditions.checkNotNull(channels);
      this.end = end;
    }
  }

  public static final class ServerList {
    public final List<Instrumented<ServerStats>> servers;
    public final boolean end;

    /** Creates an instance. */
    public ServerList(List<Instrumented<ServerStats>> servers, boolean end) {
      this.servers = Preconditions.checkNotNull(servers);
      this.end = end;
    }
  }

  public static final class ServerSocketsList {
    public final List<WithLogId> sockets;
    public final boolean end;

    /** Creates an instance. */
    public ServerSocketsList(List<WithLogId> sockets, boolean end) {
      this.sockets = sockets;
      this.end = end;
    }
  }

  @Immutable
  public static final class ServerStats {
    public final long callsStarted;
    public final long callsSucceeded;
    public final long callsFailed;
    public final long lastCallStartedMillis;
    public final List<Instrumented<SocketStats>> listenSockets;

    /**
     * Creates an instance.
     */
    public ServerStats(
        long callsStarted,
        long callsSucceeded,
        long callsFailed,
        long lastCallStartedMillis,
        List<Instrumented<SocketStats>> listenSockets) {
      this.callsStarted = callsStarted;
      this.callsSucceeded = callsSucceeded;
      this.callsFailed = callsFailed;
      this.lastCallStartedMillis = lastCallStartedMillis;
      this.listenSockets = Preconditions.checkNotNull(listenSockets);
    }

    public static final class Builder {
      private long callsStarted;
      private long callsSucceeded;
      private long callsFailed;
      private long lastCallStartedMillis;
      public List<Instrumented<SocketStats>> listenSockets = Collections.emptyList();

      public Builder setCallsStarted(long callsStarted) {
        this.callsStarted = callsStarted;
        return this;
      }

      public Builder setCallsSucceeded(long callsSucceeded) {
        this.callsSucceeded = callsSucceeded;
        return this;
      }

      public Builder setCallsFailed(long callsFailed) {
        this.callsFailed = callsFailed;
        return this;
      }

      public Builder setLastCallStartedMillis(long lastCallStartedMillis) {
        this.lastCallStartedMillis = lastCallStartedMillis;
        return this;
      }

      /** Sets the listen sockets. */
      public Builder setListenSockets(List<Instrumented<SocketStats>> listenSockets) {
        Preconditions.checkNotNull(listenSockets);
        this.listenSockets = Collections.unmodifiableList(
            new ArrayList<Instrumented<SocketStats>>(listenSockets));
        return this;
      }

      /**
       * Builds an instance.
       */
      public ServerStats build() {
        return new ServerStats(
            callsStarted,
            callsSucceeded,
            callsFailed,
            lastCallStartedMillis,
            listenSockets);
      }
    }
  }

  /**
   * A data class to represent a channel's stats.
   */
  @Immutable
  public static final class ChannelStats {
    public final String target;
    public final ConnectivityState state;
    public final long callsStarted;
    public final long callsSucceeded;
    public final long callsFailed;
    public final long lastCallStartedMillis;
    public final List<WithLogId> subchannels;
    public final List<WithLogId> sockets;

    /**
     * Creates an instance.
     */
    public ChannelStats(
        String target,
        ConnectivityState state,
        long callsStarted,
        long callsSucceeded,
        long callsFailed,
        long lastCallStartedMillis,
        List<WithLogId> subchannels,
        List<WithLogId> sockets) {
      Preconditions.checkState(
          subchannels.isEmpty() || sockets.isEmpty(),
          "channels can have subchannels only, subchannels can have either sockets OR subchannels, "
              + "neither can have both");
      this.target = target;
      this.state = state;
      this.callsStarted = callsStarted;
      this.callsSucceeded = callsSucceeded;
      this.callsFailed = callsFailed;
      this.lastCallStartedMillis = lastCallStartedMillis;
      this.subchannels = Preconditions.checkNotNull(subchannels);
      this.sockets = Preconditions.checkNotNull(sockets);
    }

    public static final class Builder {
      private String target;
      private ConnectivityState state;
      private long callsStarted;
      private long callsSucceeded;
      private long callsFailed;
      private long lastCallStartedMillis;
      private List<WithLogId> subchannels = Collections.emptyList();
      private List<WithLogId> sockets = Collections.emptyList();

      public Builder setTarget(String target) {
        this.target = target;
        return this;
      }

      public Builder setState(ConnectivityState state) {
        this.state = state;
        return this;
      }

      public Builder setCallsStarted(long callsStarted) {
        this.callsStarted = callsStarted;
        return this;
      }

      public Builder setCallsSucceeded(long callsSucceeded) {
        this.callsSucceeded = callsSucceeded;
        return this;
      }

      public Builder setCallsFailed(long callsFailed) {
        this.callsFailed = callsFailed;
        return this;
      }

      public Builder setLastCallStartedMillis(long lastCallStartedMillis) {
        this.lastCallStartedMillis = lastCallStartedMillis;
        return this;
      }

      /** Sets the subchannels. */
      public Builder setSubchannels(List<WithLogId> subchannels) {
        Preconditions.checkState(sockets.isEmpty());
        this.subchannels = Collections.unmodifiableList(Preconditions.checkNotNull(subchannels));
        return this;
      }

      /** Sets the sockets. */
      public Builder setSockets(List<WithLogId> sockets) {
        Preconditions.checkState(subchannels.isEmpty());
        this.sockets = Collections.unmodifiableList(Preconditions.checkNotNull(sockets));
        return this;
      }

      /**
       * Builds an instance.
       */
      public ChannelStats build() {
        return new ChannelStats(
            target,
            state,
            callsStarted,
            callsSucceeded,
            callsFailed,
            lastCallStartedMillis,
            subchannels,
            sockets);
      }
    }
  }

  public static final class Security {
    // TODO(zpencer): fill this in
  }

  public static final class SocketStats {
    @Nullable public final TransportStats data;
    public final SocketAddress local;
    @Nullable public final SocketAddress remote;
    public final SocketOptions socketOptions;
    @Nullable public final Security security;

    /** Creates an instance. */
    public SocketStats(
        TransportStats data,
        SocketAddress local,
        SocketAddress remote,
        SocketOptions socketOptions,
        Security security) {
      this.data = data;
      this.local = local;
      this.remote = remote;
      this.socketOptions = socketOptions;
      this.security = security;
    }
  }

  public static final class TcpInfo {
    public final int state;
    public final int caState;
    public final int retransmits;
    public final int probes;
    public final int backoff;
    public final int options;
    public final int sndWscale;
    public final int rcvWscale;
    public final int rto;
    public final int ato;
    public final int sndMss;
    public final int rcvMss;
    public final int unacked;
    public final int sacked;
    public final int lost;
    public final int retrans;
    public final int fackets;
    public final int lastDataSent;
    public final int lastAckSent;
    public final int lastDataRecv;
    public final int lastAckRecv;
    public final int pmtu;
    public final int rcvSsthresh;
    public final int rtt;
    public final int rttvar;
    public final int sndSsthresh;
    public final int sndCwnd;
    public final int advmss;
    public final int reordering;

    TcpInfo(int state, int caState, int retransmits, int probes, int backoff, int options,
        int sndWscale, int rcvWscale, int rto, int ato, int sndMss, int rcvMss, int unacked,
        int sacked, int lost, int retrans, int fackets, int lastDataSent, int lastAckSent,
        int lastDataRecv, int lastAckRecv, int pmtu, int rcvSsthresh, int rtt, int rttvar,
        int sndSsthresh, int sndCwnd, int advmss, int reordering) {
      this.state = state;
      this.caState = caState;
      this.retransmits = retransmits;
      this.probes = probes;
      this.backoff = backoff;
      this.options = options;
      this.sndWscale = sndWscale;
      this.rcvWscale = rcvWscale;
      this.rto = rto;
      this.ato = ato;
      this.sndMss = sndMss;
      this.rcvMss = rcvMss;
      this.unacked = unacked;
      this.sacked = sacked;
      this.lost = lost;
      this.retrans = retrans;
      this.fackets = fackets;
      this.lastDataSent = lastDataSent;
      this.lastAckSent = lastAckSent;
      this.lastDataRecv = lastDataRecv;
      this.lastAckRecv = lastAckRecv;
      this.pmtu = pmtu;
      this.rcvSsthresh = rcvSsthresh;
      this.rtt = rtt;
      this.rttvar = rttvar;
      this.sndSsthresh = sndSsthresh;
      this.sndCwnd = sndCwnd;
      this.advmss = advmss;
      this.reordering = reordering;
    }

    public static final class Builder {
      private int state;
      private int caState;
      private int retransmits;
      private int probes;
      private int backoff;
      private int options;
      private int sndWscale;
      private int rcvWscale;
      private int rto;
      private int ato;
      private int sndMss;
      private int rcvMss;
      private int unacked;
      private int sacked;
      private int lost;
      private int retrans;
      private int fackets;
      private int lastDataSent;
      private int lastAckSent;
      private int lastDataRecv;
      private int lastAckRecv;
      private int pmtu;
      private int rcvSsthresh;
      private int rtt;
      private int rttvar;
      private int sndSsthresh;
      private int sndCwnd;
      private int advmss;
      private int reordering;

      public Builder setState(int state) {
        this.state = state;
        return this;
      }

      public Builder setCaState(int caState) {
        this.caState = caState;
        return this;
      }

      public Builder setRetransmits(int retransmits) {
        this.retransmits = retransmits;
        return this;
      }

      public Builder setProbes(int probes) {
        this.probes = probes;
        return this;
      }

      public Builder setBackoff(int backoff) {
        this.backoff = backoff;
        return this;
      }

      public Builder setOptions(int options) {
        this.options = options;
        return this;
      }

      public Builder setSndWscale(int sndWscale) {
        this.sndWscale = sndWscale;
        return this;
      }

      public Builder setRcvWscale(int rcvWscale) {
        this.rcvWscale = rcvWscale;
        return this;
      }

      public Builder setRto(int rto) {
        this.rto = rto;
        return this;
      }

      public Builder setAto(int ato) {
        this.ato = ato;
        return this;
      }

      public Builder setSndMss(int sndMss) {
        this.sndMss = sndMss;
        return this;
      }

      public Builder setRcvMss(int rcvMss) {
        this.rcvMss = rcvMss;
        return this;
      }

      public Builder setUnacked(int unacked) {
        this.unacked = unacked;
        return this;
      }

      public Builder setSacked(int sacked) {
        this.sacked = sacked;
        return this;
      }

      public Builder setLost(int lost) {
        this.lost = lost;
        return this;
      }

      public Builder setRetrans(int retrans) {
        this.retrans = retrans;
        return this;
      }

      public Builder setFackets(int fackets) {
        this.fackets = fackets;
        return this;
      }

      public Builder setLastDataSent(int lastDataSent) {
        this.lastDataSent = lastDataSent;
        return this;
      }

      public Builder setLastAckSent(int lastAckSent) {
        this.lastAckSent = lastAckSent;
        return this;
      }

      public Builder setLastDataRecv(int lastDataRecv) {
        this.lastDataRecv = lastDataRecv;
        return this;
      }

      public Builder setLastAckRecv(int lastAckRecv) {
        this.lastAckRecv = lastAckRecv;
        return this;
      }

      public Builder setPmtu(int pmtu) {
        this.pmtu = pmtu;
        return this;
      }

      public Builder setRcvSsthresh(int rcvSsthresh) {
        this.rcvSsthresh = rcvSsthresh;
        return this;
      }

      public Builder setRtt(int rtt) {
        this.rtt = rtt;
        return this;
      }

      public Builder setRttvar(int rttvar) {
        this.rttvar = rttvar;
        return this;
      }

      public Builder setSndSsthresh(int sndSsthresh) {
        this.sndSsthresh = sndSsthresh;
        return this;
      }

      public Builder setSndCwnd(int sndCwnd) {
        this.sndCwnd = sndCwnd;
        return this;
      }

      public Builder setAdvmss(int advmss) {
        this.advmss = advmss;
        return this;
      }

      public Builder setReordering(int reordering) {
        this.reordering = reordering;
        return this;
      }

      /** Builds an instance. */
      public TcpInfo build() {
        return new TcpInfo(
            state, caState, retransmits, probes, backoff, options, sndWscale, rcvWscale,
            rto, ato, sndMss, rcvMss, unacked, sacked, lost, retrans, fackets, lastDataSent,
            lastAckSent, lastDataRecv, lastAckRecv, pmtu, rcvSsthresh, rtt, rttvar, sndSsthresh,
            sndCwnd, advmss, reordering);
      }
    }
  }

  public static final class SocketOptions {
    public final Map<String, String> others;
    // In netty, the value of a channel option may be null.
    @Nullable public final Integer soTimeoutMillis;
    @Nullable public final Integer lingerSeconds;
    @Nullable public final TcpInfo tcpInfo;

    /** Creates an instance. */
    public SocketOptions(
        @Nullable Integer timeoutMillis,
        @Nullable Integer lingerSeconds,
        @Nullable TcpInfo tcpInfo,
        Map<String, String> others) {
      Preconditions.checkNotNull(others);
      this.soTimeoutMillis = timeoutMillis;
      this.lingerSeconds = lingerSeconds;
      this.tcpInfo = tcpInfo;
      this.others = Collections.unmodifiableMap(new HashMap<String, String>(others));
    }

    public static final class Builder {
      private final Map<String, String> others = new HashMap<String, String>();

      private TcpInfo tcpInfo;
      private Integer timeoutMillis;
      private Integer lingerSeconds;

      /** The value of {@link java.net.Socket#getSoTimeout()}. */
      public Builder setSocketOptionTimeoutMillis(Integer timeoutMillis) {
        this.timeoutMillis = timeoutMillis;
        return this;
      }

      /** The value of {@link java.net.Socket#getSoLinger()}.
       * Note: SO_LINGER is typically expressed in seconds.
       */
      public Builder setSocketOptionLingerSeconds(Integer lingerSeconds) {
        this.lingerSeconds = lingerSeconds;
        return this;
      }

      public Builder setTcpInfo(TcpInfo tcpInfo) {
        this.tcpInfo = tcpInfo;
        return this;
      }

      public Builder addOption(String name, String value) {
        others.put(name, Preconditions.checkNotNull(value));
        return this;
      }

      public Builder addOption(String name, int value) {
        others.put(name, Integer.toString(value));
        return this;
      }

      public Builder addOption(String name, boolean value) {
        others.put(name, Boolean.toString(value));
        return this;
      }

      public SocketOptions build() {
        return new SocketOptions(timeoutMillis, lingerSeconds, tcpInfo, others);
      }
    }
  }

  /**
   * A data class to represent transport stats.
   */
  @Immutable
  public static final class TransportStats {
    public final long streamsStarted;
    public final long lastLocalStreamCreatedTimeNanos;
    public final long lastRemoteStreamCreatedTimeNanos;
    public final long streamsSucceeded;
    public final long streamsFailed;
    public final long messagesSent;
    public final long messagesReceived;
    public final long keepAlivesSent;
    public final long lastMessageSentTimeNanos;
    public final long lastMessageReceivedTimeNanos;
    public final long localFlowControlWindow;
    public final long remoteFlowControlWindow;
    // TODO(zpencer): report socket flags and other info

    /**
     * Creates an instance.
     */
    public TransportStats(
        long streamsStarted,
        long lastLocalStreamCreatedTimeNanos,
        long lastRemoteStreamCreatedTimeNanos,
        long streamsSucceeded,
        long streamsFailed,
        long messagesSent,
        long messagesReceived,
        long keepAlivesSent,
        long lastMessageSentTimeNanos,
        long lastMessageReceivedTimeNanos,
        long localFlowControlWindow,
        long remoteFlowControlWindow) {
      this.streamsStarted = streamsStarted;
      this.lastLocalStreamCreatedTimeNanos = lastLocalStreamCreatedTimeNanos;
      this.lastRemoteStreamCreatedTimeNanos = lastRemoteStreamCreatedTimeNanos;
      this.streamsSucceeded = streamsSucceeded;
      this.streamsFailed = streamsFailed;
      this.messagesSent = messagesSent;
      this.messagesReceived = messagesReceived;
      this.keepAlivesSent = keepAlivesSent;
      this.lastMessageSentTimeNanos = lastMessageSentTimeNanos;
      this.lastMessageReceivedTimeNanos = lastMessageReceivedTimeNanos;
      this.localFlowControlWindow = localFlowControlWindow;
      this.remoteFlowControlWindow = remoteFlowControlWindow;
    }
  }

  /** Unwraps a {@link LogId} to return a {@code long}. */
  public static long id(WithLogId withLogId) {
    return withLogId.getLogId().getId();
  }
}
