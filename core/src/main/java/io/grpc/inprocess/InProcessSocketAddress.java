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

package io.grpc.inprocess;

import static com.google.common.base.Preconditions.checkNotNull;

import java.net.SocketAddress;

/**
 * Custom SocketAddress class for {@link InProcessTransport}.
 */
public final class InProcessSocketAddress extends SocketAddress {
  private static final long serialVersionUID = -2803441206326023474L;

  private final String name;

  /**
   * @param name - The name of the inprocess channel or server.
   * @since 1.0.0
   */
  public InProcessSocketAddress(String name) {
    this.name = checkNotNull(name, "name");
  }

  /**
   * Gets the name of the inprocess channel or server.
   *
   * @since 1.0.0
   */
  public String getName() {
    return name;
  }

  /**
   * @since 1.14.0
   */
  @Override
  public String toString() {
    return name;
  }

  /**
   * @since 1.15.0
   */
  @Override
  public int hashCode() {
    return name.hashCode();
  }

  /**
   * @since 1.15.0
   */
  @Override
  public boolean equals(Object obj) {
    if (!(obj instanceof InProcessSocketAddress)) {
      return false;
    }
    return name.equals(((InProcessSocketAddress) obj).name);
  }
}
