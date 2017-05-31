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
