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

package io.grpc.util;

import com.google.common.base.MoreObjects;
import io.grpc.Attributes;
import io.grpc.ChannelLogger;
import io.grpc.ConnectivityState;
import io.grpc.EquivalentAddressGroup;
import io.grpc.ExperimentalApi;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.LoadBalancer;
import io.grpc.ManagedChannel;
import io.grpc.NameResolver;
import io.grpc.SynchronizationContext;
import java.util.List;
import java.util.concurrent.ScheduledExecutorService;

@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
public abstract class ForwardingLoadBalancerHelper extends LoadBalancer.Helper {
  /**
   * Returns the underlying helper.
   */
  protected abstract LoadBalancer.Helper delegate();

  @Override
  public Subchannel createSubchannel(List<EquivalentAddressGroup> addrs, Attributes attrs) {
    return delegate().createSubchannel(addrs, attrs);
  }

  @Override
  public void updateSubchannelAddresses(
      Subchannel subchannel, List<EquivalentAddressGroup> addrs) {
    delegate().updateSubchannelAddresses(subchannel, addrs);
  }

  @Override
  public  ManagedChannel createOobChannel(EquivalentAddressGroup eag, String authority) {
    return delegate().createOobChannel(eag, authority);
  }

  @Override
  public void updateOobChannelAddresses(ManagedChannel channel, EquivalentAddressGroup eag) {
    delegate().updateOobChannelAddresses(channel, eag);
  }

  @Override
  public void updateBalancingState(
      ConnectivityState newState, SubchannelPicker newPicker) {
    delegate().updateBalancingState(newState, newPicker);
  }

  @Override
  @Deprecated
  public void runSerialized(Runnable task) {
    delegate().runSerialized(task);
  }

  @Override
  public NameResolver.Factory getNameResolverFactory() {
    return delegate().getNameResolverFactory();
  }

  @Override
  public String getAuthority() {
    return delegate().getAuthority();
  }

  @Override
  public SynchronizationContext getSynchronizationContext() {
    return delegate().getSynchronizationContext();
  }

  @Override
  public ScheduledExecutorService getScheduledExecutorService() {
    return delegate().getScheduledExecutorService();
  }

  @Override
  public ChannelLogger getChannelLogger() {
    return delegate().getChannelLogger();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate()).toString();
  }
}
