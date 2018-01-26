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

import io.grpc.ConnectivityState;
import javax.annotation.concurrent.Immutable;

public final class Channelz {

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

    /**
     * Creates an instance.
     */
    public ChannelStats(
        String target,
        ConnectivityState state,
        long callsStarted,
        long callsSucceeded,
        long callsFailed,
        long lastCallStartedMillis) {
      this.target = target;
      this.state = state;
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

      public ChannelStats build() {
        return new ChannelStats(
            target, state, callsStarted,  callsSucceeded,  callsFailed,  lastCallStartedMillis);
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
