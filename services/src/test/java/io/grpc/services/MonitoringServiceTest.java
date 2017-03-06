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
