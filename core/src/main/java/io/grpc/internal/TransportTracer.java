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

package io.grpc.internal;

import com.google.common.base.Preconditions;
import io.grpc.Status;
import java.util.concurrent.TimeUnit;

/**
 * A class for gathering statistics about a transport. This is an experimental feature.
 * Can only be called from the transport thread unless otherwise noted.
 */
public final class TransportTracer {
  private long streamsStarted;
  private long lastStreamCreatedTimeNanos;
  private long streamsSucceeded;
  private long streamsFailed;
  private long keepAlivesSent;
  private FlowControlReader flowControlWindowReader;

  private long messagesSent;
  private long lastMessageSentTimeNanos;
  // deframing happens on the application thread, and there's no easy way to avoid synchronization
  private final LongCounter messagesReceived = LongCounterFactory.create();
  private volatile long lastMessageReceivedTimeNanos;

  /**
   * Returns a read only set of current stats.
   */
  public Stats getStats() {
    return new Stats(
        streamsStarted,
        lastStreamCreatedTimeNanos,
        streamsSucceeded,
        streamsFailed,
        messagesSent,
        messagesReceived.value(),
        keepAlivesSent,
        lastMessageSentTimeNanos,
        lastMessageReceivedTimeNanos,
        flowControlWindowReader);
  }

  /**
   * Called by the transport to report a stream has started. For clients, this happens when a header
   * is sent. For servers, this happens when a header is received.
   */
  public void reportStreamStarted() {
    streamsStarted++;
    lastStreamCreatedTimeNanos = currentTimeNanos();
  }

  /**
   * Reports that a stream closed with the specified Status.
   */
  public void reportStreamClosed(Status status) {
    if (status.isOk()) {
      streamsSucceeded++;
    } else {
      streamsFailed++;
    }
  }

  /**
   * Reports that a message was successfully sent. This method is thread safe.
   */
  public void reportMessageSent() {
    messagesSent++;
    lastMessageSentTimeNanos = currentTimeNanos();
  }

  /**
   * Reports that a message was successfully received. This method is thread safe.
   */
  public void reportMessageReceived() {
    messagesReceived.add(1);
    lastMessageReceivedTimeNanos = currentTimeNanos();
  }

  /**
   * Reports that a keep alive message was sent.
   */
  public void reportKeepAliveSent() {
    keepAlivesSent++;
  }

  /**
   * Registers a {@link FlowControlReader} that can be used to read the local and remote flow
   * control window sizes.
   */
  public void setFlowControlWindowReader(FlowControlReader flowControlWindowReader) {
    this.flowControlWindowReader = Preconditions.checkNotNull(flowControlWindowReader);
  }

  /**
   * A container that holds the local and remote flow control window sizes.
   */
  public static final class FlowControlWindows {
    public final long remoteBytes;
    public final long localBytes;

    public FlowControlWindows(long localBytes, long remoteBytes) {
      this.localBytes = localBytes;
      this.remoteBytes = remoteBytes;
    }
  }

  /**
   * An interface for reading the local and remote flow control windows of the transport.
   */
  public interface FlowControlReader {
    FlowControlWindows read();
  }

  private static long currentTimeNanos() {
    return TimeUnit.MILLISECONDS.toNanos(System.currentTimeMillis());
  }

  /**
   * A read only copy of stats from the transport tracer.
   */
  public static final class Stats {
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

    private Stats(
        long streamsStarted,
        long lastStreamCreatedTimeNanos,
        long streamsSucceeded,
        long streamsFailed,
        long messagesSent,
        long messagesReceived,
        long keepAlivesSent,
        long lastMessageSentTimeNanos,
        long lastMessageReceivedTimeNanos,
        FlowControlReader flowControlReader) {
      this.streamsStarted = streamsStarted;
      this.lastStreamCreatedTimeNanos = lastStreamCreatedTimeNanos;
      this.streamsSucceeded = streamsSucceeded;
      this.streamsFailed = streamsFailed;
      this.messagesSent = messagesSent;
      this.messagesReceived = messagesReceived;
      this.keepAlivesSent = keepAlivesSent;
      this.lastMessageSentTimeNanos = lastMessageSentTimeNanos;
      this.lastMessageReceivedTimeNanos = lastMessageReceivedTimeNanos;
      if (flowControlReader == null) {
        this.localFlowControlWindow = -1;
        this.remoteFlowControlWindow = -1;
      } else {
        FlowControlWindows windows = flowControlReader.read();
        this.localFlowControlWindow = windows.localBytes;
        this.remoteFlowControlWindow = windows.remoteBytes;
      }
    }
  }
}
