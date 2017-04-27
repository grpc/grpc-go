/*
 * Copyright 2017, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.protobuf.util.Timestamps;
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
  public ClientStreamTracer newClientStreamTracer(Metadata headers) {
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
