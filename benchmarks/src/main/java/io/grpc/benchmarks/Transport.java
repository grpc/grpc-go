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

package io.grpc.benchmarks;

import java.net.SocketAddress;

/**
 * All of the supported transports.
 */
public enum Transport {
  NETTY_NIO(true, "The Netty Java NIO transport. Using this with TLS requires "
      + "that the Java bootclasspath be configured with Jetty ALPN boot.",
      SocketAddressValidator.INET),
  NETTY_EPOLL(true, "The Netty native EPOLL transport. Using this with TLS requires that "
      + "OpenSSL be installed and configured as described in "
      + "http://netty.io/wiki/forked-tomcat-native.html. Only supported on Linux.",
      SocketAddressValidator.INET),
  NETTY_UNIX_DOMAIN_SOCKET(false, "The Netty Unix Domain Socket transport. This currently "
      + "does not support TLS.", SocketAddressValidator.UDS),
  OK_HTTP(true, "The OkHttp transport.", SocketAddressValidator.INET);

  public final boolean tlsSupported;
  final String description;
  final SocketAddressValidator socketAddressValidator;

  Transport(boolean tlsSupported, String description,
            SocketAddressValidator socketAddressValidator) {
    this.tlsSupported = tlsSupported;
    this.description = description;
    this.socketAddressValidator = socketAddressValidator;
  }

  /**
   * Validates the given address for this transport.
   *
   * @throws IllegalArgumentException if the given address is invalid for this transport.
   */
  public void validateSocketAddress(SocketAddress address) {
    if (!socketAddressValidator.isValidSocketAddress(address)) {
      throw new IllegalArgumentException(
          "Invalid address " + address + " for transport " + this);
    }
  }

  /**
   * Describe the {@link Transport}.
   */
  public static String getDescriptionString() {
    StringBuilder builder = new StringBuilder("Select the transport to use. Options:\n");
    boolean first = true;
    for (Transport transport : Transport.values()) {
      if (!first) {
        builder.append("\n");
      }
      builder.append(transport.name().toLowerCase());
      builder.append(": ");
      builder.append(transport.description);
      first = false;
    }
    return builder.toString();
  }
}
