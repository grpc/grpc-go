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

package io.grpc;

import com.google.common.util.concurrent.ListenableFuture;

import io.grpc.internal.ClientTransport;

import java.net.SocketAddress;

/**
 * Manages transport life-cycles and provide ready-to-use transports.
 */
@ExperimentalApi
public abstract class TransportManager {
  /**
   * Advises this {@code TransportManager} to retain transports only to these servers, for warming
   * up connections and discarding unused connections.
   */
  public abstract void updateRetainedTransports(SocketAddress[] addrs);

  /**
   * Returns the future of a transport for any of the addresses from the given address group.
   *
   * <p>If the channel has been shut down, the value of the future will be {@code null}.
   *
   * <p>Cancelling the returned future has no effect.
   */
  // TODO(zhangkun83): GrpcLoadBalancer will use this to get transport to connect to LB servers,
  // which would have a different authority than the primary servers. We need to figure out how to
  // do it.
  public abstract ListenableFuture<ClientTransport> getTransport(
      EquivalentAddressGroup addressGroup);
}
