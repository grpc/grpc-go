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
import com.google.common.base.Ascii;
import io.grpc.Attributes;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.InternalChannelz.ChannelTrace;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import io.grpc.Status;
import java.util.List;
import java.util.Map;
import javax.annotation.CheckForNull;
import javax.annotation.Nullable;

final class AutoConfiguredLoadBalancerFactory extends LoadBalancer.Factory {
  private static final String DEFAULT_POLICY = "pick_first";

  @Nullable
  private final ChannelTracer channelTracer;
  @Nullable
  private final TimeProvider timeProvider;
  private static final LoadBalancerRegistry registry = LoadBalancerRegistry.getDefaultRegistry();

  AutoConfiguredLoadBalancerFactory(
      @Nullable ChannelTracer channelTracer, @Nullable TimeProvider timeProvider) {
    this.channelTracer = channelTracer;
    this.timeProvider = timeProvider;
  }

  @Override
  public LoadBalancer newLoadBalancer(Helper helper) {
    return new AutoConfiguredLoadBalancer(helper, channelTracer, timeProvider);
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
  static final class AutoConfiguredLoadBalancer extends LoadBalancer {
    private final Helper helper;
    private LoadBalancer delegate;
    private LoadBalancerProvider delegateProvider;
    @CheckForNull
    private ChannelTracer channelTracer;
    @Nullable
    private final TimeProvider timeProvider;

    AutoConfiguredLoadBalancer(
        Helper helper, @Nullable ChannelTracer channelTracer, @Nullable TimeProvider timeProvider) {
      this.helper = helper;
      delegateProvider = registry.getProvider(DEFAULT_POLICY);
      if (delegateProvider == null) {
        throw new IllegalStateException("Could not find LoadBalancer " + DEFAULT_POLICY
            + ". The build probably threw away META-INF/services/io.grpc.LoadBalancerProvider");
      }
      delegate = delegateProvider.newLoadBalancer(helper);
      this.channelTracer = channelTracer;
      this.timeProvider = timeProvider;
      if (channelTracer != null) {
        checkNotNull(timeProvider, "timeProvider");
      }
    }

    //  Must be run inside ChannelExecutor.
    @Override
    public void handleResolvedAddressGroups(
        List<EquivalentAddressGroup> servers, Attributes attributes) {
      Map<String, Object> configMap = attributes.get(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG);
      LoadBalancerProvider newlbp;
      try {
        newlbp = decideLoadBalancerProvider(servers, configMap);
      } catch (PolicyNotFoundException e) {
        Status s = Status.INTERNAL.withDescription(e.getMessage());
        helper.updateBalancingState(ConnectivityState.TRANSIENT_FAILURE, new FailingPicker(s));
        delegate.shutdown();
        delegateProvider = null;
        delegate = new NoopLoadBalancer();
        return;
      }

      if (delegateProvider == null
          || !newlbp.getPolicyName().equals(delegateProvider.getPolicyName())) {
        helper.updateBalancingState(ConnectivityState.CONNECTING, new EmptyPicker());
        delegate.shutdown();
        delegateProvider = newlbp;
        LoadBalancer old = delegate;
        delegate = delegateProvider.newLoadBalancer(helper);
        if (channelTracer != null) {
          channelTracer.reportEvent(new ChannelTrace.Event.Builder()
              .setDescription("Load balancer changed from " + old.getClass().getSimpleName()
                  + " to " + delegate.getClass().getSimpleName())
              .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
              .setTimestampNanos(timeProvider.currentTimeNanos())
              .build());
        }
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
    LoadBalancer getDelegate() {
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
    static LoadBalancerProvider decideLoadBalancerProvider(
        List<EquivalentAddressGroup> servers, @Nullable Map<String, Object> config)
        throws PolicyNotFoundException {
      // Check for balancer addresses
      boolean haveBalancerAddress = false;
      for (EquivalentAddressGroup s : servers) {
        if (s.getAttributes().get(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY) != null) {
          haveBalancerAddress = true;
          break;
        }
      }

      if (haveBalancerAddress) {
        return getProviderOrThrow("grpclb", "NameResolver has returned balancer addresses");
      }

      String serviceConfigChoiceBalancingPolicy = null;
      if (config != null) {
        serviceConfigChoiceBalancingPolicy =
            ServiceConfigUtil.getLoadBalancingPolicyFromServiceConfig(config);
        if (serviceConfigChoiceBalancingPolicy != null) {
          // Handle ASCII specifically rather than relying on the implicit default locale of the str
          return getProviderOrThrow(
              Ascii.toLowerCase(serviceConfigChoiceBalancingPolicy),
              "service-config specifies load-balancing policy");
        }
      }
      return getProviderOrThrow(DEFAULT_POLICY, "Using default policy");
    }
  }

  private static LoadBalancerProvider getProviderOrThrow(String policy, String reason)
      throws PolicyNotFoundException {
    LoadBalancerProvider provider = registry.getProvider(policy);
    if (provider == null) {
      throw new PolicyNotFoundException(policy, reason);
    }
    return provider;
  }

  static final class PolicyNotFoundException extends Exception {
    private static final long serialVersionUID = 1L;

    final String policy;
    final String choiceReason;

    private PolicyNotFoundException(String policy, String choiceReason) {
      this.policy = policy;
      this.choiceReason = choiceReason;
    }

    @Override
    public String getMessage() {
      return "Trying to load '" + policy + "' because " + choiceReason + ", but it's unavailable";
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
