/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc;

import static com.google.common.base.Preconditions.checkNotNull;
import static io.grpc.ConnectivityState.SHUTDOWN;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.List;

/**
 * A {@link LoadBalancer} that provides no load balancing mechanism over the
 * addresses from the {@link NameResolver}.  The channel's default behavior
 * (currently pick-first) is used for all addresses found.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
public final class PickFirstBalancerFactory extends LoadBalancer.Factory {

  private static final PickFirstBalancerFactory INSTANCE = new PickFirstBalancerFactory();

  private PickFirstBalancerFactory() {
  }

  /**
   * Gets an instance of this factory.
   */
  public static PickFirstBalancerFactory getInstance() {
    return INSTANCE;
  }

  @Override
  public LoadBalancer newLoadBalancer(LoadBalancer.Helper helper) {
    return new PickFirstBalancer(helper);
  }

  @VisibleForTesting
  static final class PickFirstBalancer extends LoadBalancer {
    private final Helper helper;
    private Subchannel subchannel;

    PickFirstBalancer(Helper helper) {
      this.helper = checkNotNull(helper, "helper");
    }

    @Override
    public void handleResolvedAddressGroups(
        List<EquivalentAddressGroup> servers, Attributes attributes) {
      // Flatten servers list received from name resolver into single address group. This means that
      // as far as load balancer is concerned, there's virtually one single server with multiple
      // addresses so the connection will be created only for the first address (pick first).
      EquivalentAddressGroup newEag = flattenEquivalentAddressGroup(servers);
      if (subchannel == null) {
        subchannel = helper.createSubchannel(newEag, Attributes.EMPTY);
        helper.updatePicker(new Picker(PickResult.withSubchannel(subchannel)));
      } else {
        helper.updateSubchannelAddresses(subchannel, newEag);
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
      helper.updatePicker(new Picker(PickResult.withError(error)));
    }

    @Override
    public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {
      ConnectivityState currentState = stateInfo.getState();
      if (subchannel != this.subchannel || currentState == SHUTDOWN) {
        return;
      }

      PickResult pickResult;
      switch (currentState) {
        case CONNECTING:
          pickResult = PickResult.withNoResult();
          break;
        case READY:
        case IDLE:
          pickResult = PickResult.withSubchannel(subchannel);
          break;
        case TRANSIENT_FAILURE:
          pickResult = PickResult.withError(stateInfo.getStatus());
          break;
        default:
          throw new IllegalArgumentException("Unsupported state:" + currentState);
      }

      helper.updatePicker(new Picker(pickResult));
    }

    @Override
    public void shutdown() {
      if (subchannel != null) {
        subchannel.shutdown();
      }
    }

    /**
     * Flattens list of EquivalentAddressGroup objects into one EquivalentAddressGroup object.
     */
    private static EquivalentAddressGroup flattenEquivalentAddressGroup(
        List<EquivalentAddressGroup> groupList) {
      List<SocketAddress> addrs = new ArrayList<SocketAddress>();
      for (EquivalentAddressGroup group : groupList) {
        for (SocketAddress addr : group.getAddresses()) {
          addrs.add(addr);
        }
      }
      return new EquivalentAddressGroup(addrs);
    }
  }

  /**
   * No-op picker which doesn't add any custom picking logic. It just passes already known result
   * received in constructor.
   */
  @VisibleForTesting
  static final class Picker extends SubchannelPicker {
    private final PickResult result;

    Picker(PickResult result) {
      this.result = checkNotNull(result, "result");
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      return result;
    }
  }
}
