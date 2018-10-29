/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.ExperimentalApi;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;

/**
 * A factory for {@link LoadBalancer}s that uses the GRPCLB protocol.
 *
 * <p><b>Experimental:</b>This only works with the GRPCLB load-balancer service, which is not
 * available yet. Right now it's only good for internal testing.
 *
 * @deprecated The "grpclb" policy will be selected when environment is set up correctly, thus no
 *             need to directly reference the factory.  If explicit selection is needed, use {@link
 *             io.grpc.LoadBalancerRegistry#getProvider} with "grpclb" policy.  This class will be
 *             deleted soon.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1782")
@Deprecated
public final class GrpclbLoadBalancerFactory extends LoadBalancer.Factory {

  private static GrpclbLoadBalancerFactory instance;
  private final LoadBalancerProvider provider;

  private GrpclbLoadBalancerFactory() {
    provider = checkNotNull(
        LoadBalancerRegistry.getDefaultRegistry().getProvider("grpclb"),
        "grpclb balancer not available");
  }

  /**
   * Returns the instance.
   */
  public static GrpclbLoadBalancerFactory getInstance() {
    if (instance == null) {
      instance = new GrpclbLoadBalancerFactory();
    }
    return instance;
  }

  @Override
  public LoadBalancer newLoadBalancer(LoadBalancer.Helper helper) {
    return provider.newLoadBalancer(helper);
  }
}
