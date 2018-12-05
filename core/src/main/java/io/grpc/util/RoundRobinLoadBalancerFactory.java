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

package io.grpc.util;

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.ExperimentalApi;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;

/**
 * A {@link LoadBalancer} that provides round-robin load balancing mechanism over the
 * addresses.
 *
 * @deprecated use {@link io.grpc.LoadBalancerRegistry#getProvider} with "round_robin" policy.  This
 *             class will be deleted soon.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
@Deprecated
public final class RoundRobinLoadBalancerFactory extends LoadBalancer.Factory {

  private static RoundRobinLoadBalancerFactory instance;

  private final LoadBalancerProvider provider;

  private RoundRobinLoadBalancerFactory() {
    provider = checkNotNull(
        LoadBalancerRegistry.getDefaultRegistry().getProvider("round_robin"),
        "round_robin balancer not available");
  }

  /**
   * Gets the singleton instance of this factory.
   */
  public static synchronized RoundRobinLoadBalancerFactory getInstance() {
    if (instance == null) {
      instance = new RoundRobinLoadBalancerFactory();
    }
    return instance;
  }

  @Override
  public LoadBalancer newLoadBalancer(LoadBalancer.Helper helper) {
    return provider.newLoadBalancer(helper);
  }
}
