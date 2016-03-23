/*
 * Copyright 2016, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
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
