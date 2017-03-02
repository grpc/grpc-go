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

package io.grpc.services;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import com.google.instrumentation.stats.RpcConstants;
import com.google.instrumentation.stats.Stats;
import com.google.instrumentation.stats.StatsManager;
import com.google.protobuf.Empty;
import io.grpc.ExperimentalApi;
import io.grpc.instrumentation.v1alpha.CanonicalRpcStats;
import io.grpc.instrumentation.v1alpha.MonitoringGrpc;
import io.grpc.stub.StreamObserver;

/**
 * An implementation of the gRPC monitoring service.
 *
 * <p>An implementation of {@link StatsManager} must be available at runtime (determined via {@link
 * Stats#getStatsManager}) or instantiating this service will fail.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2776")
public final class MonitoringService extends MonitoringGrpc.MonitoringImplBase {
  private static MonitoringService instance;

  private final StatsManager statsManager;

  @VisibleForTesting
  MonitoringService(StatsManager statsManager) {
    checkNotNull(statsManager, "StatsManager implementation unavailable");
    this.statsManager = statsManager;
  }

  /**
   * Gets the singleton instance of the MonitoringService.
   *
   * @throws IllegalStateException if {@link Stats#getStatsManager} returns null
   */
  public static synchronized MonitoringService getInstance() {
    if (instance == null) {
      instance = new MonitoringService(Stats.getStatsManager());
    }
    return instance;
  }

  // TODO(ericgribkoff) Add remaining CanonicalRpcStats fields when they are included in
  // instrumentation.
  @Override
  public void getCanonicalRpcStats(
      Empty request, StreamObserver<CanonicalRpcStats> responseObserver) {
    CanonicalRpcStats response =
        CanonicalRpcStats.newBuilder()
            .setRpcClientElapsedTime(
                MonitoringUtil.buildCanonicalRpcStatsView(
                    statsManager.getView(RpcConstants.RPC_CLIENT_ROUNDTRIP_LATENCY_VIEW)))
            .setRpcClientServerElapsedTime(
                MonitoringUtil.buildCanonicalRpcStatsView(
                    statsManager.getView(RpcConstants.RPC_CLIENT_SERVER_ELAPSED_TIME_VIEW)))
            .setRpcClientRequestBytes(
                MonitoringUtil.buildCanonicalRpcStatsView(
                    statsManager.getView(RpcConstants.RPC_CLIENT_REQUEST_BYTES_VIEW)))
            .setRpcClientResponseBytes(
                MonitoringUtil.buildCanonicalRpcStatsView(
                    statsManager.getView(RpcConstants.RPC_CLIENT_RESPONSE_BYTES_VIEW)))
            .setRpcServerServerElapsedTime(
                MonitoringUtil.buildCanonicalRpcStatsView(
                    statsManager.getView(RpcConstants.RPC_SERVER_SERVER_LATENCY_VIEW)))
            .setRpcServerRequestBytes(
                MonitoringUtil.buildCanonicalRpcStatsView(
                    statsManager.getView(RpcConstants.RPC_SERVER_REQUEST_BYTES_VIEW)))
            .setRpcServerResponseBytes(
                MonitoringUtil.buildCanonicalRpcStatsView(
                    statsManager.getView(RpcConstants.RPC_SERVER_RESPONSE_BYTES_VIEW)))
            .setRpcServerElapsedTime(
                MonitoringUtil.buildCanonicalRpcStatsView(
                    statsManager.getView(RpcConstants.RPC_SERVER_SERVER_ELAPSED_TIME_VIEW)))
            .build();
    responseObserver.onNext(response);
    responseObserver.onCompleted();
  }
}
