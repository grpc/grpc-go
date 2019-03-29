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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.ChannelLogger.ChannelLogLevel;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import io.grpc.NameResolver.Helper.ConfigOrError;
import io.grpc.Status;
import io.grpc.internal.ServiceConfigUtil.LbConfig;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.logging.Logger;
import javax.annotation.Nullable;

public final class AutoConfiguredLoadBalancerFactory extends LoadBalancer.Factory {
  private static final Logger logger =
      Logger.getLogger(AutoConfiguredLoadBalancerFactory.class.getName());
  private static final String GRPCLB_POLICY_NAME = "grpclb";

  private final LoadBalancerRegistry registry;
  private final String defaultPolicy;

  public AutoConfiguredLoadBalancerFactory(String defaultPolicy) {
    this(LoadBalancerRegistry.getDefaultRegistry(), defaultPolicy);
  }

  @VisibleForTesting
  AutoConfiguredLoadBalancerFactory(LoadBalancerRegistry registry, String defaultPolicy) {
    this.registry = checkNotNull(registry, "registry");
    this.defaultPolicy = checkNotNull(defaultPolicy, "defaultPolicy");
  }

  @Override
  public LoadBalancer newLoadBalancer(Helper helper) {
    return new AutoConfiguredLoadBalancer(helper);
  }

  private static final class NoopLoadBalancer extends LoadBalancer {

    @Override
    @Deprecated
    public void handleResolvedAddressGroups(List<EquivalentAddressGroup> s, Attributes a) {}

    @Override
    public void handleResolvedAddresses(ResolvedAddresses resolvedAddresses) {}

    @Override
    public void handleNameResolutionError(Status error) {}

    @Override
    public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {}

    @Override
    public void shutdown() {}
  }

  @VisibleForTesting
  public final class AutoConfiguredLoadBalancer extends LoadBalancer {
    private final Helper helper;
    private LoadBalancer delegate;
    private LoadBalancerProvider delegateProvider;
    private boolean roundRobinDueToGrpclbDepMissing;

    AutoConfiguredLoadBalancer(Helper helper) {
      this.helper = helper;
      delegateProvider = registry.getProvider(defaultPolicy);
      if (delegateProvider == null) {
        throw new IllegalStateException("Could not find policy '" + defaultPolicy
            + "'. Make sure its implementation is either registered to LoadBalancerRegistry or"
            + " included in META-INF/services/io.grpc.LoadBalancerProvider from your jar files.");
      }
      delegate = delegateProvider.newLoadBalancer(helper);
    }

    //  Must be run inside ChannelExecutor.
    @Override
    public void handleResolvedAddresses(ResolvedAddresses resolvedAddresses) {
      List<EquivalentAddressGroup> servers = resolvedAddresses.getServers();
      Attributes attributes = resolvedAddresses.getAttributes();
      if (attributes.get(ATTR_LOAD_BALANCING_CONFIG) != null) {
        throw new IllegalArgumentException(
            "Unexpected ATTR_LOAD_BALANCING_CONFIG from upstream: "
            + attributes.get(ATTR_LOAD_BALANCING_CONFIG));
      }
      Map<String, ?> configMap = attributes.get(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG);
      PolicySelection selection;
      try {
        selection = decideLoadBalancerProvider(servers, configMap);
      } catch (PolicyException e) {
        Status s = Status.INTERNAL.withDescription(e.getMessage());
        helper.updateBalancingState(ConnectivityState.TRANSIENT_FAILURE, new FailingPicker(s));
        delegate.shutdown();
        delegateProvider = null;
        delegate = new NoopLoadBalancer();
        return;
      }

      if (delegateProvider == null
          || !selection.provider.getPolicyName().equals(delegateProvider.getPolicyName())) {
        helper.updateBalancingState(ConnectivityState.CONNECTING, new EmptyPicker());
        delegate.shutdown();
        delegateProvider = selection.provider;
        LoadBalancer old = delegate;
        delegate = delegateProvider.newLoadBalancer(helper);
        helper.getChannelLogger().log(
            ChannelLogLevel.INFO, "Load balancer changed from {0} to {1}",
            old.getClass().getSimpleName(), delegate.getClass().getSimpleName());
      }

      if (selection.config != null) {
        helper.getChannelLogger().log(
            ChannelLogLevel.DEBUG, "Load-balancing config: {0}", selection.config);
        attributes =
            attributes.toBuilder().set(ATTR_LOAD_BALANCING_CONFIG, selection.config).build();
      }

      LoadBalancer delegate = getDelegate();
      if (selection.serverList.isEmpty()
          && !delegate.canHandleEmptyAddressListFromNameResolution()) {
        delegate.handleNameResolutionError(
            Status.UNAVAILABLE.withDescription(
                "Name resolver returned no usable address. addrs="
                + servers + ", attrs=" + attributes));
      } else {
        delegate.handleResolvedAddresses(
            ResolvedAddresses.newBuilder()
                .setServers(selection.serverList)
                .setAttributes(attributes)
                .build());
      }
    }

