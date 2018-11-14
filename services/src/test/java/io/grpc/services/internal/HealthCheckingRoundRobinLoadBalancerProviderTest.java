/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.services.internal;

import static com.google.common.truth.Truth.assertThat;

import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link InternalHealthCheckingRoundRobinLoadBalancerProvider}.
 */
@RunWith(JUnit4.class)
public class HealthCheckingRoundRobinLoadBalancerProviderTest {
  @Test
  public void registry() {
    LoadBalancerProvider hcRoundRobin =
        LoadBalancerRegistry.getDefaultRegistry().getProvider("round_robin");
    assertThat(hcRoundRobin).isInstanceOf(
        HealthCheckingRoundRobinLoadBalancerProvider.class);
  }

  @Test
  public void policyName() {
    LoadBalancerProvider hcRoundRobin = new HealthCheckingRoundRobinLoadBalancerProvider();
    assertThat(hcRoundRobin.getPolicyName())
        .isEqualTo(
            HealthCheckingRoundRobinLoadBalancerProvider.newRoundRobinProvider().getPolicyName());
  }

  @Test
  public void priority() {
    LoadBalancerProvider hcRoundRobin = new HealthCheckingRoundRobinLoadBalancerProvider();
    assertThat(hcRoundRobin.getPriority())
        .isEqualTo(
            HealthCheckingRoundRobinLoadBalancerProvider.newRoundRobinProvider().getPriority() + 1);
  }
}
