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

package io.grpc;

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import javax.annotation.Nullable;

/**
 * An {@link ProxiedSocketAddress} for making a connection to an endpoint via an HTTP CONNECT proxy.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/5279")
public final class HttpConnectProxiedSocketAddress extends ProxiedSocketAddress {
  private static final long serialVersionUID = 0L;

  private final SocketAddress proxyAddress;
  private final InetSocketAddress targetAddress;
  @Nullable
  private final String username;
  @Nullable
  private final String password;

  private HttpConnectProxiedSocketAddress(
      SocketAddress proxyAddress,
      InetSocketAddress targetAddress,
      @Nullable String username,
      @Nullable String password) {
    checkNotNull(proxyAddress, "proxyAddress");
    checkNotNull(targetAddress, "targetAddress");
    // The resolution must be done by the HttpConnectProxiedSocketAddress producer, because
    // consumers may not be allowed to do IO.
    if (proxyAddress instanceof InetSocketAddress) {
      checkState(!((InetSocketAddress) proxyAddress).isUnresolved(),
          "The proxy address %s is not resolved", proxyAddress);
    }
    this.proxyAddress = proxyAddress;
    this.targetAddress = targetAddress;
    this.username = username;
    this.password = password;
  }

  /**
   * Returns the password used to connect to the proxy. {@code null} if there is no password.
   */
  @Nullable
  public String getPassword() {
    return password;
  }

  /**
   * Returns the username used to connect to the proxy. {@code null} if there is no username.
   */
  @Nullable
  public String getUsername() {
    return username;
  }

  /**
   * Returns the address to the proxy, which is already resolved.
   */
  public SocketAddress getProxyAddress() {
    return proxyAddress;
  }

  /**
   * Returns the address to the target server.
   */
  public InetSocketAddress getTargetAddress() {
    return targetAddress;
  }

  @Override
  public boolean equals(Object o) {
    if (!(o instanceof HttpConnectProxiedSocketAddress)) {
      return false;
    }
    HttpConnectProxiedSocketAddress that = (HttpConnectProxiedSocketAddress) o;
    return Objects.equal(proxyAddress, that.proxyAddress)
        && Objects.equal(targetAddress, that.targetAddress)
        && Objects.equal(username, that.username)
        && Objects.equal(password, that.password);
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(proxyAddress, targetAddress, username, password);
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("proxyAddr", proxyAddress)
        .add("targetAddr", targetAddress)
        .add("username", username)
        // Intentionally mask out password
        .add("hasPassword", password != null)
        .toString();
  }

  /**
   * Create a new builder.
   */
  public static Builder newBuilder() {
    return new Builder();
  }

  /**
   * The builder for {@link HttpConnectProxiedSocketAddress}.
   */
  public static final class Builder {

    private SocketAddress proxyAddress;
    private InetSocketAddress targetAddress;
    @Nullable
    private String username;
    @Nullable
    private String password;

    private Builder() {
    }

    /**
     * Sets the address to the proxy, which is already resolved.  This is a required field.
     */
    public Builder setProxyAddress(SocketAddress proxyAddress) {
      this.proxyAddress = checkNotNull(proxyAddress, "proxyAddress");
      return this;
    }

    /**
     * Sets the address to the target.  This is a required field.
     */
    public Builder setTargetAddress(InetSocketAddress targetAddress) {
      this.targetAddress = checkNotNull(targetAddress, "targetAddress");
      return this;
    }

    /**
     * Sets the username used to connect to the proxy.  This is an optional field and can be {@code
     * null}.
     */
    public Builder setUsername(@Nullable String username) {
      this.username = username;
      return this;
    }

    /**
     * Sets the password used to connect to the proxy.  This is an optional field and can be {@code
     * null}.
     */
    public Builder setPassword(@Nullable String password) {
      this.password = password;
      return this;
    }

    /**
     * Creates an {@code HttpConnectProxiedSocketAddress}.
     */
    public HttpConnectProxiedSocketAddress build() {
      return new HttpConnectProxiedSocketAddress(proxyAddress, targetAddress, username, password);
    }
  }
}
