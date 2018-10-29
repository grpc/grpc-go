/*
 * Copyright 2015 The gRPC Authors
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

import static com.google.common.base.Preconditions.checkNotNull;

/**
 * A {@link LoadBalancer} that provides no load balancing mechanism over the
 * addresses from the {@link NameResolver}.  The channel's default behavior
 * (currently pick-first) is used for all addresses found.
 *
 * @deprecated this is the default balancer and should not be referenced to.  This will be deleted
 *             soon.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
@Deprecated
public final class PickFirstBalancerFactory extends LoadBalancer.Factory {
  private static PickFirstBalancerFactory instance;

  private final LoadBalancerProvider provider;

  private PickFirstBalancerFactory() {
    provider = checkNotNull(
        LoadBalancerRegistry.getDefaultRegistry().getProvider("pick_first"),
        "pick_first balancer not available");
  }

  /**
   * Gets an instance of this factory.
   */
  public static synchronized PickFirstBalancerFactory getInstance() {
    if (instance == null) {
      instance = new PickFirstBalancerFactory();
    }
    return instance;
  }

  @Override
  public LoadBalancer newLoadBalancer(LoadBalancer.Helper helper) {
    return provider.newLoadBalancer(helper);
  }
}
