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

package io.grpc.internal;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;

import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.Status;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Transports for a single {@link SocketAddress}.
 */
@ThreadSafe
final class TransportSet {
  private static final Logger log = Logger.getLogger(TransportSet.class.getName());
  private static final SettableFuture<ClientTransport> NULL_VALUE_FUTURE;

  static {
    NULL_VALUE_FUTURE = SettableFuture.create();
    NULL_VALUE_FUTURE.set(null);
  }

  private final Object lock = new Object();
  private final EquivalentAddressGroup addressGroup;
  private final String authority;
  private final BackoffPolicy.Provider backoffPolicyProvider;
  private final Callback callback;
  private final ClientTransportFactory transportFactory;
  private final ScheduledExecutorService scheduledExecutor;

  @GuardedBy("lock")
  private int nextAddressIndex;

  @GuardedBy("lock")
  private BackoffPolicy reconnectPolicy;

  // The address index from which the current series of consecutive failing connection attempts
  // started. -1 means the current series have not started.
  // In the case of consecutive failures, the time between two attempts for this address is
  // controlled by connectPolicy.
  @GuardedBy("lock")
  private int headIndex = -1;

  @GuardedBy("lock")
  private final Stopwatch backoffWatch;

  @GuardedBy("lock")
  @Nullable
  private ScheduledFuture<?> reconnectTask;

  /**
   * All transports that are not stopped. At the very least the value of {@link
   * activeTransportFuture} will be present, but previously used transports that still have streams
   * or are stopping may also be present.
   */
  @GuardedBy("lock")
  private final Collection<ClientTransport> transports = new ArrayList<ClientTransport>();

  private final LoadBalancer loadBalancer;

  @GuardedBy("lock")
  private boolean shutdown;

  /**
   * The future for the transport for new outgoing requests. 'lock' must be held when assigning
   * to it.
   */
  @Nullable
  private volatile SettableFuture<ClientTransport> activeTransportFuture;

  TransportSet(EquivalentAddressGroup addressGroup, String authority, LoadBalancer loadBalancer,
      BackoffPolicy.Provider backoffPolicyProvider, ClientTransportFactory transportFactory,
      ScheduledExecutorService scheduledExecutor, Callback callback) {
    this(addressGroup, authority, loadBalancer, backoffPolicyProvider, transportFactory,
        scheduledExecutor, callback, Stopwatch.createUnstarted());
  }

  @VisibleForTesting
  TransportSet(EquivalentAddressGroup addressGroup, String authority, LoadBalancer loadBalancer,
      BackoffPolicy.Provider backoffPolicyProvider, ClientTransportFactory transportFactory,
      ScheduledExecutorService scheduledExecutor, Callback callback, Stopwatch backoffWatch) {
    this.addressGroup = Preconditions.checkNotNull(addressGroup, "addressGroup");
    this.authority = authority;
    this.loadBalancer = loadBalancer;
    this.backoffPolicyProvider = backoffPolicyProvider;
    this.transportFactory = transportFactory;
    this.scheduledExecutor = scheduledExecutor;
    this.callback = callback;
    this.backoffWatch = backoffWatch;
  }

  /**
   * Returns a future for the active transport that will be used to create new streams.
   *
   * <p>If this {@code TransportSet} has been shut down, the returned future will have {@code null}
   * value.
   */
  final ListenableFuture<ClientTransport> obtainActiveTransport() {
    SettableFuture<ClientTransport> savedTransportFuture = activeTransportFuture;
    if (savedTransportFuture != null) {
      return savedTransportFuture;
    }
    synchronized (lock) {
      // Check again, since it could have changed before acquiring the lock
      if (activeTransportFuture == null) {
        // In shutdown(), activeTransportFuture is set to NULL_VALUE_FUTURE, thus if
        // activeTransportFuture is null, shutdown must be false.
        Preconditions.checkState(!shutdown, "already shutdown");
        Preconditions.checkState(activeTransportFuture == null || activeTransportFuture.isDone(),
            "activeTransportFuture is neither null nor done");
        activeTransportFuture = SettableFuture.create();
        scheduleConnection();
      }
      return activeTransportFuture;
    }
  }

