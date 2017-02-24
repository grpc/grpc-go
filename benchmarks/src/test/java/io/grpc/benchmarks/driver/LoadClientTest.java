/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.benchmarks.driver;

import static junit.framework.TestCase.assertEquals;
import static junit.framework.TestCase.assertTrue;

import io.grpc.benchmarks.proto.Control;
import io.grpc.benchmarks.proto.Stats;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link LoadClient}.
 */
@RunWith(JUnit4.class)
public class LoadClientTest {

  @Test
  public void testHistogramToStatsConversion() throws Exception {
    double resolution = 1.01;
    double maxPossible = 10000.0;
    Control.ClientConfig.Builder config = Control.ClientConfig.newBuilder();
    config.getHistogramParamsBuilder().setMaxPossible(maxPossible)
        .setResolution(resolution - 1.0);
    config.getPayloadConfigBuilder().getSimpleParamsBuilder()
        .setReqSize(1)
        .setRespSize(1);
    config.setRpcType(Control.RpcType.UNARY);
    config.setClientType(Control.ClientType.SYNC_CLIENT);
    config.setClientChannels(1);
    config.setOutstandingRpcsPerChannel(1);
    config.getLoadParamsBuilder().getClosedLoopBuilder();
    config.addServerTargets("localhost:9999");

    LoadClient loadClient = new LoadClient(config.build());
    loadClient.delay(1);
    loadClient.delay(10);
    loadClient.delay(10);
    loadClient.delay(100);
    loadClient.delay(100);
    loadClient.delay(100);
    loadClient.delay(1000);
    loadClient.delay(1000);
    loadClient.delay(1000);
    loadClient.delay(1000);

    Stats.ClientStats stats = loadClient.getStats();

    assertEquals(1.0, stats.getLatencies().getMinSeen(), 0.0);
    assertEquals(1000.0, stats.getLatencies().getMaxSeen(), 0.0);
    assertEquals(10.0, stats.getLatencies().getCount(), 0.0);

    double base = 0;
    double logBase = 1;

    for (int i = 0; i < stats.getLatencies().getBucketCount(); i++) {
      int bucketCount = stats.getLatencies().getBucket(i);
      if (base > 1.0 && base / resolution < 1.0) {
        assertEquals(1, bucketCount);
      } else if (base > 10.0 && base / resolution < 10.0) {
        assertEquals(2, bucketCount);
      } else if (base > 100.0 && base / resolution < 100.0) {
        assertEquals(3, bucketCount);
      } else if (base > 1000.0 && base / resolution < 1000.0) {
        assertEquals(4, bucketCount);
      } else {
        assertEquals(0, bucketCount);
      }
      logBase = logBase * resolution;
      base = logBase - 1;
    }
    assertTrue(base > 10000);
    assertTrue(base / resolution <= 10000);
  }
}
