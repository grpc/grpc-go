/*
 * Copyright 2017 The gRPC Authors
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

import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import java.net.InetSocketAddress;
import javax.annotation.Nullable;

/**
 * Used to express the result of a proxy lookup.
 */
public final class ProxyParameters {
  public final InetSocketAddress proxyAddress;
  @Nullable public final String username;
  @Nullable public final String password;

  /** Creates an instance. */
  public ProxyParameters(
      InetSocketAddress proxyAddress,
      @Nullable String username,
      @Nullable String password) {
    Preconditions.checkNotNull(proxyAddress);
    // The resolution must be done by the ProxyParameters producer, because consumers
    // may not be allowed to do IO.
    Preconditions.checkState(!proxyAddress.isUnresolved());
    this.proxyAddress = proxyAddress;
    this.username = username;
    this.password = password;
  }

  @Override
  public boolean equals(Object o) {
    if (!(o instanceof ProxyParameters)) {
      return false;
    }
    ProxyParameters that = (ProxyParameters) o;
    return Objects.equal(proxyAddress, that.proxyAddress)
        && Objects.equal(username, that.username)
        && Objects.equal(password, that.password);
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(proxyAddress, username, password);
  }
}
