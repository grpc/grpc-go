/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

import com.google.common.base.Objects;
import java.net.SocketAddress;
import javax.annotation.concurrent.Immutable;

/**
 * The information about a server from a {@link NameResolver}.
 *
 * @deprecated This class will be removed along with {@link ResolvedServerInfoGroup}.
 */
@Deprecated
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
@Immutable
public final class ResolvedServerInfo {
  private final SocketAddress address;
  private final Attributes attributes;

  /**
   * Constructs a new resolved server without attributes.
   *
   * @param address the address of the server
   */
  public ResolvedServerInfo(SocketAddress address) {
    this(address, Attributes.EMPTY);
  }

  /**
   * Constructs a new resolved server with attributes.
   *
   * @param address the address of the server
   * @param attributes attributes associated with this address.
   */
  public ResolvedServerInfo(SocketAddress address, Attributes attributes) {
    this.address = checkNotNull(address);
    this.attributes = checkNotNull(attributes);
  }

  /**
   * Returns the address.
   */
  public SocketAddress getAddress() {
    return address;
  }

  /**
   * Returns the associated attributes.
   */
  public Attributes getAttributes() {
    return attributes;
  }

  @Override
  public String toString() {
    return "[address=" + address + ", attrs=" + attributes + "]";
  }

  /**
   * Returns true if the given object is also a {@link ResolvedServerInfo} with an equal address
   * and equal attribute values.
   *
   * <p>Note that if a resolver includes mutable values in the attributes, it is possible for two
   * objects to be considered equal at one point in time and not equal at another (due to concurrent
   * mutation of attribute values).
   *
   * @param o an object.
   * @return true if the given object is a {@link ResolvedServerInfo} with an equal address and
   *     equal attributes.
   */
  @Override
  public boolean equals(Object o) {
    if (this == o) {
      return true;
    }
    if (o == null || getClass() != o.getClass()) {
      return false;
    }
    ResolvedServerInfo that = (ResolvedServerInfo) o;
    return Objects.equal(address, that.address) && Objects.equal(attributes, that.attributes);
  }

  /**
   * Returns a hash code for the server info.
   *
   * <p>Note that if a resolver includes mutable values in the attributes, this object's hash code
   * could change over time. So care must be used when putting these objects into a set or using
   * them as keys for a map.
   *
   * @return a hash code for the server info, computed as described above.
   */
  @Override
  public int hashCode() {
    return Objects.hashCode(address, attributes);
  }
}
