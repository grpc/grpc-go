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

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.PickFirstBalancerFactory;
import io.grpc.Status;
import java.lang.reflect.Method;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import javax.annotation.Nullable;

final class AutoConfiguredLoadBalancerFactory extends LoadBalancer.Factory {

  @VisibleForTesting
  static final String ROUND_ROBIN_LOAD_BALANCER_FACTORY_NAME =
      "io.grpc.util.RoundRobinLoadBalancerFactory";
  @VisibleForTesting
  static final String GRPCLB_LOAD_BALANCER_FACTORY_NAME =
      "io.grpc.grpclb.GrpclbLoadBalancerFactory";

  AutoConfiguredLoadBalancerFactory() {}

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
  static final class AutoConfiguredLoadBalancer extends LoadBalancer {
    private final Helper helper;
    private LoadBalancer delegate;
    private LoadBalancer.Factory delegateFactory;

    AutoConfiguredLoadBalancer(Helper helper) {
      this.helper = helper;
      delegateFactory = PickFirstBalancerFactory.getInstance();
      delegate = delegateFactory.newLoadBalancer(helper);
    }

    //  Must be run inside ChannelExecutor.
    @Override
    public void handleResolvedAddressGroups(
        List<EquivalentAddressGroup> servers, Attributes attributes) {
      Map<String, Object> configMap = attributes.get(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG);
      Factory newlbf;
      try {
        newlbf = decideLoadBalancerFactory(servers, configMap);
      } catch (RuntimeException e) {
        Status s = Status.INTERNAL
            .withDescription("Failed to pick a load balancer from service config")
            .withCause(e);
        helper.updateBalancingState(ConnectivityState.TRANSIENT_FAILURE, new FailingPicker(s));
        delegate.shutdown();
        delegateFactory = null;
        delegate = new NoopLoadBalancer();
        return;
      }

      if (newlbf != null && newlbf != delegateFactory) {
        helper.updateBalancingState(ConnectivityState.CONNECTING, new EmptyPicker());
        delegate.shutdown();
        delegateFactory = newlbf;
        delegate = delegateFactory.newLoadBalancer(helper);
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
    LoadBalancer.Factory getDelegateFactory() {
      return delegateFactory;
    }

    /**
     * Picks a load balancer based on given criteria.  In order of preference:
     *
     * <ol>
     *   <li>User provided lb on the channel.  This is a degenerate case and not handled here.</li>
     *   <li>gRPCLB if on the class path and any gRPC LB balancer addresses are present</li>
     *   <li>RoundRobin if on the class path and picked by the service config</li>
     *   <li>PickFirst if the service config choice does not specify</li>
     * </ol>
     *
     * @param servers The list of servers reported
     * @param config the service config object
     * @return the new load balancer factory, or null if the existing lb should be used.
     */
    @Nullable
    @VisibleForTesting
    static LoadBalancer.Factory decideLoadBalancerFactory(
        List<EquivalentAddressGroup> servers, @Nullable Map<String, Object> config) {
      // Check for balancer addresses
      boolean haveBalancerAddress = false;
      for (EquivalentAddressGroup s : servers) {
        if (s.getAttributes().get(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY) != null) {
          haveBalancerAddress = true;
          break;
        }
      }

      if (haveBalancerAddress) {
        try {
          Class<?> lbFactoryClass = Class.forName(GRPCLB_LOAD_BALANCER_FACTORY_NAME);
          Method getInstance = lbFactoryClass.getMethod("getInstance");
          return (LoadBalancer.Factory) getInstance.invoke(null);
        } catch (RuntimeException e) {
          throw e;
        } catch (Exception e) {
          throw new RuntimeException("Can't get GRPCLB, but balancer addresses were present", e);
        }
      }

      String serviceConfigChoiceBalancingPolicy = null;
      if (config != null) {
        serviceConfigChoiceBalancingPolicy =
            ServiceConfigUtil.getLoadBalancingPolicyFromServiceConfig(config);
      }

      // Check for an explicitly present lb choice
      if (serviceConfigChoiceBalancingPolicy != null) {
        if (serviceConfigChoiceBalancingPolicy.toUpperCase(Locale.ROOT).equals("ROUND_ROBIN")) {
          try {
            Class<?> lbFactoryClass = Class.forName(ROUND_ROBIN_LOAD_BALANCER_FACTORY_NAME);
            Method getInstance = lbFactoryClass.getMethod("getInstance");
            return (LoadBalancer.Factory) getInstance.invoke(null);
          } catch (RuntimeException e) {
            throw e;
          } catch (Exception e) {
            throw new RuntimeException("Can't get Round Robin LB", e);
          }
        }
        throw new IllegalArgumentException(
            "Unknown service config policy: " + serviceConfigChoiceBalancingPolicy);
      }

      return PickFirstBalancerFactory.getInstance();
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
