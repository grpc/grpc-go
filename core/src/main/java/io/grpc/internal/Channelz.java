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
import io.grpc.ConnectivityState;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.logging.Logger;
import javax.annotation.concurrent.Immutable;

public final class Channelz {
  private static final Logger log = Logger.getLogger(Channelz.class.getName());
  private static final Channelz INSTANCE = new Channelz();

  private final ConcurrentMap<Long, Instrumented<ServerStats>> servers =
      new ConcurrentHashMap<Long, Instrumented<ServerStats>>();
  private final ConcurrentMap<Long, Instrumented<ChannelStats>> rootChannels =
      new ConcurrentHashMap<Long, Instrumented<ChannelStats>>();
  private final ConcurrentMap<Long, Instrumented<ChannelStats>> channels =
      new ConcurrentHashMap<Long, Instrumented<ChannelStats>>();
  private final ConcurrentMap<Long, Instrumented<TransportStats>> transports =
      new ConcurrentHashMap<Long, Instrumented<TransportStats>>();

  @VisibleForTesting
  public Channelz() {
  }

  public static Channelz instance() {
    return INSTANCE;
  }

  public void addServer(Instrumented<ServerStats> server) {
    add(servers, server);
  }

  public void addChannel(Instrumented<ChannelStats> channel) {
    add(channels, channel);
  }

  public void addRootChannel(Instrumented<ChannelStats> rootChannel) {
    addChannel(rootChannel);
    add(rootChannels, rootChannel);
  }

  public void addTransport(Instrumented<TransportStats> transport) {
    add(transports, transport);
  }

  public void removeServer(Instrumented<ServerStats> server) {
    remove(servers, server);
  }

  public void removeChannel(Instrumented<ChannelStats> channel) {
    remove(channels, channel);
  }

  public void removeRootChannel(Instrumented<ChannelStats> channel) {
    removeChannel(channel);
    remove(rootChannels, channel);
  }

  public void removeTransport(Instrumented<TransportStats> transport) {
    remove(transports, transport);
  }

  @VisibleForTesting
  public boolean containsServer(LogId serverRef) {
    return contains(servers, serverRef);
  }

  @VisibleForTesting
  public boolean containsChannel(LogId channelRef) {
    return contains(channels, channelRef);
  }

  @VisibleForTesting
  public boolean containsRootChannel(LogId channelRef) {
    return contains(rootChannels, channelRef);
  }

  @VisibleForTesting
  public boolean containsTransport(LogId transportRef) {
    return contains(transports, transportRef);
  }

  private static <T extends Instrumented<?>> void add(Map<Long, T> map, T object) {
    map.put(object.getLogId().getId(), object);
  }

  private static <T extends Instrumented<?>> void remove(Map<Long, T> map, T object) {
    map.remove(object.getLogId().getId());
  }

  private static <T extends Instrumented<?>> boolean contains(Map<Long, T> map, LogId id) {
    return map.containsKey(id.getId());
  }

  @Immutable
  public static final class ServerStats {
    public final long callsStarted;
    public final long callsSucceeded;
    public final long callsFailed;
    public final long lastCallStartedMillis;

    /**
     * Creates an instance.
     */
    public ServerStats(
        long callsStarted,
        long callsSucceeded,
        long callsFailed,
        long lastCallStartedMillis) {
      this.callsStarted = callsStarted;
      this.callsSucceeded = callsSucceeded;
      this.callsFailed = callsFailed;
      this.lastCallStartedMillis = lastCallStartedMillis;
    }

    public static final class Builder {
      private String target;
      private ConnectivityState state;
      private long callsStarted;
      private long callsSucceeded;
      private long callsFailed;
      private long lastCallStartedMillis;
      public List<LogId> subchannels;

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

      /**
       * Builds an instance.
       */
      public ServerStats build() {
        return new ServerStats(
            callsStarted,
            callsSucceeded,
            callsFailed,
            lastCallStartedMillis);
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
    public final List<LogId> subchannels;

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
        List<LogId> subchannels) {
      this.target = target;
      this.state = state;
      this.callsStarted = callsStarted;
      this.callsSucceeded = callsSucceeded;
      this.callsFailed = callsFailed;
      this.lastCallStartedMillis = lastCallStartedMillis;
      this.subchannels = subchannels;
    }

    public static final class Builder {
      private String target;
      private ConnectivityState state;
      private long callsStarted;
      private long callsSucceeded;
      private long callsFailed;
      private long lastCallStartedMillis;
      public List<LogId> subchannels;

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

      public Builder setSubchannels(List<LogId> subchannels) {
        this.subchannels = Collections.unmodifiableList(subchannels);
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
            subchannels);
      }
    }
  }

  /**
   * A data class to represent transport stats.
   */
  @Immutable
  public static final class TransportStats {
    public final long streamsStarted;
    public final long lastStreamCreatedTimeNanos;
    public final long streamsSucceeded;
    public final long streamsFailed;
    public final long messagesSent;
    public final long messagesReceived;
    public final long keepAlivesSent;
    public final long lastMessageSentTimeNanos;
    public final long lastMessageReceivedTimeNanos;
    public final long localFlowControlWindow;
    public final long remoteFlowControlWindow;

    /**
     * Creates an instance.
     */
    public TransportStats(
        long streamsStarted,
        long lastStreamCreatedTimeNanos,
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
      this.lastStreamCreatedTimeNanos = lastStreamCreatedTimeNanos;
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
}