    @Override
    public void handleNameResolutionError(Status error) {
      getDelegate().handleNameResolutionError(error);
    }

    @Override
    public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {
      getDelegate().handleSubchannelState(subchannel, stateInfo);
    }

    @Override
    public boolean canHandleEmptyAddressListFromNameResolution() {
      return true;
    }

    @Override
    public void shutdown() {
      delegate.shutdown();
      delegate = null;
    }

    @VisibleForTesting
    public LoadBalancer getDelegate() {
      return delegate;
    }

    @VisibleForTesting
    void setDelegate(LoadBalancer lb) {
      delegate = lb;
    }

    @VisibleForTesting
    LoadBalancerProvider getDelegateProvider() {
      return delegateProvider;
    }

    /**
     * Picks a load balancer based on given criteria.  In order of preference:
     *
     * <ol>
     *   <li>User provided lb on the channel.  This is a degenerate case and not handled here.
     *       This options is deprecated.</li>
     *   <li>Policy from "loadBalancingConfig" if present.  This is not covered here.</li>
     *   <li>"grpclb" if any gRPC LB balancer addresses are present</li>
     *   <li>The policy from "loadBalancingPolicy" if present</li>
     *   <li>"pick_first" if the service config choice does not specify</li>
     * </ol>
     *
     * @param servers The list of servers reported
     * @param config the service config object
     * @return the new load balancer factory, never null
     */
    @VisibleForTesting
    PolicySelection decideLoadBalancerProvider(
        List<EquivalentAddressGroup> servers, @Nullable Map<String, ?> config)
        throws PolicyException {
      // Check for balancer addresses
      boolean haveBalancerAddress = false;
      List<EquivalentAddressGroup> backendAddrs = new ArrayList<>();
      for (EquivalentAddressGroup s : servers) {
        if (s.getAttributes().get(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY) != null) {
          haveBalancerAddress = true;
        } else {
          backendAddrs.add(s);
        }
      }

      List<LbConfig> lbConfigs = null;
      if (config != null) {
        List<Map<String, ?>> rawLbConfigs =
            ServiceConfigUtil.getLoadBalancingConfigsFromServiceConfig(config);
        lbConfigs = ServiceConfigUtil.unwrapLoadBalancingConfigList(rawLbConfigs);
      }
      if (lbConfigs != null && !lbConfigs.isEmpty()) {
        LinkedHashSet<String> policiesTried = new LinkedHashSet<>();
        for (LbConfig lbConfig : lbConfigs) {
          String policy = lbConfig.getPolicyName();
          LoadBalancerProvider provider = registry.getProvider(policy);
          if (provider == null) {
            policiesTried.add(policy);
          } else {
            if (!policiesTried.isEmpty()) {
              // Before returning, log all previously tried policies
              helper.getChannelLogger().log(
                  ChannelLogLevel.DEBUG,
                  "{0} specified by Service Config are not available", policiesTried);
            }
            return new PolicySelection(
                provider,
                policy.equals(GRPCLB_POLICY_NAME) ? servers : backendAddrs,
                lbConfig.getRawConfigValue());
          }
        }
        if (!haveBalancerAddress) {
          throw new PolicyException(
            "None of " + policiesTried + " specified by Service Config are available.");
        }
      }

      if (haveBalancerAddress) {
        // This is a special case where the existence of balancer address in the resolved address
        // selects "grpclb" policy if the service config couldn't select a policy
        LoadBalancerProvider grpclbProvider = registry.getProvider(GRPCLB_POLICY_NAME);
        if (grpclbProvider == null) {
          if (backendAddrs.isEmpty()) {
            throw new PolicyException(
                "Received ONLY balancer addresses but grpclb runtime is missing");
          }
          if (!roundRobinDueToGrpclbDepMissing) {
            // We don't log the warning every time we have an update.
            roundRobinDueToGrpclbDepMissing = true;
            String errorMsg = "Found balancer addresses but grpclb runtime is missing."
                + " Will use round_robin. Please include grpc-grpclb in your runtime depedencies.";
            helper.getChannelLogger().log(ChannelLogLevel.ERROR, errorMsg);
            logger.warning(errorMsg);
          }
          return new PolicySelection(
              getProviderOrThrow(
                  "round_robin", "received balancer addresses but grpclb runtime is missing"),
              backendAddrs, null);
        }
        return new PolicySelection(grpclbProvider, servers, null);
      }
      // No balancer address this time.  If balancer address shows up later, we want to make sure
      // the warning is logged one more time.
      roundRobinDueToGrpclbDepMissing = false;

      // No config nor balancer address. Use default.
      return new PolicySelection(
          getProviderOrThrow(defaultPolicy, "using default policy"), servers, null);
    }
  }

