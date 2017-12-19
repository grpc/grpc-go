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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.InternalLogId;
import io.grpc.InternalWithLogId;
import io.grpc.LoadBalancer;
import io.grpc.Status;
import io.grpc.grpclb.GrpclbConstants.LbPolicy;
import io.grpc.internal.GrpcAttributes;
import io.grpc.internal.ObjectPool;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ScheduledExecutorService;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * A {@link LoadBalancer} that uses the GRPCLB protocol.
 *
 * <p>Optionally, when requested by the naming system, will delegate the work to a local pick-first
 * or round-robin balancer.
 */
class GrpclbLoadBalancer extends LoadBalancer implements InternalWithLogId {
  private static final Logger logger = Logger.getLogger(GrpclbLoadBalancer.class.getName());

  private final InternalLogId logId = InternalLogId.allocate(getClass().getName());

  private final Helper helper;
  private final Factory pickFirstBalancerFactory;
  private final Factory roundRobinBalancerFactory;
  private final ObjectPool<ScheduledExecutorService> timerServicePool;
  private final TimeProvider time;

  // All mutable states in this class are mutated ONLY from Channel Executor

  private ScheduledExecutorService timerService;

  // If not null, all work is delegated to it.
  @Nullable
  private LoadBalancer delegate;
  private LbPolicy lbPolicy;

  // Null if lbPolicy != GRPCLB
  @Nullable
  private GrpclbState grpclbState;

  GrpclbLoadBalancer(Helper helper, Factory pickFirstBalancerFactory,
      Factory roundRobinBalancerFactory, ObjectPool<ScheduledExecutorService> timerServicePool,
      TimeProvider time) {
    this.helper = checkNotNull(helper, "helper");
    this.pickFirstBalancerFactory =
        checkNotNull(pickFirstBalancerFactory, "pickFirstBalancerFactory");
    this.roundRobinBalancerFactory =
        checkNotNull(roundRobinBalancerFactory, "roundRobinBalancerFactory");
    this.timerServicePool = checkNotNull(timerServicePool, "timerServicePool");
    this.timerService = checkNotNull(timerServicePool.getObject(), "timerService");
    this.time = checkNotNull(time, "time provider");
    setLbPolicy(LbPolicy.GRPCLB);
  }

  @Override
  public InternalLogId getLogId() {
    return logId;
  }

  @Override
  public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo newState) {
    if (delegate != null) {
      delegate.handleSubchannelState(subchannel, newState);
      return;
    }
    if (grpclbState != null) {
      grpclbState.handleSubchannelState(subchannel, newState);
    }
  }

  @Override
  public void handleResolvedAddressGroups(
      List<EquivalentAddressGroup> updatedServers, Attributes attributes) {
    LbPolicy newLbPolicy = attributes.get(GrpclbConstants.ATTR_LB_POLICY);
    // LB addresses and backend addresses are treated separately
    List<LbAddressGroup> newLbAddressGroups = new ArrayList<LbAddressGroup>();
    List<EquivalentAddressGroup> newBackendServers = new ArrayList<EquivalentAddressGroup>();
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

    if (!newLbAddressGroups.isEmpty()) {
      if (newLbPolicy != LbPolicy.GRPCLB) {
        newLbPolicy = LbPolicy.GRPCLB;
        logger.log(
            Level.FINE, "[{0}] Switching to GRPCLB because there is at least one balancer", logId);
      }
    }
    if (newLbPolicy == null) {
      logger.log(Level.FINE, "[{0}] New config missing policy. Using PICK_FIRST", logId);
      newLbPolicy = LbPolicy.PICK_FIRST;
    }

    // Switch LB policy if requested
    setLbPolicy(newLbPolicy);

    // Consume the new addresses
    switch (lbPolicy) {
      case PICK_FIRST:
      case ROUND_ROBIN:
        checkNotNull(delegate, "delegate should not be null. newLbPolicy=" + newLbPolicy);
        delegate.handleResolvedAddressGroups(newBackendServers, attributes);
        break;
      case GRPCLB:
        grpclbState.handleAddresses(newLbAddressGroups, newBackendServers);
        break;
      default:
        // Do nothing
    }
  }

  private void setLbPolicy(LbPolicy newLbPolicy) {
    if (newLbPolicy != lbPolicy) {
      resetStates();
      switch (newLbPolicy) {
        case PICK_FIRST:
          delegate = checkNotNull(pickFirstBalancerFactory.newLoadBalancer(helper),
              "pickFirstBalancerFactory.newLoadBalancer()");
          break;
        case ROUND_ROBIN:
          delegate = checkNotNull(roundRobinBalancerFactory.newLoadBalancer(helper),
              "roundRobinBalancerFactory.newLoadBalancer()");
          break;
        case GRPCLB:
          grpclbState = new GrpclbState(helper, time, timerService, logId);
          break;
        default:
          // Do nohting
      }
    }
    lbPolicy = newLbPolicy;
  }

  private void resetStates() {
    if (delegate != null) {
      delegate.shutdown();
      delegate = null;
    }
    if (grpclbState != null) {
      grpclbState.shutdown();
      grpclbState = null;
    }
  }

  @Override
  public void shutdown() {
    resetStates();
    timerService = timerServicePool.returnObject(timerService);
  }

  @Override
  public void handleNameResolutionError(Status error) {
    if (delegate != null) {
      delegate.handleNameResolutionError(error);
    }
    if (grpclbState != null) {
      grpclbState.propagateError(error);
    }
  }

  @VisibleForTesting
  @Nullable
  GrpclbState getGrpclbState() {
    return grpclbState;
  }

  @VisibleForTesting
  LoadBalancer getDelegate() {
    return delegate;
  }

  @VisibleForTesting
  LbPolicy getLbPolicy() {
    return lbPolicy;
  }
}
