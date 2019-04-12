/*
 * Copyright 2015 The gRPC Authors
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
import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.SHUTDOWN;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;

import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.LoadBalancer.SubchannelStateListener;
import io.grpc.Status;
import java.util.List;

/**
 * A {@link LoadBalancer} that provides no load-balancing over the addresses from the {@link
 * NameResolver}.  The channel's default behavior is used, which is walking down the address list
 * and sticking to the first that works.
 */
final class PickFirstLoadBalancer extends LoadBalancer implements SubchannelStateListener {
  private final Helper helper;
  private Subchannel subchannel;

  PickFirstLoadBalancer(Helper helper) {
    this.helper = checkNotNull(helper, "helper");
  }

  @Override
  public void handleResolvedAddresses(ResolvedAddresses resolvedAddresses) {
    List<EquivalentAddressGroup> servers = resolvedAddresses.getServers();
    if (subchannel == null) {
      subchannel = helper.createSubchannel(
          CreateSubchannelArgs.newBuilder()
              .setAddresses(servers)
              .setStateListener(this)
              .build());

      // The channel state does not get updated when doing name resolving today, so for the moment
      // let LB report CONNECTION and call subchannel.requestConnection() immediately.
      helper.updateBalancingState(CONNECTING, new Picker(PickResult.withSubchannel(subchannel)));
      subchannel.requestConnection();
    } else {
      helper.updateSubchannelAddresses(subchannel, servers);
    }
  }

  @Override
  public void handleNameResolutionError(Status error) {
    if (subchannel != null) {
      subchannel.shutdown();
      subchannel = null;
    }
    // NB(lukaszx0) Whether we should propagate the error unconditionally is arguable. It's fine
    // for time being.
    helper.updateBalancingState(TRANSIENT_FAILURE, new Picker(PickResult.withError(error)));
  }

  @Override
  public void onSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {
    ConnectivityState currentState = stateInfo.getState();
    if (subchannel != PickFirstLoadBalancer.this.subchannel || currentState == SHUTDOWN) {
      return;
    }

    SubchannelPicker picker;
    switch (currentState) {
      case IDLE:
        picker = new RequestConnectionPicker(subchannel);
        break;
      case CONNECTING:
        // It's safe to use RequestConnectionPicker here, so when coming from IDLE we could leave
        // the current picker in-place. But ignoring the potential optimization is simpler.
        picker = new Picker(PickResult.withNoResult());
        break;
      case READY:
        picker = new Picker(PickResult.withSubchannel(subchannel));
        break;
      case TRANSIENT_FAILURE:
        picker = new Picker(PickResult.withError(stateInfo.getStatus()));
        break;
      default:
        throw new IllegalArgumentException("Unsupported state:" + currentState);
    }
    helper.updateBalancingState(currentState, picker);
  }

  @Override
  public void shutdown() {
    if (subchannel != null) {
      subchannel.shutdown();
    }
  }

  /**
   * No-op picker which doesn't add any custom picking logic. It just passes already known result
   * received in constructor.
   */
  private static final class Picker extends SubchannelPicker {
    private final PickResult result;

    Picker(PickResult result) {
      this.result = checkNotNull(result, "result");
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      return result;
    }
  }

  /** Picker that requests connection during pick, and returns noResult. */
  private static final class RequestConnectionPicker extends SubchannelPicker {
    private final Subchannel subchannel;

    RequestConnectionPicker(Subchannel subchannel) {
      this.subchannel = checkNotNull(subchannel, "subchannel");
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      subchannel.requestConnection();
      return PickResult.withNoResult();
    }

    @Override
    public void requestConnection() {
      subchannel.requestConnection();
    }
  }
}