  private LoadBalancerProvider getProviderOrThrow(String policy, String choiceReason)
      throws PolicyException {
    LoadBalancerProvider provider = registry.getProvider(policy);
    if (provider == null) {
      throw new PolicyException(
          "Trying to load '" + policy + "' because " + choiceReason + ", but it's unavailable");
    }
    return provider;
  }

  /**
   * Unlike a normal {@link LoadBalancer.Factory}, this accepts a full service config rather than
   * the LoadBalancingConfig.
   *
   * @return null if no selection could be made.
   */
  @Nullable
  ConfigOrError selectLoadBalancerPolicy(Map<String, ?> serviceConfig) {
    try {
      List<LbConfig> loadBalancerConfigs = null;
      if (serviceConfig != null) {
        List<Map<String, ?>> rawLbConfigs =
            ServiceConfigUtil.getLoadBalancingConfigsFromServiceConfig(serviceConfig);
        loadBalancerConfigs = ServiceConfigUtil.unwrapLoadBalancingConfigList(rawLbConfigs);
      }
      if (loadBalancerConfigs != null && !loadBalancerConfigs.isEmpty()) {
        List<String> policiesTried = new ArrayList<>();
        for (LbConfig lbConfig : loadBalancerConfigs) {
          String policy = lbConfig.getPolicyName();
          LoadBalancerProvider provider = registry.getProvider(policy);
          if (provider == null) {
            policiesTried.add(policy);
          } else {
            return ConfigOrError.fromConfig(new PolicySelection(
                provider,
                /* serverList= */ null,
                lbConfig.getRawConfigValue()));
          }
        }
        return ConfigOrError.fromError(
            Status.UNKNOWN.withDescription(
                "None of " + policiesTried + " specified by Service Config are available."));
      }
      return null;
    } catch (RuntimeException e) {
      return ConfigOrError.fromError(
          Status.UNKNOWN.withDescription("can't parse load balancer configuration").withCause(e));
    }
  }

  @VisibleForTesting
  static final class PolicyException extends Exception {
    private static final long serialVersionUID = 1L;

    private PolicyException(String msg) {
      super(msg);
    }
  }

  @VisibleForTesting
  static final class PolicySelection {
    final LoadBalancerProvider provider;
    @Nullable final List<EquivalentAddressGroup> serverList;
    // TODO(carl-mastrangelo): make this the non-raw service config object.
    @Nullable final Map<String, ?> config;

    PolicySelection(
        LoadBalancerProvider provider, List<EquivalentAddressGroup> serverList,
        @Nullable Map<String, ?> config) {
      this.provider = checkNotNull(provider, "provider");
      this.serverList = Collections.unmodifiableList(checkNotNull(serverList, "serverList"));
      this.config = config;
    }

    PolicySelection(
        LoadBalancerProvider provider,
        @Nullable Map<String, ?> config) {
      this.provider = checkNotNull(provider, "provider");
      this.serverList = null;
      this.config = config;
    }
  }

  private static final class EmptyPicker extends SubchannelPicker {

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      return PickResult.withNoResult();
    }
  }

  private static final class FailingPicker extends SubchannelPicker {
    private final Status failure;

    FailingPicker(Status failure) {
      this.failure = failure;
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      return PickResult.withError(failure);
    }
  }
}
