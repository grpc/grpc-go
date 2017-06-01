/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

package io.grpc.util;

import static com.google.common.base.Preconditions.checkNotNull;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.ExperimentalApi;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.NameResolver;
import io.grpc.Status;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.NoSuchElementException;
import java.util.Set;
import java.util.concurrent.atomic.AtomicReference;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * A {@link LoadBalancer} that provides round-robin load balancing mechanism over the
 * addresses from the {@link NameResolver}.  The sub-lists received from the name resolver
 * are considered to be an {@link EquivalentAddressGroup} and each of these sub-lists is
 * what is then balanced across.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
public class RoundRobinLoadBalancerFactory extends LoadBalancer.Factory {
  private static final RoundRobinLoadBalancerFactory INSTANCE =
      new RoundRobinLoadBalancerFactory();

  private RoundRobinLoadBalancerFactory() {
  }

  /**
   * Gets the singleton instance of this factory.
   */
  public static RoundRobinLoadBalancerFactory getInstance() {
    return INSTANCE;
  }

  @Override
  public LoadBalancer newLoadBalancer(LoadBalancer.Helper helper) {
    return new RoundRobinLoadBalancer(helper);
  }

  @VisibleForTesting
  static class RoundRobinLoadBalancer extends LoadBalancer {
    private final Helper helper;
    private final Map<EquivalentAddressGroup, Subchannel> subchannels =
        new HashMap<EquivalentAddressGroup, Subchannel>();

    @VisibleForTesting
    static final Attributes.Key<AtomicReference<ConnectivityStateInfo>> STATE_INFO =
        Attributes.Key.of("state-info");

    RoundRobinLoadBalancer(Helper helper) {
      this.helper = checkNotNull(helper, "helper");
    }

    @Override
    public void handleResolvedAddressGroups(
        List<EquivalentAddressGroup> servers, Attributes attributes) {
      Set<EquivalentAddressGroup> currentAddrs = subchannels.keySet();
      Set<EquivalentAddressGroup> latestAddrs = stripAttrs(servers);
      Set<EquivalentAddressGroup> addedAddrs = setsDifference(latestAddrs, currentAddrs);
      Set<EquivalentAddressGroup> removedAddrs = setsDifference(currentAddrs, latestAddrs);

      // Create new subchannels for new addresses.
      for (EquivalentAddressGroup addressGroup : addedAddrs) {
        // NB(lukaszx0): we don't merge `attributes` with `subchannelAttr` because subchannel
        // doesn't need them. They're describing the resolved server list but we're not taking
        // any action based on this information.
        Attributes subchannelAttrs = Attributes.newBuilder()
            // NB(lukaszx0): because attributes are immutable we can't set new value for the key
            // after creation but since we can mutate the values we leverge that and set
            // AtomicReference which will allow mutating state info for given channel.
            .set(STATE_INFO, new AtomicReference<ConnectivityStateInfo>(
                ConnectivityStateInfo.forNonError(IDLE)))
            .build();

        Subchannel subchannel = checkNotNull(helper.createSubchannel(addressGroup, subchannelAttrs),
            "subchannel");
        subchannels.put(addressGroup, subchannel);
        subchannel.requestConnection();
      }

      // Shutdown subchannels for removed addresses.
      for (EquivalentAddressGroup addressGroup : removedAddrs) {
        Subchannel subchannel = subchannels.remove(addressGroup);
        subchannel.shutdown();
      }

      updatePicker(getAggregatedError());
    }

    @Override
    public void handleNameResolutionError(Status error) {
      updatePicker(error);
    }

    @Override
    public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {
      if (!subchannels.containsValue(subchannel)) {
        return;
      }
      if (stateInfo.getState() == IDLE) {
        subchannel.requestConnection();
      }
      getSubchannelStateInfoRef(subchannel).set(stateInfo);
      updatePicker(getAggregatedError());
    }

    @Override
    public void shutdown() {
      for (Subchannel subchannel : getSubchannels()) {
        subchannel.shutdown();
      }
    }

    /**
     * Updates picker with the list of active subchannels (state == READY).
     */
    private void updatePicker(@Nullable Status error) {
      List<Subchannel> activeList = filterNonFailingSubchannels(getSubchannels());
      helper.updatePicker(new Picker(activeList, error));
    }

    /**
     * Filters out non-ready subchannels.
     */
    private static List<Subchannel> filterNonFailingSubchannels(
        Collection<Subchannel> subchannels) {
      List<Subchannel> readySubchannels = new ArrayList<Subchannel>(subchannels.size());
      for (Subchannel subchannel : subchannels) {
        if (getSubchannelStateInfoRef(subchannel).get().getState() == READY) {
          readySubchannels.add(subchannel);
        }
      }
      return readySubchannels;
    }

    /**
     * Converts list of {@link EquivalentAddressGroup} to {@link EquivalentAddressGroup} set and
     * remove all attributes.
     */
    private static Set<EquivalentAddressGroup> stripAttrs(List<EquivalentAddressGroup> groupList) {
      Set<EquivalentAddressGroup> addrs = new HashSet<EquivalentAddressGroup>();
      for (EquivalentAddressGroup group : groupList) {
        addrs.add(new EquivalentAddressGroup(group.getAddresses()));
      }
      return addrs;
    }

    /**
     * If all subchannels are TRANSIENT_FAILURE, return the Status associated with an arbitrary
     * subchannel otherwise, return null.
     */
    @Nullable
    private Status getAggregatedError() {
      Status status = null;
      for (Subchannel subchannel : getSubchannels()) {
        ConnectivityStateInfo stateInfo = getSubchannelStateInfoRef(subchannel).get();
        if (stateInfo.getState() != TRANSIENT_FAILURE) {
          return null;
        }
        status = stateInfo.getStatus();
      }
      return status;
    }

    @VisibleForTesting
    Collection<Subchannel> getSubchannels() {
      return subchannels.values();
    }

    private static AtomicReference<ConnectivityStateInfo> getSubchannelStateInfoRef(
        Subchannel subchannel) {
      return checkNotNull(subchannel.getAttributes().get(STATE_INFO), "STATE_INFO");
    }

    private static <T> Set<T> setsDifference(Set<T> a, Set<T> b) {
      Set<T> aCopy = new HashSet<T>(a);
      aCopy.removeAll(b);
      return aCopy;
    }
  }

  @VisibleForTesting
  static final class Picker extends SubchannelPicker {
    @Nullable
    private final Status status;
    private final List<Subchannel> list;
    private final int size;
    @GuardedBy("this")
    private int index = 0;

    Picker(List<Subchannel> list, @Nullable Status status) {
      this.list = Collections.unmodifiableList(list);
      this.size = list.size();
      this.status = status;
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      if (size > 0) {
        return PickResult.withSubchannel(nextSubchannel());
      }

      if (status != null) {
        return PickResult.withError(status);
      }

      return PickResult.withNoResult();
    }

    private Subchannel nextSubchannel() {
      if (size == 0) {
        throw new NoSuchElementException();
      }
      synchronized (this) {
        Subchannel val = list.get(index);
        index++;
        if (index >= size) {
          index = 0;
        }
        return val;
      }
    }

    @VisibleForTesting
    List<Subchannel> getList() {
      return list;
    }

    @VisibleForTesting
    Status getStatus() {
      return status;
    }
  }
}
