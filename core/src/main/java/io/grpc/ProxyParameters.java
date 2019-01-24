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

package io.grpc;

import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import javax.annotation.Nullable;

/**
 * Used to express the result of a proxy lookup.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/5113")
public final class ProxyParameters {

  private final SocketAddress proxyAddress;
  @Nullable
  private final String username;
  @Nullable
  private final String password;

  /**
   * Creates an instance.
   */
  private ProxyParameters(
      SocketAddress proxyAddress,
      @Nullable String username,
      @Nullable String password) {
    Preconditions.checkNotNull(proxyAddress);
    // The resolution must be done by the ProxyParameters producer, because consumers
    // may not be allowed to do IO.
    if (proxyAddress instanceof InetSocketAddress) {
      Preconditions.checkState(!((InetSocketAddress)proxyAddress).isUnresolved());
    }
    this.proxyAddress = proxyAddress;
    this.username = username;
    this.password = password;
  }

  @Nullable
  public String getPassword() {
    return password;
  }

  @Nullable
  public String getUsername() {
    return username;
  }

  public SocketAddress getProxyAddress() {
    return proxyAddress;
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

  /**
   * Create a new builder.
   */
  public static Builder forAddress(SocketAddress proxyAddress) {
    return new Builder(proxyAddress);
  }

  /**
   * The helper class to build an Attributes instance.
   */
  public static final class Builder {

    private final SocketAddress proxyAddress;
    @Nullable
    private String username;
    @Nullable
    private String password;

    private Builder(SocketAddress proxyAddress) {
      this.proxyAddress = Preconditions.checkNotNull(proxyAddress, "proxyAddress");
    }

    public Builder username(@Nullable String username) {
      this.username = username;
      return this;
    }

    public Builder password(@Nullable String password) {
      this.password = password;
      return this;
    }

    public ProxyParameters build() {
      return new ProxyParameters(this.proxyAddress, this.username, this.password);
    }
  }
}
