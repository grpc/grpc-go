/*
 * Copyright 2015, Google Inc. All rights reserved.
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
