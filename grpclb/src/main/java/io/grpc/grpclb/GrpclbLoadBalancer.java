/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.Status;
import io.grpc.internal.BackoffPolicy;
import io.grpc.internal.GrpcAttributes;
import io.grpc.internal.TimeProvider;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import javax.annotation.Nullable;

/**
 * A {@link LoadBalancer} that uses the GRPCLB protocol.
 *
 * <p>Optionally, when requested by the naming system, will delegate the work to a local pick-first
 * or round-robin balancer.
 */
class GrpclbLoadBalancer extends LoadBalancer {
  private final SubchannelPool subchannelPool;

  // All mutable states in this class are mutated ONLY from Channel Executor
  @Nullable
  private GrpclbState grpclbState;

  GrpclbLoadBalancer(
      Helper helper,
      SubchannelPool subchannelPool,
      TimeProvider time,
      BackoffPolicy.Provider backoffPolicyProvider) {
    checkNotNull(helper, "helper");
    checkNotNull(time, "time provider");
    checkNotNull(backoffPolicyProvider, "backoffPolicyProvider");
    this.subchannelPool = checkNotNull(subchannelPool, "subchannelPool");
    this.subchannelPool.init(helper);
    grpclbState =
        new GrpclbState(helper, subchannelPool, time, backoffPolicyProvider);
  }

  @Override
  public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo newState) {
    // grpclbState should never be null here since handleSubchannelState cannot be called while the
    // lb is shutdown.
    grpclbState.handleSubchannelState(subchannel, newState);
  }

  @Override
  public void handleResolvedAddressGroups(
      List<EquivalentAddressGroup> updatedServers, Attributes attributes) {
    // LB addresses and backend addresses are treated separately
    List<LbAddressGroup> newLbAddressGroups = new ArrayList<>();
    List<EquivalentAddressGroup> newBackendServers = new ArrayList<>();
    for (EquivalentAddressGroup server : updatedServers) {
      String lbAddrAuthority = server.getAttributes().get(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY);
      if (lbAddrAuthority != null) {
        newLbAddressGroups.add(new LbAddressGroup(server, lbAddrAuthority));
      } else {
        newBackendServers.add(server);
      }
    }

    newLbAddressGroups = Collections.unmodifiableList(newLbAddressGroups);
    newBackendServers = Collections.unmodifiableList(newBackendServers);
    grpclbState.handleAddresses(newLbAddressGroups, newBackendServers);
  }

  private void resetStates() {
    if (grpclbState != null) {
      grpclbState.shutdown();
      grpclbState = null;
    }
  }

  @Override
  public void shutdown() {
    resetStates();
  }

  @Override
  public void handleNameResolutionError(Status error) {
    if (grpclbState != null) {
      grpclbState.propagateError(error);
    }
  }

  @VisibleForTesting
  @Nullable
  GrpclbState getGrpclbState() {
    return grpclbState;
  }
}
