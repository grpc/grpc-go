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

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.instrumentation.stats.RpcConstants;
import com.google.instrumentation.stats.StatsManager;
import com.google.instrumentation.stats.ViewDescriptor.DistributionViewDescriptor;
import com.google.instrumentation.stats.ViewDescriptor.IntervalViewDescriptor;
import com.google.protobuf.Empty;
import io.grpc.instrumentation.v1alpha.CanonicalRpcStats;
import io.grpc.stub.StreamObserver;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/** Unit tests for {@link MonitoringService}. */
@RunWith(JUnit4.class)
public class MonitoringServiceTest {
  @Mock private StatsManager statsManager;

  private MonitoringService monitoringService;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);
    when(statsManager.getView(any(DistributionViewDescriptor.class)))
        .thenReturn(MonitoringUtilTest.DISTRIBUTION_VIEW);
    when(statsManager.getView(any(IntervalViewDescriptor.class)))
        .thenReturn(MonitoringUtilTest.INTERVAL_VIEW);
    monitoringService = new MonitoringService(statsManager);
  }

  @Test
  public void getCanonicalRpcStats() throws Exception {
    @SuppressWarnings("unchecked")
    StreamObserver<CanonicalRpcStats> observer = mock(StreamObserver.class);
    ArgumentCaptor<CanonicalRpcStats> statsResponse =
        ArgumentCaptor.forClass(CanonicalRpcStats.class);

    monitoringService.getCanonicalRpcStats(Empty.getDefaultInstance(), observer);

    verify(statsManager).getView(RpcConstants.RPC_CLIENT_ROUNDTRIP_LATENCY_VIEW);
    verify(statsManager).getView(RpcConstants.RPC_CLIENT_SERVER_ELAPSED_TIME_VIEW);
    verify(statsManager).getView(RpcConstants.RPC_CLIENT_REQUEST_BYTES_VIEW);
    verify(statsManager).getView(RpcConstants.RPC_CLIENT_RESPONSE_BYTES_VIEW);
    verify(statsManager).getView(RpcConstants.RPC_SERVER_SERVER_LATENCY_VIEW);
    verify(statsManager).getView(RpcConstants.RPC_SERVER_REQUEST_BYTES_VIEW);
    verify(statsManager).getView(RpcConstants.RPC_SERVER_RESPONSE_BYTES_VIEW);
    verify(statsManager).getView(RpcConstants.RPC_SERVER_SERVER_ELAPSED_TIME_VIEW);
    verifyNoMoreInteractions(statsManager);

    verify(observer).onNext(statsResponse.capture());
    assertFalse(statsResponse.getValue().hasRpcClientErrors());
    assertFalse(statsResponse.getValue().hasRpcClientCompletedRpcs());
    assertFalse(statsResponse.getValue().hasRpcClientStartedRpcs());
    assertTrue(statsResponse.getValue().hasRpcClientElapsedTime());
    assertTrue(statsResponse.getValue().hasRpcClientServerElapsedTime());
    assertTrue(statsResponse.getValue().hasRpcClientRequestBytes());
    assertTrue(statsResponse.getValue().hasRpcClientResponseBytes());
    assertFalse(statsResponse.getValue().hasRpcClientRequestCount());
    assertFalse(statsResponse.getValue().hasRpcClientResponseCount());
    assertFalse(statsResponse.getValue().hasRpcServerErrors());
    assertFalse(statsResponse.getValue().hasRpcServerCompletedRpcs());
    assertTrue(statsResponse.getValue().hasRpcServerServerElapsedTime());
    assertTrue(statsResponse.getValue().hasRpcServerRequestBytes());
    assertTrue(statsResponse.getValue().hasRpcServerResponseBytes());
    assertFalse(statsResponse.getValue().hasRpcServerRequestCount());
    assertFalse(statsResponse.getValue().hasRpcServerResponseCount());
    assertTrue(statsResponse.getValue().hasRpcServerElapsedTime());
  }
}
