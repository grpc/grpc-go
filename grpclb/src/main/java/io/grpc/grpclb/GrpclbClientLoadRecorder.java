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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.protobuf.util.Timestamps;
import io.grpc.ClientStreamTracer;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.TimeProvider;
import io.grpc.lb.v1.ClientStats;
import io.grpc.lb.v1.ClientStatsPerToken;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.Map.Entry;
import java.util.concurrent.atomic.AtomicLongFieldUpdater;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Record and aggregate client-side load data for GRPCLB.  This records load occurred during the
 * span of an LB stream with the remote load-balancer.
 */
@ThreadSafe
final class GrpclbClientLoadRecorder extends ClientStreamTracer.Factory {

  private static final AtomicLongFieldUpdater<GrpclbClientLoadRecorder> callsStartedUpdater =
      AtomicLongFieldUpdater.newUpdater(GrpclbClientLoadRecorder.class, "callsStarted");
  private static final AtomicLongFieldUpdater<GrpclbClientLoadRecorder> callsFinishedUpdater =
      AtomicLongFieldUpdater.newUpdater(GrpclbClientLoadRecorder.class, "callsFinished");
  private static final AtomicLongFieldUpdater<GrpclbClientLoadRecorder> callsFailedToSendUpdater =
      AtomicLongFieldUpdater.newUpdater(GrpclbClientLoadRecorder.class, "callsFailedToSend");
  private static final AtomicLongFieldUpdater<GrpclbClientLoadRecorder>
      callsFinishedKnownReceivedUpdater =
          AtomicLongFieldUpdater.newUpdater(
              GrpclbClientLoadRecorder.class, "callsFinishedKnownReceived");

  private final TimeProvider time;
  @SuppressWarnings("unused")
  private volatile long callsStarted;
  @SuppressWarnings("unused")
  private volatile long callsFinished;

  private static final class LongHolder {
    long num;
  }

  // Specific finish types
  @GuardedBy("this")
  private Map<String, LongHolder> callsDroppedPerToken = new HashMap<>(1);
  @SuppressWarnings("unused")
  private volatile long callsFailedToSend;
  @SuppressWarnings("unused")
  private volatile long callsFinishedKnownReceived;

  GrpclbClientLoadRecorder(TimeProvider time) {
    this.time = checkNotNull(time, "time provider");
  }

  @Override
  public ClientStreamTracer newClientStreamTracer(
      ClientStreamTracer.StreamInfo info, Metadata headers) {
    callsStartedUpdater.getAndIncrement(this);
    return new StreamTracer();
  }

  /**
   * Records that a request has been dropped as instructed by the remote balancer.
   */
  void recordDroppedRequest(String token) {
    callsStartedUpdater.getAndIncrement(this);
    callsFinishedUpdater.getAndIncrement(this);

    synchronized (this) {
      LongHolder holder;
      if ((holder = callsDroppedPerToken.get(token)) == null) {
        callsDroppedPerToken.put(token, (holder = new LongHolder()));
      }
      holder.num++;
    }
  }

  /**
   * Generate the report with the data recorded this LB stream since the last report.
   */
  ClientStats generateLoadReport() {
    ClientStats.Builder statsBuilder =
        ClientStats.newBuilder()
        .setTimestamp(Timestamps.fromNanos(time.currentTimeNanos()))
        .setNumCallsStarted(callsStartedUpdater.getAndSet(this, 0))
        .setNumCallsFinished(callsFinishedUpdater.getAndSet(this, 0))
        .setNumCallsFinishedWithClientFailedToSend(callsFailedToSendUpdater.getAndSet(this, 0))
        .setNumCallsFinishedKnownReceived(callsFinishedKnownReceivedUpdater.getAndSet(this, 0));

    Map<String, LongHolder> localCallsDroppedPerToken = Collections.emptyMap();
    synchronized (this) {
      if (!callsDroppedPerToken.isEmpty()) {
        localCallsDroppedPerToken = callsDroppedPerToken;
        callsDroppedPerToken = new HashMap<>(localCallsDroppedPerToken.size());
      }
    }
    for (Entry<String, LongHolder> entry : localCallsDroppedPerToken.entrySet()) {
      statsBuilder.addCallsFinishedWithDrop(
          ClientStatsPerToken.newBuilder()
              .setLoadBalanceToken(entry.getKey())
              .setNumCalls(entry.getValue().num)
              .build());
    }
    return statsBuilder.build();
  }

  private class StreamTracer extends ClientStreamTracer {
    private volatile boolean headersSent;
    private volatile boolean anythingReceived;

    @Override
    public void outboundHeaders() {
      headersSent = true;
    }

    @Override
    public void inboundHeaders() {
      anythingReceived = true;
    }

    @Override
    public void inboundMessage(int seqNo) {
      anythingReceived = true;
    }

    @Override
    public void streamClosed(Status status) {
      callsFinishedUpdater.getAndIncrement(GrpclbClientLoadRecorder.this);
      if (!headersSent) {
        callsFailedToSendUpdater.getAndIncrement(GrpclbClientLoadRecorder.this);
      }
      if (anythingReceived) {
        callsFinishedKnownReceivedUpdater.getAndIncrement(GrpclbClientLoadRecorder.this);
      }
    }
  }
}
