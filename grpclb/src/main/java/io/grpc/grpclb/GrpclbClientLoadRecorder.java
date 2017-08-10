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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.protobuf.util.Timestamps;
import io.grpc.CallOptions;
import io.grpc.ClientStreamTracer;
import io.grpc.Metadata;
import io.grpc.Status;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Record and aggregate client-side load data for GRPCLB.  This records load occurred during the
 * span of an LB stream with the remote load-balancer.
 */
@ThreadSafe
final class GrpclbClientLoadRecorder extends ClientStreamTracer.Factory {
  private final TimeProvider time;
  private final AtomicLong callsStarted = new AtomicLong();
  private final AtomicLong callsFinished = new AtomicLong();

  // Specific finish types
  // Access to it should be protected by lock.  Contention is not an issue for these counts, because
  // normally only a small portion of all RPCs are dropped.
  @GuardedBy("this")
  private HashMap<String, AtomicLong> callsDroppedPerToken = new HashMap<String, AtomicLong>();
  private final AtomicLong callsFailedToSend = new AtomicLong();
  private final AtomicLong callsFinishedKnownReceived = new AtomicLong();

  GrpclbClientLoadRecorder(TimeProvider time) {
    this.time = checkNotNull(time, "time provider");
  }

  @Override
  public ClientStreamTracer newClientStreamTracer(CallOptions callOptions, Metadata headers) {
    callsStarted.incrementAndGet();
    return new StreamTracer();
  }

  /**
   * Records that a request has been dropped as instructed by the remote balancer.
   */
  void recordDroppedRequest(String token) {
    callsStarted.incrementAndGet();
    callsFinished.incrementAndGet();

    synchronized (this) {
      AtomicLong count = callsDroppedPerToken.get(token);
      if (count == null) {
        count = new AtomicLong(1);
        callsDroppedPerToken.put(token, count);
      } else {
        count.incrementAndGet();
      }
    }
  }

  /**
   * Generate the report with the data recorded this LB stream since the last report.
   */
  ClientStats generateLoadReport() {
    ClientStats.Builder statsBuilder =
        ClientStats.newBuilder()
        .setTimestamp(Timestamps.fromMillis(time.currentTimeMillis()))
        .setNumCallsStarted(callsStarted.getAndSet(0))
        .setNumCallsFinished(callsFinished.getAndSet(0))
        .setNumCallsFinishedWithClientFailedToSend(callsFailedToSend.getAndSet(0))
        .setNumCallsFinishedKnownReceived(callsFinishedKnownReceived.getAndSet(0));
    HashMap<String, AtomicLong> savedCallsDroppedPerToken;
    synchronized (this) {
      savedCallsDroppedPerToken = callsDroppedPerToken;
      callsDroppedPerToken = new HashMap<String, AtomicLong>();
    }
    for (Map.Entry<String, AtomicLong> dropCount : savedCallsDroppedPerToken.entrySet()) {
      statsBuilder.addCallsFinishedWithDrop(
          ClientStatsPerToken.newBuilder()
              .setLoadBalanceToken(dropCount.getKey())
              .setNumCalls(dropCount.getValue().get())
              .build());
    }
    return statsBuilder.build();
  }

  private class StreamTracer extends ClientStreamTracer {
    final AtomicBoolean headersSent = new AtomicBoolean();
    final AtomicBoolean anythingReceived = new AtomicBoolean();

    @Override
    public void outboundHeaders() {
      headersSent.set(true);
    }

    @Override
    public void inboundHeaders() {
      anythingReceived.set(true);
    }

    @Override
    public void inboundMessage() {
      anythingReceived.set(true);
    }

    @Override
    public void streamClosed(Status status) {
      callsFinished.incrementAndGet();
      if (!headersSent.get()) {
        callsFailedToSend.incrementAndGet();
      }
      if (anythingReceived.get()) {
        callsFinishedKnownReceived.incrementAndGet();
      }
    }
  }
}
