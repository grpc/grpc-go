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
import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.ConnectivityState;
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
import java.util.EnumSet;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.NoSuchElementException;
import java.util.Set;
import java.util.concurrent.atomic.AtomicIntegerFieldUpdater;
import javax.annotation.Nullable;

/**
 * A {@link LoadBalancer} that provides round-robin load balancing mechanism over the
 * addresses from the {@link NameResolver}.  The sub-lists received from the name resolver
 * are considered to be an {@link EquivalentAddressGroup} and each of these sub-lists is
 * what is then balanced across.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
public final class RoundRobinLoadBalancerFactory extends LoadBalancer.Factory {

  private static final RoundRobinLoadBalancerFactory INSTANCE =
      new RoundRobinLoadBalancerFactory();

  private RoundRobinLoadBalancerFactory() {}

  /**
   * A lighter weight Reference than AtomicReference.
   */
  @VisibleForTesting
  static final class Ref<T> {
    T value;

    Ref(T value) {
      this.value = value;
    }
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
  static final class RoundRobinLoadBalancer extends LoadBalancer {
    @VisibleForTesting
    static final Attributes.Key<Ref<ConnectivityStateInfo>> STATE_INFO =
        Attributes.Key.of("state-info");

    private final Helper helper;
    private final Map<EquivalentAddressGroup, Subchannel> subchannels =
        new HashMap<EquivalentAddressGroup, Subchannel>();

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
            .set(
                STATE_INFO, new Ref<ConnectivityStateInfo>(ConnectivityStateInfo.forNonError(IDLE)))
            .build();

        Subchannel subchannel =
            checkNotNull(helper.createSubchannel(addressGroup, subchannelAttrs), "subchannel");
        subchannels.put(addressGroup, subchannel);
        subchannel.requestConnection();
      }

      // Shutdown subchannels for removed addresses.
      for (EquivalentAddressGroup addressGroup : removedAddrs) {
        Subchannel subchannel = subchannels.remove(addressGroup);
        subchannel.shutdown();
      }

      updateBalancingState(getAggregatedState(), getAggregatedError());
    }

    @Override
    public void handleNameResolutionError(Status error) {
      updateBalancingState(TRANSIENT_FAILURE, error);
    }

    @Override
    public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {
      if (subchannels.get(subchannel.getAddresses()) != subchannel) {
        return;
      }
      if (stateInfo.getState() == IDLE) {
        subchannel.requestConnection();
      }
      getSubchannelStateInfoRef(subchannel).value = stateInfo;
      updateBalancingState(getAggregatedState(), getAggregatedError());
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
    private void updateBalancingState(ConnectivityState state, Status error) {
      List<Subchannel> activeList = filterNonFailingSubchannels(getSubchannels());
      helper.updateBalancingState(state, new Picker(activeList, error));
    }

    /**
     * Filters out non-ready subchannels.
     */
    private static List<Subchannel> filterNonFailingSubchannels(
        Collection<Subchannel> subchannels) {
      List<Subchannel> readySubchannels = new ArrayList<Subchannel>(subchannels.size());
      for (Subchannel subchannel : subchannels) {
        if (getSubchannelStateInfoRef(subchannel).value.getState() == READY) {
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
      Set<EquivalentAddressGroup> addrs = new HashSet<EquivalentAddressGroup>(groupList.size());
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
        ConnectivityStateInfo stateInfo = getSubchannelStateInfoRef(subchannel).value;
        if (stateInfo.getState() != TRANSIENT_FAILURE) {
          return null;
        }
        status = stateInfo.getStatus();
      }
      return status;
    }

    private ConnectivityState getAggregatedState() {
      Set<ConnectivityState> states = EnumSet.noneOf(ConnectivityState.class);
      for (Subchannel subchannel : getSubchannels()) {
        states.add(getSubchannelStateInfoRef(subchannel).value.getState());
      }
      if (states.contains(READY)) {
        return READY;
      }
      if (states.contains(CONNECTING)) {
        return CONNECTING;
      }
      if (states.contains(IDLE)) {
        // This subchannel IDLE is not because of channel IDLE_TIMEOUT, in which case LB is already
        // shutdown.
        // RRLB will request connection immediately on subchannel IDLE.
        return CONNECTING;
      }
      return TRANSIENT_FAILURE;
    }

    @VisibleForTesting
    Collection<Subchannel> getSubchannels() {
      return subchannels.values();
    }

    private static Ref<ConnectivityStateInfo> getSubchannelStateInfoRef(
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
    private static final AtomicIntegerFieldUpdater<Picker> indexUpdater =
        AtomicIntegerFieldUpdater.newUpdater(Picker.class, "index");

    @Nullable
    private final Status status;
    private final List<Subchannel> list;
    @SuppressWarnings("unused")
    private volatile int index = -1; // start off at -1 so the address on first use is 0.

    Picker(List<Subchannel> list, @Nullable Status status) {
      this.list = list;
      this.status = status;
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      if (list.size() > 0) {
        return PickResult.withSubchannel(nextSubchannel());
      }

      if (status != null) {
        return PickResult.withError(status);
      }

      return PickResult.withNoResult();
    }

    private Subchannel nextSubchannel() {
      if (list.isEmpty()) {
        throw new NoSuchElementException();
      }
      int size = list.size();

      int i = indexUpdater.incrementAndGet(this);
      if (i >= size) {
        int oldi = i;
        i %= size;
        indexUpdater.compareAndSet(this, oldi, i);
      }
      return list.get(i);
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
