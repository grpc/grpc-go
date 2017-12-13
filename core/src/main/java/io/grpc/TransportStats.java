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

package io.grpc;

import javax.annotation.concurrent.Immutable;

/**
 * A data class to represent transport stats.
 */
@Immutable
class TransportStats {
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
