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
import com.google.common.base.Preconditions;
import java.net.SocketAddress;

/**
 * A data structure to associate a {@link SocketAddress} with {@link ProxyParameters}.
 */
final class ProxySocketAddress extends SocketAddress {
  private static final long serialVersionUID = -6854992294603212793L;

  private final SocketAddress address;
  private final ProxyParameters proxyParameters;

  @VisibleForTesting
  ProxySocketAddress(SocketAddress address, ProxyParameters proxyParameters) {
    this.address = Preconditions.checkNotNull(address);
    this.proxyParameters = Preconditions.checkNotNull(proxyParameters);
  }

  public ProxyParameters getProxyParameters() {
    return proxyParameters;
  }

  public SocketAddress getAddress() {
    return address;
  }
}
