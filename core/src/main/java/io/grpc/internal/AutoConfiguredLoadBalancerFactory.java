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
import io.grpc.ChannelLogger;
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
import io.grpc.Status;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Map.Entry;
import javax.annotation.Nullable;

@VisibleForTesting
public final class AutoConfiguredLoadBalancerFactory extends LoadBalancer.Factory {
  private static final String DEFAULT_POLICY = "pick_first";

  private static final LoadBalancerRegistry registry = LoadBalancerRegistry.getDefaultRegistry();

  @Override
  public LoadBalancer newLoadBalancer(Helper helper) {
    return new AutoConfiguredLoadBalancer(helper);
  }

  private static final class NoopLoadBalancer extends LoadBalancer {

    @Override
    public void handleResolvedAddressGroups(List<EquivalentAddressGroup> s, Attributes a) {}

    @Override
    public void handleNameResolutionError(Status error) {}

    @Override
    public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {}

    @Override
    public void shutdown() {}
  }


  @VisibleForTesting
  public static final class AutoConfiguredLoadBalancer extends LoadBalancer {
    private final Helper helper;
    private LoadBalancer delegate;
    private LoadBalancerProvider delegateProvider;

    AutoConfiguredLoadBalancer(Helper helper) {
      this.helper = helper;
      delegateProvider = registry.getProvider(DEFAULT_POLICY);
      if (delegateProvider == null) {
        throw new IllegalStateException("Could not find LoadBalancer " + DEFAULT_POLICY
            + ". The build probably threw away META-INF/services/io.grpc.LoadBalancerProvider");
      }
      delegate = delegateProvider.newLoadBalancer(helper);
    }

    //  Must be run inside ChannelExecutor.
    @Override
    public void handleResolvedAddressGroups(
        List<EquivalentAddressGroup> servers, Attributes attributes) {
      if (attributes.get(ATTR_LOAD_BALANCING_CONFIG) != null) {
        throw new IllegalArgumentException(
            "Unexpected ATTR_LOAD_BALANCING_CONFIG from upstream: "
            + attributes.get(ATTR_LOAD_BALANCING_CONFIG));
      }
      Map<String, Object> configMap = attributes.get(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG);
      PolicySelection selection;
      try {
        selection = decideLoadBalancerProvider(servers, configMap, helper.getChannelLogger());
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
      getDelegate().handleResolvedAddressGroups(servers, attributes);
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
  }

  /**
   * Picks a load balancer based on given criteria.  In order of preference:
   *
   * <ol>
   *   <li>User provided lb on the channel.  This is a degenerate case and not handled here.</li>
   *   <li>"grpclb" if any gRPC LB balancer addresses are present</li>
   *   <li>The policy picked by the service config</li>
   *   <li>"pick_first" if the service config choice does not specify</li>
   * </ol>
   *
   * @param servers The list of servers reported
   * @param config the service config object
   * @return the new load balancer factory, never null
   */
  @VisibleForTesting
  static PolicySelection decideLoadBalancerProvider(
      List<EquivalentAddressGroup> servers, @Nullable Map<String, Object> config,
      ChannelLogger channelLogger) throws PolicyException {
    // Check for balancer addresses
    boolean haveBalancerAddress = false;
    for (EquivalentAddressGroup s : servers) {
      if (s.getAttributes().get(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY) != null) {
        haveBalancerAddress = true;
        break;
      }
    }

    if (haveBalancerAddress) {
      LoadBalancerProvider provider =
          getProviderOrThrow("grpclb", "NameResolver has returned balancer addresses");
      return new PolicySelection(provider, null);
    }

    if (config != null) {
      List<Map<String, Object>> lbConfigs =
          ServiceConfigUtil.getLoadBalancingConfigsFromServiceConfig(config);
      LinkedHashSet<String> policiesTried = new LinkedHashSet<>();
      for (Map<String, Object> lbConfig : lbConfigs) {
        if (lbConfig.size() != 1) {
          throw new PolicyException(
              "There are " + lbConfig.size()
              + " load-balancing configs in a list item. Exactly one is expected. Config="
              + lbConfig);
        }
        Entry<String, Object> entry = lbConfig.entrySet().iterator().next();
        String policy = entry.getKey();
        LoadBalancerProvider provider = registry.getProvider(policy);
        if (provider != null) {
          if (!policiesTried.isEmpty()) {
            channelLogger.log(
                ChannelLogLevel.DEBUG,
                "{0} specified by Service Config are not available", policiesTried);
          }
          return new PolicySelection(provider, (Map) entry.getValue());
        }
        policiesTried.add(policy);
      }
      throw new PolicyException(
          "None of " + policiesTried + " specified by Service Config are available.");
    }
    return new PolicySelection(
        getProviderOrThrow(DEFAULT_POLICY, "using default policy"), null);
  }

  private static LoadBalancerProvider getProviderOrThrow(String policy, String choiceReason)
      throws PolicyException {
    LoadBalancerProvider provider = registry.getProvider(policy);
    if (provider == null) {
      throw new PolicyException(
          "Trying to load '" + policy + "' because " + choiceReason + ", but it's unavailable");
    }
    return provider;
  }

  @VisibleForTesting
  static class PolicyException extends Exception {
    private static final long serialVersionUID = 1L;

    private PolicyException(String msg) {
      super(msg);
    }
  }

  @VisibleForTesting
  static final class PolicySelection {
    final LoadBalancerProvider provider;
    @Nullable final Map<String, Object> config;

    @SuppressWarnings("unchecked")
    PolicySelection(LoadBalancerProvider provider, @Nullable Map<?, ?> config) {
      this.provider = checkNotNull(provider, "provider");
      this.config = (Map<String, Object>) config;
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
