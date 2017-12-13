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

/**
 * Do not use.
 *
 * <p>A read only copy of stats from the transport tracer.
 */
@Internal
public final class InternalTransportStats extends TransportStats {
  /**
   * Creates an instance.
   */
  public InternalTransportStats(
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
    super(
        streamsStarted,
        lastStreamCreatedTimeNanos,
        streamsSucceeded,
        streamsFailed,
        messagesSent,
        messagesReceived,
        keepAlivesSent,
        lastMessageSentTimeNanos,
        lastMessageReceivedTimeNanos,
        localFlowControlWindow,
        remoteFlowControlWindow);
  }
}
