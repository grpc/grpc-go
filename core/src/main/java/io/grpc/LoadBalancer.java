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

import java.util.List;

import javax.annotation.concurrent.ThreadSafe;

/**
 * A pluggable component that receives resolved addresses from {@link NameResolver} and provides the
 * channel a usable transport when asked.
 *
 * <p>Note to implementations: all methods are expected to return quickly. Any work that may block
 * should be done asynchronously.
 *
 * @param T the transport type to balance
 */
// TODO(zhangkun83): since it's also used for non-loadbalancing cases like pick-first,
// "RequestRouter" might be a better name.
@ExperimentalApi
@ThreadSafe
public abstract class LoadBalancer<T> {
  /**
   * Pick a transport that Channel will use for next RPC.
   *
   * <p>If called after {@link #shutdown} has been called, this method will return
   * a transport that would fail all requests.
   *
   * @param affinity for affinity-based routing
   */
  public abstract T pickTransport(Attributes affinity);

  /**
   * Shuts down this {@code LoadBalancer}.
   */
  public void shutdown() { }

  /**
   * Handles newly resolved addresses and service config from name resolution system.
   *
   * <p>Implementations should not modify the given {@code servers}.
   */
  public void handleResolvedAddresses(List<ResolvedServerInfo> servers, Attributes config) { }

  /**
   * Handles an error from the name resolution system.
   *
   * @param error a non-OK status
   */
  public void handleNameResolutionError(Status error) { }

  /**
   * Called when a transport is fully connected and ready to accept traffic.
   */
  public void handleTransportReady(EquivalentAddressGroup addressGroup) { }

  /**
   * Called when a transport is shutting down.
   */
  public void handleTransportShutdown(EquivalentAddressGroup addressGroup, Status s) { }

  public abstract static class Factory {
    /**
     * Creates a {@link LoadBalancer} that will be used inside a channel.
     *
     * @param serviceName the DNS-style service name, which is also the authority
     * @param tm the interface where an {@code LoadBalancer} implementation gets connected
     *               transports from
     */
    public abstract <T> LoadBalancer<T> newLoadBalancer(String serviceName, TransportManager<T> tm);
  }
}
