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
import static com.google.common.base.Preconditions.checkState;

import io.envoyproxy.envoy.api.v2.DiscoveryRequest;
import io.envoyproxy.envoy.api.v2.DiscoveryResponse;
import io.envoyproxy.envoy.service.discovery.v2.AggregatedDiscoveryServiceGrpc;
import io.grpc.LoadBalancer.Helper;
import io.grpc.ManagedChannel;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;

/**
 * ADS client implementation.
 */
final class XdsComms {
  private final ManagedChannel channel;
  private final Helper helper;

  // never null
  private AdsStream adsStream;

  private final class AdsStream {

    final AdsStreamCallback adsStreamCallback;

    final StreamObserver<DiscoveryRequest> xdsRequestWriter;

    final StreamObserver<DiscoveryResponse> xdsResponseReader =
        new StreamObserver<DiscoveryResponse>() {

          boolean firstResponseReceived;

          @Override
          public void onNext(DiscoveryResponse value) {
            if (!firstResponseReceived) {
              firstResponseReceived = true;
              helper.getSynchronizationContext().execute(
                  new Runnable() {
                    @Override
                    public void run() {
                      adsStreamCallback.onWorking();
                    }
                  });
            }
            // TODO: more impl
          }

          @Override
          public void onError(Throwable t) {
            helper.getSynchronizationContext().execute(
                new Runnable() {
                  @Override
                  public void run() {
                    closed = true;
                    if (cancelled) {
                      return;
                    }
                    adsStreamCallback.onError();
                  }
                });
            // TODO: more impl
          }

          @Override
          public void onCompleted() {
            // TODO: impl
          }
        };

    boolean cancelled;
    boolean closed;

    AdsStream(AdsStreamCallback adsStreamCallback) {
      this.adsStreamCallback = adsStreamCallback;
      this.xdsRequestWriter = AggregatedDiscoveryServiceGrpc.newStub(channel).withWaitForReady()
          .streamAggregatedResources(xdsResponseReader);
    }
  }

  /**
   * Starts a new ADS streaming RPC.
   */
  XdsComms(
      ManagedChannel channel, Helper helper, AdsStreamCallback adsStreamCallback) {
    this.channel = checkNotNull(channel, "channel");
    this.helper = checkNotNull(helper, "helper");
    this.adsStream = new AdsStream(checkNotNull(adsStreamCallback, "adsStreamCallback"));
  }

  void shutdownChannel() {
    channel.shutdown();
    shutdownLbRpc("Loadbalancer client shutdown");
  }

  void refreshAdsStream() {
    checkState(!channel.isShutdown(), "channel is alreday shutdown");

    if (adsStream.closed || adsStream.cancelled) {
      adsStream = new AdsStream(adsStream.adsStreamCallback);
    }
  }

  void shutdownLbRpc(String message) {
    if (adsStream.cancelled) {
      return;
    }
    adsStream.cancelled = true;
    adsStream.xdsRequestWriter.onError(
        Status.CANCELLED.withDescription(message).asRuntimeException());
  }

  /**
   * Callback on ADS stream events. The callback methods should be called in a proper {@link
   * io.grpc.SynchronizationContext}.
   */
  interface AdsStreamCallback {

    /**
     * Once the response observer receives the first response.
     */
    void onWorking();

    /**
     * Once an error occurs in ADS stream.
     */
    void onError();
  }
}
