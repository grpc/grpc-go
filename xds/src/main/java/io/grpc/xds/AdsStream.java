/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.xds;

import static com.google.common.base.Preconditions.checkNotNull;

import io.envoyproxy.envoy.api.v2.DiscoveryRequest;
import io.envoyproxy.envoy.api.v2.DiscoveryResponse;
import io.envoyproxy.envoy.service.discovery.v2.AggregatedDiscoveryServiceGrpc.AggregatedDiscoveryServiceStub;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;

/**
 * ADS client implementation.
 */
final class AdsStream implements StreamObserver<DiscoveryResponse> {
  private final AggregatedDiscoveryServiceStub stub;

  private StreamObserver<DiscoveryRequest> xdsRequestWriter;
  private boolean cancelled;

  AdsStream(AggregatedDiscoveryServiceStub stub) {
    this.stub = checkNotNull(stub, "stub");
  }

  void start() {
    xdsRequestWriter = stub.withWaitForReady().streamAggregatedResources(this);
  }

  @Override
  public void onNext(DiscoveryResponse value) {
    // TODO: impl
  }

  @Override
  public void onError(Throwable t) {
    // TODO: impl
  }

  @Override
  public void onCompleted() {
    // TODO: impl
  }

  void cancel(String message) {
    if (cancelled) {
      return;
    }
    cancelled = true;
    xdsRequestWriter.onError(Status.CANCELLED.withDescription(message).asRuntimeException());
  }
}
