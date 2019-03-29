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
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.ExperimentalApi;
import io.grpc.LoadBalancer;
import io.grpc.NameResolver;
import io.grpc.Status;
import java.util.List;

@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
public abstract class ForwardingLoadBalancer extends LoadBalancer {
  /**
   * Returns the underlying balancer.
   */
  protected abstract LoadBalancer delegate();

  @Override
  @Deprecated
  public void handleResolvedAddressGroups(
        List<EquivalentAddressGroup> servers,
        @NameResolver.ResolutionResultAttr Attributes attributes) {
    delegate().handleResolvedAddressGroups(servers, attributes);
  }

  @Override
  public void handleResolvedAddresses(ResolvedAddresses resolvedAddresses) {
    delegate().handleResolvedAddresses(resolvedAddresses);
  }

  @Override
  public void handleNameResolutionError(Status error) {
    delegate().handleNameResolutionError(error);
  }

  @Override
  public void handleSubchannelState(
      Subchannel subchannel, ConnectivityStateInfo stateInfo) {
    delegate().handleSubchannelState(subchannel, stateInfo);
  }

  @Override
  public void shutdown() {
    delegate().shutdown();
  }

  @Override
  public boolean canHandleEmptyAddressListFromNameResolution() {
    return delegate().canHandleEmptyAddressListFromNameResolution();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate()).toString();
  }
}
