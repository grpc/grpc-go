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

import com.google.common.base.MoreObjects;
import io.grpc.NameResolver.Helper.ConfigOrError;
import java.util.Map;

/**
 * Provider of {@link LoadBalancer}s.  Each provider is bounded to a load-balancing policy name.
 *
 * @since 1.17.0
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
public abstract class LoadBalancerProvider extends LoadBalancer.Factory {
  /**
   * Whether this provider is available for use, taking the current environment into consideration.
   * If {@code false}, {@link #newLoadBalancer} is not safe to be called.
   */
  public abstract boolean isAvailable();

  /**
   * A priority, from 0 to 10 that this provider should be used, taking the current environment into
   * consideration. 5 should be considered the default, and then tweaked based on environment
   * detection. A priority of 0 does not imply that the provider wouldn't work; just that it should
   * be last in line.
   */
  public abstract int getPriority();

  /**
   * Returns the load-balancing policy name associated with this provider, which makes it selectable
   * via {@link LoadBalancerRegistry#getProvider}.  This is called only when the class is loaded. It
   * shouldn't change, and there is no point doing so.
   *
   * <p>The policy name should consist of only lower case letters letters, underscore and digits,
   * and can only start with letters.
   */
  public abstract String getPolicyName();

  /**
   * Parses the config for the Load Balancing policy unpacked from the service config.  This will
   * return a {@link ConfigOrError} which contains either the successfully parsed config, or the
   * {@link Status} representing the failure to parse.  Implementations are expected to not throw
   * exceptions but return a Status representing the failure.  If successful, the load balancing
   * policy config should be immutable, and implement {@link Object#equals(Object)} and
   * {@link Object#hashCode()}.
   *
   * @param rawLoadBalancingPolicyConfig The {@link Map} representation of the load balancing
   *     policy choice.
   * @return a tuple of the fully parsed and validated balancer configuration, else the Status.
   * @since 1.20.0
   * @see https://github.com/grpc/proposal/blob/master/A24-lb-policy-config.md
   */
  public ConfigOrError parseLoadBalancingPolicyConfig(Map<String, ?> rawLoadBalancingPolicyConfig) {
    return ConfigOrError.UNKNOWN_CONFIG;
  }

  @Override
  public final String toString() {
    return MoreObjects.toStringHelper(this)
        .add("policy", getPolicyName())
        .add("priority", getPriority())
        .add("available", isAvailable())
        .toString();
  }

  /**
   * Uses identity equality.
   */
  @Override
  public final boolean equals(Object other) {
    return this == other;
  }

  @Override
  public final int hashCode() {
    return super.hashCode();
  }
}
