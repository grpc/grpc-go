/*
 * Copyright 2014, Google Inc. All rights reserved.
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

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;

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
    // TODO(zhangkun83): virtually any LoadBalancer would need to handle picks before name
    // resolution is done, we may want to move the related logic into ManagedChannelImpl.
    @GuardedBy("servers")
    private List<SettableFuture<ClientTransport>> pendingPicks;
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
          SettableFuture<ClientTransport> future = SettableFuture.create();
          if (pendingPicks == null) {
            pendingPicks = new ArrayList<SettableFuture<ClientTransport>>();
          }
          pendingPicks.add(future);
          return future;
        }
        currentServer = servers.get(currentServerIndex);
      }
      return tm.getTransport(currentServer.getAddress());
    }

    @Override
    public void handleResolvedAddresses(
        List<ResolvedServerInfo> updatedServers, Attributes config) {
      List<SettableFuture<ClientTransport>> pendingPicksCopy = null;
      ResolvedServerInfo currentServer = null;
      synchronized (servers) {
        nameResolutionError = null;
        servers.clear();
        for (ResolvedServerInfo addr : updatedServers) {
          servers.add(addr);
        }
        if (!servers.isEmpty()) {
          pendingPicksCopy = pendingPicks;
          pendingPicks = null;
          if (currentServerIndex >= servers.size()) {
            currentServerIndex = 0;
          }
          currentServer = servers.get(currentServerIndex);
        }
      }
      if (pendingPicksCopy != null) {
        // If pendingPicksCopy != null, then servers.isEmpty() == false, then
        // currentServer must have been assigned.
        Preconditions.checkState(currentServer != null, "currentServer is null");
        for (final SettableFuture<ClientTransport> pendingPick : pendingPicksCopy) {
          ListenableFuture<ClientTransport> future = tm.getTransport(currentServer.getAddress());
          Futures.addCallback(future, new FutureCallback<ClientTransport>() {
            @Override public void onSuccess(ClientTransport result) {
              pendingPick.set(result);
            }

            @Override public void onFailure(Throwable t) {
              pendingPick.setException(t);
            }
          });
        }
      }
    }

    @Override
    public void handleNameResolutionError(Status error) {
      List<SettableFuture<ClientTransport>> pendingPicksCopy = null;
      StatusException statusException = error.asException();
      synchronized (servers) {
        pendingPicksCopy = pendingPicks;
        pendingPicks = null;
        nameResolutionError = statusException;
      }
      if (pendingPicksCopy != null) {
        for (SettableFuture<ClientTransport> pendingPick : pendingPicksCopy) {
          pendingPick.setException(statusException);
        }
      }
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
