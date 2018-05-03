/*
 * Copyright 2016 The gRPC Authors
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
