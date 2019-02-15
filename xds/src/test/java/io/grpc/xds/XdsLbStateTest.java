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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.envoyproxy.envoy.api.v2.DiscoveryRequest;
import io.envoyproxy.envoy.api.v2.DiscoveryResponse;
import io.envoyproxy.envoy.service.discovery.v2.AggregatedDiscoveryServiceGrpc;
import io.envoyproxy.envoy.service.discovery.v2.AggregatedDiscoveryServiceGrpc.AggregatedDiscoveryServiceImplBase;
import io.envoyproxy.envoy.service.discovery.v2.AggregatedDiscoveryServiceGrpc.AggregatedDiscoveryServiceStub;
import io.grpc.ManagedChannel;
import io.grpc.Status;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.testing.StreamRecorder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcCleanupRule;
import io.grpc.xds.XdsLbState.XdsComms;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link XdsLbState}.
 */
@RunWith(JUnit4.class)
public class XdsLbStateTest {
  @Rule
  public final GrpcCleanupRule cleanupRule = new GrpcCleanupRule();

  private final StreamRecorder<DiscoveryRequest> streamRecorder = StreamRecorder.create();

  private XdsComms xdsComms;

  private ManagedChannel channel;

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);

    String serverName = InProcessServerBuilder.generateName();

    AggregatedDiscoveryServiceImplBase serviceImpl = new AggregatedDiscoveryServiceImplBase() {
      @Override
      public StreamObserver<DiscoveryRequest> streamAggregatedResources(
          final StreamObserver<DiscoveryResponse> responseObserver) {
        return new StreamObserver<DiscoveryRequest>() {

          @Override
          public void onNext(DiscoveryRequest value) {
            streamRecorder.onNext(value);
          }

          @Override
          public void onError(Throwable t) {
            streamRecorder.onError(t);
          }

          @Override
          public void onCompleted() {
            streamRecorder.onCompleted();
            responseObserver.onCompleted();
          }
        };
      }
    };

    cleanupRule.register(
        InProcessServerBuilder
            .forName(serverName)
            .addService(serviceImpl)
            .build()
            .start());
    channel =
        cleanupRule.register(InProcessChannelBuilder.forName(serverName).build());
    AggregatedDiscoveryServiceStub stub = AggregatedDiscoveryServiceGrpc.newStub(channel);
    AdsStream adsStream = new AdsStream(stub);
    adsStream.start();
    xdsComms = new XdsComms(channel, adsStream);
  }

  @After
  public void tearDown() {
    if (!channel.isShutdown()) {
      channel.shutdownNow();
    }
  }

  @Test
  public void shutdownLbComm() throws Exception {
    xdsComms.shutdownChannel();
    assertTrue(channel.isShutdown());
    assertTrue(streamRecorder.awaitCompletion(1, TimeUnit.SECONDS));
    assertEquals(Status.Code.CANCELLED, Status.fromThrowable(streamRecorder.getError()).getCode());
  }

  @Test
  public void shutdownLbRpc_verifyChannelNotShutdown() throws Exception {
    xdsComms.shutdownLbRpc("shutdown msg1");
    assertTrue(streamRecorder.awaitCompletion(1, TimeUnit.SECONDS));
    assertEquals(Status.Code.CANCELLED, Status.fromThrowable(streamRecorder.getError()).getCode());
    assertFalse(channel.isShutdown());
  }

  @Test
  public void shutdownAndReleaseXdsCommsDoesShutdown() {
    XdsLbState xdsLbState = mock(XdsLbState.class);
    xdsLbState.shutdownAndReleaseXdsComms();
    verify(xdsLbState).shutdown();
  }
}
