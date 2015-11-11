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
    @GuardedBy("servers")
    private final List<ResolvedServerInfo> servers = new ArrayList<ResolvedServerInfo>();
    @GuardedBy("servers")
    private int currentServerIndex;
    @GuardedBy("servers")
    private final BlankFutureProvider<ClientTransport> pendingPicks =
        new BlankFutureProvider<ClientTransport>();
    @GuardedBy("servers")
    private StatusException nameResolutionError;

    private final TransportManager tm;

    private SimpleLoadBalancer(TransportManager tm) {
      this.tm = tm;
    }

    @Override
    public ListenableFuture<ClientTransport> pickTransport(@Nullable RequestKey requestKey) {
      ResolvedServerInfo currentServer;
      synchronized (servers) {
        if (servers.isEmpty()) {
          if (nameResolutionError != null) {
            return Futures.immediateFailedFuture(nameResolutionError);
          }
          return pendingPicks.newBlankFuture();
        }
        currentServer = servers.get(currentServerIndex);
      }
      return tm.getTransport(currentServer.getAddress());
    }

    @Override
    public void handleResolvedAddresses(
        List<ResolvedServerInfo> updatedServers, Attributes config) {
      BlankFutureProvider.FulfillmentBatch<ClientTransport> pendingPicksFulfillmentBatch;
      final ResolvedServerInfo currentServer;
      synchronized (servers) {
        nameResolutionError = null;
        servers.clear();
        for (ResolvedServerInfo addr : updatedServers) {
          servers.add(addr);
        }
        if (servers.isEmpty()) {
          return;
        }
        pendingPicksFulfillmentBatch = pendingPicks.createFulfillmentBatch();
        if (currentServerIndex >= servers.size()) {
          currentServerIndex = 0;
        }
        currentServer = servers.get(currentServerIndex);
      }
      pendingPicksFulfillmentBatch.link(new Supplier<ListenableFuture<ClientTransport>>() {
        @Override public ListenableFuture<ClientTransport> get() {
          return tm.getTransport(currentServer.getAddress());
        }
      });
    }

    @Override
    public void handleNameResolutionError(Status error) {
      BlankFutureProvider.FulfillmentBatch<ClientTransport> pendingPicksFulfillmentBatch;
      StatusException statusException =
          error.augmentDescription("Name resolution failed").asException();
      synchronized (servers) {
        pendingPicksFulfillmentBatch = pendingPicks.createFulfillmentBatch();
        nameResolutionError = statusException;
      }
      pendingPicksFulfillmentBatch.fail(statusException);
    }

    @Override
    public void transportShutdown(SocketAddress addr, ClientTransport transport, Status s) {
      if (!s.isOk()) {
        // If the current transport is shut down due to error, move on to the next address in the
        // list
        synchronized (servers) {
          if (addr.equals(servers.get(currentServerIndex).getAddress())) {
            currentServerIndex++;
            if (currentServerIndex >= servers.size()) {
              currentServerIndex = 0;
            }
          }
        }
      }
    }
  }
}
