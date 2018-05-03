/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.benchmarks;

import com.google.errorprone.annotations.Immutable;
import java.net.InetSocketAddress;
import java.net.SocketAddress;

/**
 * Verifies whether or not the given {@link SocketAddress} is valid.
 */
@Immutable
public interface SocketAddressValidator {
  /**
   * Verifier for {@link InetSocketAddress}es.
   */
  SocketAddressValidator INET = new SocketAddressValidator() {
    @Override
    public boolean isValidSocketAddress(SocketAddress address) {
      return address instanceof InetSocketAddress;
    }
  };

  /**
   * Verifier for Netty Unix Domain Socket addresses.
   */
  SocketAddressValidator UDS = new SocketAddressValidator() {
    @Override
    public boolean isValidSocketAddress(SocketAddress address) {
      return "DomainSocketAddress".equals(address.getClass().getSimpleName());
    }
  };

  /**
   * Returns {@code true} if the given address is valid.
   */
  boolean isValidSocketAddress(SocketAddress address);
}
