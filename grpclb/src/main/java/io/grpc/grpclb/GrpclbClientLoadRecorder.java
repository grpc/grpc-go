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
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
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
  private final AtomicLong callsDroppedForRateLimiting = new AtomicLong();
  private final AtomicLong callsDroppedForLoadBalancing = new AtomicLong();
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
  void recordDroppedRequest(DropType type) {
    callsStarted.incrementAndGet();
    callsFinished.incrementAndGet();
    switch (type) {
      case RATE_LIMITING:
        callsDroppedForRateLimiting.incrementAndGet();
        break;
      case LOAD_BALANCING:
        callsDroppedForLoadBalancing.incrementAndGet();
        break;
      default:
        throw new AssertionError("Unsupported DropType: " + type);
    }
  }

  /**
   * Generate the report with the data recorded this LB stream since the last report.
   */
  ClientStats generateLoadReport() {
    return ClientStats.newBuilder()
        .setTimestamp(Timestamps.fromMillis(time.currentTimeMillis()))
        .setNumCallsStarted(callsStarted.getAndSet(0))
        .setNumCallsFinished(callsFinished.getAndSet(0))
        .setNumCallsFinishedWithDropForRateLimiting(callsDroppedForRateLimiting.getAndSet(0))
        .setNumCallsFinishedWithDropForLoadBalancing(callsDroppedForLoadBalancing.getAndSet(0))
        .setNumCallsFinishedWithClientFailedToSend(callsFailedToSend.getAndSet(0))
        .setNumCallsFinishedKnownReceived(callsFinishedKnownReceived.getAndSet(0))
        .build();
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
