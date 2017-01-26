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

package io.grpc.testing.integration;

import static com.google.common.collect.Sets.newHashSet;
import static java.util.Collections.singletonList;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import com.google.common.collect.ImmutableList;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.testing.integration.Metrics.EmptyMessage;
import io.grpc.testing.integration.Metrics.GaugeResponse;
import io.grpc.testing.integration.StressTestClient.TestCaseWeightPair;
import java.net.InetSocketAddress;
import java.util.Arrays;
import java.util.List;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.locks.LockSupport;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link StressTestClient}. */
@RunWith(JUnit4.class)
public class StressTestClientTest {

  @Test
  public void ipv6AddressesShouldBeSupported() {
    StressTestClient client = new StressTestClient();
    client.parseArgs(new String[] {"--server_addresses=[0:0:0:0:0:0:0:1]:8080,"
        + "[1:2:3:4:f:e:a:b]:8083"});

    assertEquals(2, client.addresses().size());
    assertEquals(new InetSocketAddress("0:0:0:0:0:0:0:1", 8080), client.addresses().get(0));
    assertEquals(new InetSocketAddress("1:2:3:4:f:e:a:b", 8083), client.addresses().get(1));
  }

  @Test
  public void defaults() {
    StressTestClient client = new StressTestClient();
    assertEquals(singletonList(new InetSocketAddress("localhost", 8080)), client.addresses());
    assertTrue(client.testCaseWeightPairs().isEmpty());
    assertEquals(-1, client.durationSecs());
    assertEquals(1, client.channelsPerServer());
    assertEquals(1, client.stubsPerChannel());
    assertEquals(8081, client.metricsPort());
  }

  @Test
  public void allCommandlineSwitchesAreSupported() {
    StressTestClient client = new StressTestClient();
    client.parseArgs(new String[] {
        "--server_addresses=localhost:8080,localhost:8081,localhost:8082",
        "--test_cases=empty_unary:20,large_unary:50,server_streaming:30",
        "--test_duration_secs=20",
        "--num_channels_per_server=10",
        "--num_stubs_per_channel=5",
        "--metrics_port=9090",
        "--server_host_override=foo.test.google.fr",
        "--use_tls=true",
        "--use_test_ca=true"
    });

    List<InetSocketAddress> addresses = Arrays.asList(new InetSocketAddress("localhost", 8080),
        new InetSocketAddress("localhost", 8081), new InetSocketAddress("localhost", 8082));
    assertEquals(addresses, client.addresses());

    List<TestCaseWeightPair> testCases = Arrays.asList(
        new TestCaseWeightPair(TestCases.EMPTY_UNARY, 20),
        new TestCaseWeightPair(TestCases.LARGE_UNARY, 50),
        new TestCaseWeightPair(TestCases.SERVER_STREAMING, 30));
    assertEquals(testCases, client.testCaseWeightPairs());

    assertEquals("foo.test.google.fr", client.serverHostOverride());
    assertTrue(client.useTls());
    assertTrue(client.useTestCa());
    assertEquals(20, client.durationSecs());
    assertEquals(10, client.channelsPerServer());
    assertEquals(5, client.stubsPerChannel());
    assertEquals(9090, client.metricsPort());
  }

  @Test
  public void serverHostOverrideShouldBeApplied() {
    StressTestClient client = new StressTestClient();
    client.parseArgs(new String[] {
        "--server_addresses=localhost:8080",
        "--server_host_override=foo.test.google.fr",
    });

    assertEquals("foo.test.google.fr", client.addresses().get(0).getHostName());
  }

  @Test(timeout = 5000)
  public void gaugesShouldBeExported() throws Exception {

    TestServiceServer server = new TestServiceServer();
    server.parseArgs(new String[]{"--port=" + 0, "--use_tls=false"});
    server.start();

    StressTestClient client = new StressTestClient();
    client.parseArgs(new String[] {"--test_cases=empty_unary:1",
        "--server_addresses=localhost:" + server.getPort(), "--metrics_port=" + 0,
        "--num_stubs_per_channel=2"});
    client.startMetricsService();
    client.runStressTest();

    // Connect to the metrics service
    ManagedChannel ch = ManagedChannelBuilder.forAddress("localhost", client.getMetricServerPort())
        .usePlaintext(true)
        .build();

    MetricsServiceGrpc.MetricsServiceBlockingStub stub = MetricsServiceGrpc.newBlockingStub(ch);

    // Wait until gauges have been exported
    Set<String> gaugeNames = newHashSet("/stress_test/server_0/channel_0/stub_0/qps",
        "/stress_test/server_0/channel_0/stub_1/qps");

    List<GaugeResponse> allGauges =
        ImmutableList.copyOf(stub.getAllGauges(EmptyMessage.getDefaultInstance()));
    while (allGauges.size() < gaugeNames.size()) {
      LockSupport.parkNanos(TimeUnit.MILLISECONDS.toNanos(100));
      allGauges = ImmutableList.copyOf(stub.getAllGauges(EmptyMessage.getDefaultInstance()));
    }

    for (GaugeResponse gauge : allGauges) {
      String gaugeName = gauge.getName();

      assertTrue("gaugeName: " + gaugeName, gaugeNames.contains(gaugeName));
      assertTrue("qps: " + gauge.getLongValue(), gauge.getLongValue() > 0);
      gaugeNames.remove(gauge.getName());

      GaugeResponse gauge1 =
          stub.getGauge(Metrics.GaugeRequest.newBuilder().setName(gaugeName).build());
      assertEquals(gaugeName, gauge1.getName());
      assertTrue("qps: " + gauge1.getLongValue(), gauge1.getLongValue() > 0);
    }

    assertTrue("gauges: " + gaugeNames, gaugeNames.isEmpty());

    client.shutdown();
    server.stop();
  }

}
