/*
 * Copyright 2017 The gRPC Authors
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

import static io.grpc.internal.TimeProvider.SYSTEM_TIME_PROVIDER;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.InternalChannelz.TransportStats;

/**
 * A class for gathering statistics about a transport. This is an experimental feature.
 * Can only be called from the transport thread unless otherwise noted.
 */
public final class TransportTracer {
  private static final Factory DEFAULT_FACTORY = new Factory(SYSTEM_TIME_PROVIDER);

  private final TimeProvider timeProvider;
  private long streamsStarted;
  private long lastLocalStreamCreatedTimeNanos;
  private long lastRemoteStreamCreatedTimeNanos;
  private long streamsSucceeded;
  private long streamsFailed;
  private long keepAlivesSent;
  private FlowControlReader flowControlWindowReader;

  private long messagesSent;
  private long lastMessageSentTimeNanos;
  // deframing happens on the application thread, and there's no easy way to avoid synchronization
  private final LongCounter messagesReceived = LongCounterFactory.create();
  private volatile long lastMessageReceivedTimeNanos;

  public TransportTracer() {
    this.timeProvider = SYSTEM_TIME_PROVIDER;
  }

  private TransportTracer(TimeProvider timeProvider) {
    this.timeProvider = timeProvider;
  }

  /**
   * Returns a read only set of current stats.
   */
  public TransportStats getStats() {
    long localFlowControlWindow =
        flowControlWindowReader == null ? -1 : flowControlWindowReader.read().localBytes;
    long remoteFlowControlWindow =
        flowControlWindowReader == null ? -1 : flowControlWindowReader.read().remoteBytes;
    return new TransportStats(
        streamsStarted,
        lastLocalStreamCreatedTimeNanos,
        lastRemoteStreamCreatedTimeNanos,
        streamsSucceeded,
        streamsFailed,
        messagesSent,
        messagesReceived.value(),
        keepAlivesSent,
        lastMessageSentTimeNanos,
        lastMessageReceivedTimeNanos,
        localFlowControlWindow,
        remoteFlowControlWindow);
  }

  /**
   * Called by the client to report a stream has started.
   */
  public void reportLocalStreamStarted() {
    streamsStarted++;
    lastLocalStreamCreatedTimeNanos = timeProvider.currentTimeNanos();
  }

  /**
   * Called by the server to report a stream has started.
   */
  public void reportRemoteStreamStarted() {
    streamsStarted++;
    lastRemoteStreamCreatedTimeNanos = timeProvider.currentTimeNanos();
  }

  /**
   * Reports that a stream closed with the specified Status.
   */
  public void reportStreamClosed(boolean success) {
    if (success) {
      streamsSucceeded++;
    } else {
      streamsFailed++;
    }
  }

  /**
   * Reports that some messages were successfully sent. {@code numMessages} must be at least 0.
   */
  public void reportMessageSent(int numMessages) {
    if (numMessages == 0) {
      return;
    }
    messagesSent += numMessages;
    lastMessageSentTimeNanos = timeProvider.currentTimeNanos();
  }

  /**
   * Reports that a message was successfully received. This method is thread safe.
   */
  public void reportMessageReceived() {
    messagesReceived.add(1);
    lastMessageReceivedTimeNanos = timeProvider.currentTimeNanos();
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

  public static final class Factory {
    private TimeProvider timeProvider;

    @VisibleForTesting
    public Factory(TimeProvider timeProvider) {
      this.timeProvider = timeProvider;
    }

    public TransportTracer create() {
      return new TransportTracer(timeProvider);
    }
  }

  public static Factory getDefaultFactory() {
    return DEFAULT_FACTORY;
  }
}
