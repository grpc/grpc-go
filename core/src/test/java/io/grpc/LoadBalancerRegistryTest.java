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

package io.grpc;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.fail;

import io.grpc.grpclb.GrpclbLoadBalancerProvider;
import io.grpc.internal.PickFirstLoadBalancerProvider;
import java.util.List;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link LoadBalancerRegistry}. */
@RunWith(JUnit4.class)
public class LoadBalancerRegistryTest {
  @Test
  public void getClassesViaHardcoded_classesPresent() throws Exception {
    List<Class<?>> classes = LoadBalancerRegistry.getHardCodedClasses();
    assertThat(classes).hasSize(2);
    assertThat(classes.get(0)).isEqualTo(PickFirstLoadBalancerProvider.class);
    assertThat(classes.get(1).getName()).isEqualTo(
        "io.grpc.util.SecretRoundRobinLoadBalancerProvider$Provider");
  }

  @Test
  public void stockProviders() {
    LoadBalancerRegistry defaultRegistry = LoadBalancerRegistry.getDefaultRegistry();
    assertThat(defaultRegistry.providers()).hasSize(3);

    LoadBalancerProvider pickFirst = defaultRegistry.getProvider("pick_first");
    assertThat(pickFirst).isInstanceOf(PickFirstLoadBalancerProvider.class);
    assertThat(pickFirst.getPriority()).isEqualTo(5);

    LoadBalancerProvider roundRobin = defaultRegistry.getProvider("round_robin");
    assertThat(roundRobin.getClass().getName()).isEqualTo(
        "io.grpc.util.SecretRoundRobinLoadBalancerProvider$Provider");
    assertThat(roundRobin.getPriority()).isEqualTo(5);

    LoadBalancerProvider grpclb = defaultRegistry.getProvider("grpclb");
    assertThat(grpclb).isInstanceOf(GrpclbLoadBalancerProvider.class);
    assertThat(grpclb.getPriority()).isEqualTo(5);
  }

  @Test
  public void unavilableProviderThrows() {
    LoadBalancerRegistry reg = new LoadBalancerRegistry();
    try {
      reg.register(new FakeProvider("great", 5, false));
      fail("Should throw");
    } catch (IllegalArgumentException e) {
      assertThat(e.getMessage()).contains("isAvailable() returned false");
    }
    assertThat(reg.getProvider("great")).isNull();
  }

  @Test
  public void registerAndDeregister() {
    LoadBalancerProvider[] providers = new LoadBalancerProvider[] {
      new FakeProvider("cool", 5, true),
      new FakeProvider("cool", 6, true),
      new FakeProvider("great", 5, true),
      new FakeProvider("great", 4, true),
      new FakeProvider("excellent", 5, true),
      new FakeProvider("excellent", 5, true)};
    LoadBalancerRegistry reg = new LoadBalancerRegistry();
    for (LoadBalancerProvider provider : providers) {
      reg.register(provider);
    }

    assertThat(reg.providers()).hasSize(3);
    assertThat(reg.getProvider("cool")).isSameAs(providers[1]);
    assertThat(reg.getProvider("great")).isSameAs(providers[2]);
    assertThat(reg.getProvider("excellent")).isSameAs(providers[4]);

    reg.deregister(providers[1]);
    assertThat(reg.getProvider("cool")).isSameAs(providers[0]);
    reg.deregister(providers[2]);
    assertThat(reg.getProvider("great")).isSameAs(providers[3]);
    reg.deregister(providers[4]);
    assertThat(reg.getProvider("excellent")).isSameAs(providers[5]);
  }

  private static class FakeProvider extends LoadBalancerProvider {
    final String policy;
    final int priority;
    final boolean isAvailable;

    FakeProvider(String policy, int priority, boolean isAvailable) {
      this.policy = policy;
      this.priority = priority;
      this.isAvailable = isAvailable;
    }

    @Override
    public boolean isAvailable() {
      return isAvailable;
    }

    @Override
    public int getPriority() {
      return priority;
    }

    @Override
    public String getPolicyName() {
      return policy;
    }

    @Override
    public LoadBalancer newLoadBalancer(LoadBalancer.Helper helper) {
      throw new AssertionError("Should not be called in test");
    }
  }
}