  // Can only be called when shutdown == false
  @GuardedBy("lock")
  private void scheduleConnection() {
    Preconditions.checkState(!shutdown, "Already shut down");
    Preconditions.checkState(reconnectTask == null || reconnectTask.isDone(),
        "previous reconnectTask is not done");

    final int currentAddressIndex = nextAddressIndex;
    List<SocketAddress> addrs = addressGroup.getAddresses();
    final SocketAddress address = addrs.get(currentAddressIndex);
    nextAddressIndex++;
    if (nextAddressIndex >= addrs.size()) {
      nextAddressIndex = 0;
    }

    Runnable createTransportRunnable = new Runnable() {
      @Override
      public void run() {
        synchronized (lock) {
          if (shutdown) {
            return;
          }
          if (currentAddressIndex == headIndex) {
            backoffWatch.reset().start();
          }
          ClientTransport newActiveTransport =
              transportFactory.newClientTransport(address, authority);
          log.log(Level.INFO, "Created transport {0} for {1}",
              new Object[] {newActiveTransport, address});
          transports.add(newActiveTransport);
          newActiveTransport.start(
              new TransportListener(newActiveTransport, activeTransportFuture, address));
          Preconditions.checkState(activeTransportFuture.set(newActiveTransport),
              "failed to set the new transport to the future");
        }
      }
    };

    long delayMillis;
    if (currentAddressIndex == headIndex) {
      // Back to the first attempted address. Calculate back-off delay.
      delayMillis =
          reconnectPolicy.nextBackoffMillis() - backoffWatch.elapsed(TimeUnit.MILLISECONDS);
    } else {
      delayMillis = 0;
      if (headIndex == -1) {
        // First connect attempt, or the first attempt since last successful connection.
        headIndex = currentAddressIndex;
        reconnectPolicy = backoffPolicyProvider.get();
      }
    }
    if (delayMillis <= 0) {
      reconnectTask = null;
      // No back-off this time.
      createTransportRunnable.run();
    } else {
      reconnectTask = scheduledExecutor.schedule(
          createTransportRunnable, delayMillis, TimeUnit.MILLISECONDS);
    }
  }

  /**
   * Shut down all transports, may run callback inline.
   */
  final void shutdown() {
    SettableFuture<ClientTransport> savedActiveTransportFuture;
    boolean runCallback = false;
    synchronized (lock) {
      if (shutdown) {
        return;
      }
      shutdown = true;
      savedActiveTransportFuture = activeTransportFuture;
      activeTransportFuture = NULL_VALUE_FUTURE;
      if (transports.isEmpty()) {
        runCallback = true;
      }
      if (reconnectTask != null) {
        reconnectTask.cancel(false);
      }
      // else: the callback will be run once all transports have been terminated
    }
    if (savedActiveTransportFuture != null) {
      if (savedActiveTransportFuture.isDone()) {
        try {
          // Should not throw any exception here
          savedActiveTransportFuture.get().shutdown();
        } catch (Exception e) {
          throw Throwables.propagate(e);
        }
      } else {
        savedActiveTransportFuture.set(null);
      }
    }
    if (runCallback) {
      callback.onTerminated();
    }
  }

  private class TransportListener implements ClientTransport.Listener {
    private final SocketAddress address;
    private final ClientTransport transport;
    private final SettableFuture<ClientTransport> transportFuture;

    public TransportListener(ClientTransport transport,
        SettableFuture<ClientTransport> transportFuture, SocketAddress address) {
      this.transport = transport;
      this.transportFuture = transportFuture;
      this.address = address;
    }

    @GuardedBy("lock")
    private boolean isAttachedToActiveTransport() {
      return activeTransportFuture == transportFuture;
    }

    @Override
    public void transportReady() {
      synchronized (lock) {
        log.log(Level.INFO, "Transport {0} for {1} is ready", new Object[] {transport, address});
        Preconditions.checkState(transportFuture.isDone(), "the transport future is not done");
        if (isAttachedToActiveTransport()) {
          headIndex = -1;
        }
      }
      loadBalancer.transportReady(addressGroup, transport);
    }

    @Override
    public void transportShutdown(Status s) {
      synchronized (lock) {
        log.log(Level.INFO, "Transport {0} for {1} is being shutdown",
            new Object[] {transport, address});
        Preconditions.checkState(transportFuture.isDone(), "the transport future is not done");
        if (isAttachedToActiveTransport()) {
          activeTransportFuture = null;
        }
      }
      loadBalancer.transportShutdown(addressGroup, transport, s);
    }

    @Override
    public void transportTerminated() {
      boolean runCallback = false;
      synchronized (lock) {
        log.log(Level.INFO, "Transport {0} for {1} is terminated",
            new Object[] {transport, address});
        Preconditions.checkState(!isAttachedToActiveTransport(),
            "Listener is still attached to activeTransportFuture. "
            + "Seems transportTerminated was not called.");
        transports.remove(transport);
        if (shutdown && transports.isEmpty()) {
          runCallback = true;
        }
      }
      if (runCallback) {
        callback.onTerminated();
      }
    }
  }

  interface Callback {
    void onTerminated();
  }
}
