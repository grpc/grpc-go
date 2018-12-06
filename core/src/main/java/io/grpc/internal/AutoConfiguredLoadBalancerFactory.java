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
import io.grpc.Status;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Map.Entry;
import java.util.logging.Logger;
import javax.annotation.Nullable;

@VisibleForTesting
public final class AutoConfiguredLoadBalancerFactory extends LoadBalancer.Factory {
  private static final String DEFAULT_POLICY = "pick_first";
  private static final Logger logger =
      Logger.getLogger(AutoConfiguredLoadBalancerFactory.class.getName());

  private final LoadBalancerRegistry registry;

  public AutoConfiguredLoadBalancerFactory() {
    this(LoadBalancerRegistry.getDefaultRegistry());
  }

  @VisibleForTesting
  AutoConfiguredLoadBalancerFactory(LoadBalancerRegistry registry) {
    this.registry = checkNotNull(registry, "registry");
  }

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
  public final class AutoConfiguredLoadBalancer extends LoadBalancer {
    private final Helper helper;
    private LoadBalancer delegate;
    private LoadBalancerProvider delegateProvider;
    private boolean roundRobinDueToGrpclbDepMissing;

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
      getDelegate().handleResolvedAddressGroups(selection.serverList, attributes);
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
    PolicySelection decideLoadBalancerProvider(
        List<EquivalentAddressGroup> servers, @Nullable Map<String, Object> config)
        throws PolicyException {
      // Check for balancer addresses
      boolean haveBalancerAddress = false;
      List<EquivalentAddressGroup> backendAddrs = new ArrayList<EquivalentAddressGroup>();
      for (EquivalentAddressGroup s : servers) {
        if (s.getAttributes().get(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY) != null) {
          haveBalancerAddress = true;
        } else {
          backendAddrs.add(s);
        }
      }

      if (haveBalancerAddress) {
        LoadBalancerProvider grpclbProvider = registry.getProvider("grpclb");
        if (grpclbProvider == null) {
          if (backendAddrs.isEmpty()) {
            throw new PolicyException(
                "Received ONLY balancer addresses but grpclb runtime is missing");
          }
          if (!roundRobinDueToGrpclbDepMissing) {
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
        } else {
          return new PolicySelection(grpclbProvider, servers, null);
        }
      }
      roundRobinDueToGrpclbDepMissing = false;

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
              helper.getChannelLogger().log(
                  ChannelLogLevel.DEBUG,
                  "{0} specified by Service Config are not available", policiesTried);
            }
            return new PolicySelection(provider, servers, (Map) entry.getValue());
          }
          policiesTried.add(policy);
        }
        throw new PolicyException(
            "None of " + policiesTried + " specified by Service Config are available.");
      }
      return new PolicySelection(
          getProviderOrThrow(DEFAULT_POLICY, "using default policy"), servers, null);
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
    final List<EquivalentAddressGroup> serverList;
    @Nullable final Map<String, Object> config;

    @SuppressWarnings("unchecked")
    PolicySelection(
        LoadBalancerProvider provider, List<EquivalentAddressGroup> serverList,
        @Nullable Map<?, ?> config) {
      this.provider = checkNotNull(provider, "provider");
      this.serverList = Collections.unmodifiableList(checkNotNull(serverList, "serverList"));
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
