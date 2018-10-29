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

  @Override
  public final String toString() {
    return MoreObjects.toStringHelper(this)
        .add("policy", getPolicyName())
        .add("priority", getPriority())
        .add("available", isAvailable())
        .toString();
  }
}
