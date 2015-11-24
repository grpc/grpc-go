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

import com.google.common.base.Supplier;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;

import io.grpc.internal.BlankFutureProvider;
import io.grpc.internal.ClientTransport;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.List;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * A {@link LoadBalancer} that provides simple round-robin and pick-first routing mechanism over the
 * addresses from the {@link NameResolver}.
 */
// TODO(zhangkun83): Only pick-first is implemented. We need to implement round-robin.
@ExperimentalApi
public final class SimpleLoadBalancerFactory extends LoadBalancer.Factory {

  private static final SimpleLoadBalancerFactory instance = new SimpleLoadBalancerFactory();

  private SimpleLoadBalancerFactory() {
  }

  public static SimpleLoadBalancerFactory getInstance() {
    return instance;
  }

  @Override
  public LoadBalancer newLoadBalancer(String serviceName, TransportManager tm) {
    return new SimpleLoadBalancer(tm);
  }

  private static class SimpleLoadBalancer extends LoadBalancer {
    private final Object lock = new Object();

    @GuardedBy("lock")
    private EquivalentAddressGroup addresses;
    @GuardedBy("lock")
    private final BlankFutureProvider<ClientTransport> pendingPicks =
        new BlankFutureProvider<ClientTransport>();
    @GuardedBy("lock")
    private StatusException nameResolutionError;

    private final TransportManager tm;

    private SimpleLoadBalancer(TransportManager tm) {
      this.tm = tm;
    }

    @Override
    public ListenableFuture<ClientTransport> pickTransport(@Nullable RequestKey requestKey) {
      EquivalentAddressGroup addressesCopy;
      synchronized (lock) {
        addressesCopy = addresses;
        if (addressesCopy == null) {
          if (nameResolutionError != null) {
            return Futures.immediateFailedFuture(nameResolutionError);
          }
          return pendingPicks.newBlankFuture();
        }
      }
      return tm.getTransport(addressesCopy);
    }

    @Override
    public void handleResolvedAddresses(
        List<ResolvedServerInfo> updatedServers, Attributes config) {
      BlankFutureProvider.FulfillmentBatch<ClientTransport> pendingPicksFulfillmentBatch;
      final EquivalentAddressGroup newAddresses;
      synchronized (lock) {
        ArrayList<SocketAddress> newAddressList =
            new ArrayList<SocketAddress>(updatedServers.size());
        for (ResolvedServerInfo server : updatedServers) {
          newAddressList.add(server.getAddress());
        }
        newAddresses = new EquivalentAddressGroup(newAddressList);
        if (newAddresses.equals(addresses)) {
          return;
        }
        addresses = newAddresses;
        nameResolutionError = null;
        pendingPicksFulfillmentBatch = pendingPicks.createFulfillmentBatch();
      }
      pendingPicksFulfillmentBatch.link(new Supplier<ListenableFuture<ClientTransport>>() {
        @Override public ListenableFuture<ClientTransport> get() {
          return tm.getTransport(newAddresses);
        }
      });
    }

    @Override
    public void handleNameResolutionError(Status error) {
      BlankFutureProvider.FulfillmentBatch<ClientTransport> pendingPicksFulfillmentBatch;
      StatusException statusException =
          error.augmentDescription("Name resolution failed").asException();
      synchronized (lock) {
        pendingPicksFulfillmentBatch = pendingPicks.createFulfillmentBatch();
        nameResolutionError = statusException;
      }
      pendingPicksFulfillmentBatch.fail(statusException);
    }
  }
}
