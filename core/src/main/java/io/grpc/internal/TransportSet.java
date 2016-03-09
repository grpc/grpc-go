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

import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.Status;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.Callable;
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
final class TransportSet implements WithLogId {
  private static final Logger log = Logger.getLogger(TransportSet.class.getName());
  private static final ClientTransport SHUTDOWN_TRANSPORT =
      new FailingClientTransport(Status.UNAVAILABLE.withDescription("TransportSet is shutdown"));

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

  // True if the next connect attempt is the first attempt ever, or the one right after a successful
  // connection (i.e., transportReady() was called).  If true, the next connect attempt will start
  // from the first address and will reset back-off.
  @GuardedBy("lock")
  private boolean firstAttempt = true;

  @GuardedBy("lock")
  private final Stopwatch backoffWatch;

  @GuardedBy("lock")
  @Nullable
  private ScheduledFuture<?> reconnectTask;

  /**
   * All transports that are not terminated. At the very least the value of {@link activeTransport}
   * will be present, but previously used transports that still have streams or are stopping may
   * also be present.
   */
  @GuardedBy("lock")
  private final Collection<ManagedClientTransport> transports =
      new ArrayList<ManagedClientTransport>();

  private final LoadBalancer<ClientTransport> loadBalancer;

  @GuardedBy("lock")
  private boolean shutdown;

  /*
   * The transport for new outgoing requests.
   * - If shutdown == true, activeTransport is null (shutdown)
   * - Otherwise, if delayedTransport != null,
   *   activeTransport is delayedTransport (waiting to connect)
   * - Otherwise, activeTransport is either null (initially or when idle)
   *   or points to a real transport (when connecting or connected).
   *
   * 'lock' must be held when assigning to it.
   */
  @Nullable
  private volatile ManagedClientTransport activeTransport;

  @GuardedBy("lock")
  @Nullable
  private DelayedClientTransport delayedTransport;

  TransportSet(EquivalentAddressGroup addressGroup, String authority,
      LoadBalancer<ClientTransport> loadBalancer, BackoffPolicy.Provider backoffPolicyProvider,
      ClientTransportFactory transportFactory, ScheduledExecutorService scheduledExecutor,
      Callback callback) {
    this(addressGroup, authority, loadBalancer, backoffPolicyProvider, transportFactory,
        scheduledExecutor, callback, Stopwatch.createUnstarted());
  }

