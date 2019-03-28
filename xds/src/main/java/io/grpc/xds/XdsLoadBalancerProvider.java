/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.xds;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.collect.ImmutableMap;
import io.grpc.Internal;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import io.grpc.NameResolver.Helper.ConfigOrError;
import io.grpc.Status;
import io.grpc.internal.ServiceConfigUtil;
import io.grpc.internal.ServiceConfigUtil.LbConfig;
import io.grpc.xds.XdsLbState.SubchannelStoreImpl;
import io.grpc.xds.XdsLoadBalancer.XdsConfig;
import java.util.List;
import java.util.Map;
import javax.annotation.Nullable;

/**
 * The provider for the "xds" balancing policy.  This class should not be directly referenced in
 * code.  The policy should be accessed through {@link io.grpc.LoadBalancerRegistry#getProvider}
 * with the name "xds" (currently "xds_experimental").
 */
@Internal
public final class XdsLoadBalancerProvider extends LoadBalancerProvider {
  private final LoadBalancerRegistry registry = LoadBalancerRegistry.getDefaultRegistry();

  private static final LbConfig DEFAULT_FALLBACK_POLICY =
      new LbConfig("round_robin", ImmutableMap.<String, Void>of());

  @Override
  public boolean isAvailable() {
    return true;
  }

  @Override
  public int getPriority() {
    return 5;
  }

  @Override
  public String getPolicyName() {
    return "xds_experimental";
  }

  @Override
  public LoadBalancer newLoadBalancer(Helper helper) {
    return new XdsLoadBalancer(helper, registry, new SubchannelStoreImpl());
  }

  @Override
  public ConfigOrError parseLoadBalancingPolicyConfig(
      Map<String, ?> rawLoadBalancingPolicyConfig) {
    return parseLoadBalancingConfigPolicy(rawLoadBalancingPolicyConfig, registry);
  }

  static ConfigOrError parseLoadBalancingConfigPolicy(
      Map<String, ?> rawLoadBalancingPolicyConfig, LoadBalancerRegistry registry) {
    try {
      LbConfig newLbConfig =
          ServiceConfigUtil.unwrapLoadBalancingConfig(rawLoadBalancingPolicyConfig);
      String newBalancerName = ServiceConfigUtil.getBalancerNameFromXdsConfig(newLbConfig);
      LbConfig childPolicy = selectChildPolicy(newLbConfig, registry);
      LbConfig fallbackPolicy = selectFallbackPolicy(newLbConfig, registry);
      return ConfigOrError.fromConfig(new XdsConfig(newBalancerName, childPolicy, fallbackPolicy));
    } catch (RuntimeException e) {
      return ConfigOrError.fromError(
          Status.UNKNOWN.withDescription("Failed to parse config " + e.getMessage()).withCause(e));
    }
  }

  @VisibleForTesting
  static LbConfig selectFallbackPolicy(LbConfig lbConfig, LoadBalancerRegistry lbRegistry) {
    List<LbConfig> fallbackConfigs = ServiceConfigUtil.getFallbackPolicyFromXdsConfig(lbConfig);
    LbConfig fallbackPolicy = selectSupportedLbPolicy(fallbackConfigs, lbRegistry);
    return fallbackPolicy == null ? DEFAULT_FALLBACK_POLICY : fallbackPolicy;
  }

  @Nullable
  @VisibleForTesting
  static LbConfig selectChildPolicy(LbConfig lbConfig, LoadBalancerRegistry lbRegistry) {
    List<LbConfig> childConfigs = ServiceConfigUtil.getChildPolicyFromXdsConfig(lbConfig);
    return selectSupportedLbPolicy(childConfigs, lbRegistry);
  }

  @Nullable
  private static LbConfig selectSupportedLbPolicy(
      @Nullable List<LbConfig> lbConfigs, LoadBalancerRegistry lbRegistry) {
    if (lbConfigs == null) {
      return null;
    }
    for (LbConfig lbConfig : lbConfigs) {
      String lbPolicy = lbConfig.getPolicyName();
      if (lbRegistry.getProvider(lbPolicy) != null) {
        return lbConfig;
      }
    }
    return null;
  }
}
