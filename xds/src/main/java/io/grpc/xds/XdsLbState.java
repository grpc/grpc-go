/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.xds;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.collect.ImmutableList;
import io.grpc.Attributes;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.ManagedChannel;
import io.grpc.Status;
import io.grpc.xds.XdsComms.AdsStreamCallback;
import java.net.SocketAddress;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import javax.annotation.Nullable;

/**
 * The states of an XDS working session of {@link XdsLoadBalancer}.  Created when XdsLoadBalancer
 * switches to the current mode.  Shutdown and discarded when XdsLoadBalancer switches to another
 * mode.
 *
 * <p>There might be two implementations:
 *
 * <ul>
 *   <li>Standard plugin: No child plugin specified in lb config. Lb will send CDS request,
 *       and then EDS requests. EDS requests request for endpoints.</li>
 *   <li>Custom plugin: Child plugin specified in lb config. Lb will send EDS directly. EDS requests
 *       do not request for endpoints.</li>
 * </ul>
 */
class XdsLbState {

  private static final Attributes.Key<AtomicReference<ConnectivityStateInfo>> STATE_INFO =
      Attributes.Key.create("io.grpc.xds.XdsLoadBalancer.stateInfo");
  final String balancerName;

  @Nullable
  final Map<String, Object> childPolicy;

  private final SubchannelStore subchannelStore;
  private final Helper helper;
  private final AdsStreamCallback adsStreamCallback;

  @Nullable
  private XdsComms xdsComms;


  XdsLbState(
      String balancerName,
      @Nullable Map<String, Object> childPolicy,
      @Nullable XdsComms xdsComms,
      Helper helper,
      SubchannelStore subchannelStore,
      AdsStreamCallback adsStreamCallback) {
    this.balancerName = checkNotNull(balancerName, "balancerName");
    this.childPolicy = childPolicy;
    this.xdsComms = xdsComms;
    this.helper = checkNotNull(helper, "helper");
    this.subchannelStore = checkNotNull(subchannelStore, "subchannelStore");
    this.adsStreamCallback = checkNotNull(adsStreamCallback, "adsStreamCallback");
  }

  final void handleResolvedAddressGroups(
      List<EquivalentAddressGroup> servers, Attributes attributes) {

    // start XdsComms if not already alive
    if (xdsComms != null) {
      xdsComms.refreshAdsStream();
    } else {
      // ** This is wrong **
      // FIXME: use name resolver to resolve addresses for balancerName, and create xdsComms in
      // name resolver listener callback.
      // TODO: consider pass a fake EAG as a static final field visible to tests and verify
      // createOobChannel() with this EAG in tests.
      ManagedChannel oobChannel = helper.createOobChannel(
          new EquivalentAddressGroup(ImmutableList.<SocketAddress>of(new SocketAddress() {
          })),
          balancerName);
      xdsComms = new XdsComms(oobChannel, helper, adsStreamCallback);
    }

    // TODO: maybe update picker
  }


  final void handleNameResolutionError(Status error) {
    if (!subchannelStore.hasNonDropBackends()) {
      // TODO: maybe update picker with transient failure
    }
  }

  final void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo newState) {
    // TODO: maybe update picker
  }

  /**
   * Shuts down subchannels and child loadbalancers, and cancels retry timer.
   */
  void shutdown() {
    // TODO: cancel retry timer
    // TODO: shutdown child balancers
    subchannelStore.shutdown();
  }

  @Nullable
  final XdsComms shutdownAndReleaseXdsComms() {
    shutdown();
    XdsComms xdsComms = this.xdsComms;
    this.xdsComms = null;
    return xdsComms;
  }

  /**
   * Manages EAG and locality info for a collection of subchannels, not including subchannels
   * created by the fallback balancer.
   */
  static final class SubchannelStoreImpl implements SubchannelStore {

    SubchannelStoreImpl() {}

    @Override
    public boolean hasReadyBackends() {
      // TODO: impl
      return false;
    }

    @Override
    public boolean hasNonDropBackends() {
      // TODO: impl
      return false;
    }


    @Override
    public boolean hasSubchannel(Subchannel subchannel) {
      // TODO: impl
      return false;
    }

    @Override
    public void shutdown() {
      // TODO: impl
    }
  }

  /**
   * The interface of {@link XdsLbState.SubchannelStoreImpl} that is convenient for testing.
   */
  public interface SubchannelStore {

    boolean hasReadyBackends();

    boolean hasNonDropBackends();

    boolean hasSubchannel(Subchannel subchannel);

    void shutdown();
  }
}