  @VisibleForTesting
  TransportSet(EquivalentAddressGroup addressGroup, String authority,
      LoadBalancer<ClientTransport> loadBalancer, BackoffPolicy.Provider backoffPolicyProvider,
      ClientTransportFactory transportFactory, ScheduledExecutorService scheduledExecutor,
      Callback callback, Stopwatch backoffWatch) {
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
   * Returns the active transport that will be used to create new streams.
   *
   * <p>Never returns {@code null}.
   */
  final ClientTransport obtainActiveTransport() {
    ClientTransport savedTransport = activeTransport;
    if (savedTransport != null) {
      return savedTransport;
    }
    Callable<ClientTransport> immediateConnectionTask = null;
    synchronized (lock) {
      // Check again, since it could have changed before acquiring the lock
      if (activeTransport == null) {
        if (shutdown) {
          return SHUTDOWN_TRANSPORT;
        }
        delayedTransport = new DelayedClientTransport();
        transports.add(delayedTransport);
        delayedTransport.start(new BaseTransportListener(delayedTransport));
        activeTransport = delayedTransport;
        immediateConnectionTask = scheduleConnection();
      }
      savedTransport = activeTransport;
    }
    if (immediateConnectionTask != null) {
      try {
        return immediateConnectionTask.call();
      } catch (Exception e) {
        throw Throwables.propagate(e);
      }
    }
    return savedTransport;
  }

  /**
   * Schedule a task that creates a new transport.
   *
   * @return if not {@code null}, caller should run the returned callable outside of lock. The
   *         callable returns the real transport that has been created.
   */
  @Nullable
  @GuardedBy("lock")
  private Callable<ClientTransport> scheduleConnection() {
    Preconditions.checkState(reconnectTask == null || reconnectTask.isDone(),
        "previous reconnectTask is not done");

    if (firstAttempt) {
      nextAddressIndex = 0;
    }
    final int currentAddressIndex = nextAddressIndex;
    List<SocketAddress> addrs = addressGroup.getAddresses();
    final SocketAddress address = addrs.get(currentAddressIndex);
    nextAddressIndex++;
    if (nextAddressIndex >= addrs.size()) {
      nextAddressIndex = 0;
    }

    final Callable<ClientTransport> createTransportCallable = new Callable<ClientTransport>() {
      @Override
      public ClientTransport call() {
        DelayedClientTransport savedDelayedTransport;
        ManagedClientTransport newActiveTransport;
        boolean savedShutdown;
        synchronized (lock) {
          savedShutdown = shutdown;
          reconnectTask = null;
          if (currentAddressIndex == 0) {
            backoffWatch.reset().start();
          }
          newActiveTransport = transportFactory.newClientTransport(address, authority);
          if (log.isLoggable(Level.FINE)) {
            log.log(Level.FINE, "[{0}] Created {1} for {2}",
                new Object[] {getLogId(), newActiveTransport.getLogId(), address});
          }
          transports.add(newActiveTransport);
          newActiveTransport.start(
              new TransportListener(newActiveTransport, address));
          if (shutdown) {
            // If TransportSet already shutdown, newActiveTransport is only to take care of pending
            // streams in delayedTransport, but will not serve new streams, and it will be shutdown
            // as soon as it's set to the delayedTransport.
            // activeTransport should have already been set to null by shutdown(). We keep it null.
            Preconditions.checkState(activeTransport == null,
                "Unexpected non-null activeTransport");
          } else {
            activeTransport = newActiveTransport;
          }
          savedDelayedTransport = delayedTransport;
          delayedTransport = null;
        }
        savedDelayedTransport.setTransport(newActiveTransport);
        // This delayed transport will terminate and be removed from transports.
        savedDelayedTransport.shutdown();
        if (savedShutdown) {
          // See comments in the synchronized block above on why we shutdown here.
          newActiveTransport.shutdown();
        }
        return newActiveTransport;
      }
    };

    long delayMillis = 0;
    if (currentAddressIndex == 0) {
      if (firstAttempt) {
        // First connect attempt, or the first attempt since last successful connection.
        reconnectPolicy = backoffPolicyProvider.get();
      } else {
        // Back to the first address. Calculate back-off delay.
        delayMillis =
            reconnectPolicy.nextBackoffMillis() - backoffWatch.elapsed(TimeUnit.MILLISECONDS);
      }
    }
    firstAttempt = false;
    if (log.isLoggable(Level.FINE)) {
      log.log(Level.FINE, "[{0}] Scheduling connection after {1} ms for {2}",
          new Object[]{getLogId(), delayMillis, address});
    }
    if (delayMillis <= 0) {
      reconnectTask = null;
      // No back-off this time.
      // Note createTransportRunnable is not supposed to run under the lock.
      return createTransportCallable;
    } else {
      reconnectTask = scheduledExecutor.schedule(
          new Runnable() {
            @Override public void run() {
              try {
                createTransportCallable.call();
              } catch (Exception e) {
                throw Throwables.propagate(e);
              }
            }
          }, delayMillis, TimeUnit.MILLISECONDS);
      return null;
    }
  }

  /**
   * Shut down all transports, stop creating new streams, but existing streams will continue.
   *
   * <p>May run callback inline.
   */
  final void shutdown() {
    ManagedClientTransport savedActiveTransport;
    boolean runCallback = false;
    synchronized (lock) {
      if (shutdown) {
        return;
      }
      shutdown = true;
      savedActiveTransport = activeTransport;
      activeTransport = null;
      if (transports.isEmpty()) {
        runCallback = true;
        Preconditions.checkState(reconnectTask == null, "Should have no reconnectTask scheduled");
        Preconditions.checkState(delayedTransport == null, "Should have no delayedTransport");
      }  // else: the callback will be run once all transports have been terminated
    }
    if (savedActiveTransport != null) {
      savedActiveTransport.shutdown();
    }
    if (runCallback) {
      callback.onTerminated();
    }
  }

  @GuardedBy("lock")
  private void cancelReconnectTask() {
    if (reconnectTask != null) {
      reconnectTask.cancel(false);
      reconnectTask = null;
    }
  }

  @Override
  public String getLogId() {
    return GrpcUtil.getLogId(this);
  }

  /** Shared base for both delayed and real transports. */
  private class BaseTransportListener implements ManagedClientTransport.Listener {
    protected final ManagedClientTransport transport;

    public BaseTransportListener(ManagedClientTransport transport) {
      this.transport = transport;
    }

    @Override
    public void transportReady() {}

    @Override
    public void transportShutdown(Status status) {}

    @Override
    public void transportTerminated() {
      boolean runCallback = false;
      synchronized (lock) {
        transports.remove(transport);
        if (shutdown && transports.isEmpty()) {
          if (log.isLoggable(Level.FINE)) {
            log.log(Level.FINE, "[{0}] Terminated", getLogId());
          }
          runCallback = true;
          cancelReconnectTask();
        }
      }
      if (runCallback) {
        callback.onTerminated();
      }
    }
  }

  /** Listener for real transports. */
  private class TransportListener extends BaseTransportListener {
    private final SocketAddress address;

    public TransportListener(ManagedClientTransport transport, SocketAddress address) {
      super(transport);
      this.address = address;
    }

    private boolean isAttachedToActiveTransport() {
      return activeTransport == transport;
    }

    @Override
    public void transportReady() {
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is ready",
            new Object[] {getLogId(), transport.getLogId(), address});
      }
      super.transportReady();
      synchronized (lock) {
        if (isAttachedToActiveTransport()) {
          firstAttempt = true;
        }
      }
      loadBalancer.handleTransportReady(addressGroup);
    }

    @Override
    public void transportShutdown(Status s) {
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is being shutdown with status {3}",
            new Object[] {getLogId(), transport.getLogId(), address, s});
      }
      super.transportShutdown(s);
      synchronized (lock) {
        if (isAttachedToActiveTransport()) {
          activeTransport = null;
        }
      }
      loadBalancer.handleTransportShutdown(addressGroup, s);
    }

    @Override
    public void transportTerminated() {
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is terminated",
            new Object[] {getLogId(), transport.getLogId(), address});
      }
      super.transportTerminated();
      Preconditions.checkState(!isAttachedToActiveTransport(),
          "Listener is still attached to activeTransport. "
          + "Seems transportTerminated was not called.");
    }
  }

  interface Callback {
    void onTerminated();
  }
}
