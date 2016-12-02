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

import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ConnectivityState;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Transports for a single {@link SocketAddress}.
 */
@ThreadSafe
final class TransportSet extends ManagedChannel implements WithLogId {
  private static final Logger log = Logger.getLogger(TransportSet.class.getName());
  private static final ClientTransport SHUTDOWN_TRANSPORT =
      new FailingClientTransport(Status.UNAVAILABLE.withDescription("TransportSet is shutdown"));

  private final CountDownLatch terminatedLatch = new CountDownLatch(1);
  private final Object lock = new Object();
  private final LogId logId = LogId.allocate(getClass().getName());
  private final EquivalentAddressGroup addressGroup;
  private final String authority;
  private final String userAgent;
  private final BackoffPolicy.Provider backoffPolicyProvider;
  private final Callback callback;
  private final ClientTransportFactory transportFactory;
  private final ScheduledExecutorService scheduledExecutor;
  private final Executor appExecutor;

  @GuardedBy("lock")
  private int nextAddressIndex;

  /**
   * The policy to control back off between reconnects. Non-{@code null} when last connect failed.
   */
  @GuardedBy("lock")
  private BackoffPolicy reconnectPolicy;

  /**
   * Timer monitoring duration since entering CONNECTING state.
   */
  @GuardedBy("lock")
  private final Stopwatch connectingTimer;

  @GuardedBy("lock")
  @Nullable
  private ScheduledFuture<?> reconnectTask;

  /**
   * All transports that are not terminated. At the very least the value of {@link #activeTransport}
   * will be present, but previously used transports that still have streams or are stopping may
   * also be present.
   */
  @GuardedBy("lock")
  private final Collection<ManagedClientTransport> transports =
      new ArrayList<ManagedClientTransport>();

  private final InUseStateAggregator<ManagedClientTransport> inUseStateAggregator =
      new InUseStateAggregator<ManagedClientTransport>() {
        @Override
        Object getLock() {
          return lock;
        }

        @Override
        void handleInUse() {
          callback.onInUse(TransportSet.this);
        }

        @Override
        void handleNotInUse() {
          callback.onNotInUse(TransportSet.this);
        }
      };

  /**
   * The to-be active transport, which is not ready yet.
   */
  @GuardedBy("lock")
  @Nullable
  private ConnectionClientTransport pendingTransport;

  private final LoadBalancer<ClientTransport> loadBalancer;

  @GuardedBy("lock")
  private boolean shutdown;

  /**
   * The transport for new outgoing requests. 'lock' must be held when assigning to it.
   *
   * <pre><code>
   * State             Value
   * -----             ------
   * IDLE              null, shutdown == false
   * CONNECTING        instanceof DelayedTransport, reconnectTask == null
   * READY             connected transport
   * TRANSIENT_FAILURE instanceof DelayedTransport, reconnectTask != null
   * SHUTDOWN          null, shutdown == true
   * </code></pre>
   */
  @Nullable
  private volatile ManagedClientTransport activeTransport;

  @GuardedBy("lock")
  private final ConnectivityStateManager stateManager =
      new ConnectivityStateManager(ConnectivityState.IDLE);

  TransportSet(EquivalentAddressGroup addressGroup, String authority, String userAgent,
      LoadBalancer<ClientTransport> loadBalancer, BackoffPolicy.Provider backoffPolicyProvider,
      ClientTransportFactory transportFactory, ScheduledExecutorService scheduledExecutor,
      Supplier<Stopwatch> stopwatchSupplier, Executor appExecutor, Callback callback) {
    this.addressGroup = Preconditions.checkNotNull(addressGroup, "addressGroup");
    this.authority = authority;
    this.userAgent = userAgent;
    this.loadBalancer = loadBalancer;
    this.backoffPolicyProvider = backoffPolicyProvider;
    this.transportFactory = transportFactory;
    this.scheduledExecutor = scheduledExecutor;
    this.connectingTimer = stopwatchSupplier.get();
    this.appExecutor = appExecutor;
    this.callback = callback;
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
    Runnable runnable;
    synchronized (lock) {
      // Check again, since it could have changed before acquiring the lock
      savedTransport = activeTransport;
      if (savedTransport != null) {
        return savedTransport;
      }
      if (shutdown) {
        return SHUTDOWN_TRANSPORT;
      }
      stateManager.gotoState(ConnectivityState.CONNECTING);
      DelayedClientTransport delayedTransport = new DelayedClientTransport(appExecutor);
      transports.add(delayedTransport);
      delayedTransport.start(new BaseTransportListener(delayedTransport));
      savedTransport = activeTransport = delayedTransport;
      runnable = startNewTransport(delayedTransport);
    }
    if (runnable != null) {
      runnable.run();
    }
    return savedTransport;
  }

  @CheckReturnValue
  @GuardedBy("lock")
  private Runnable startNewTransport(DelayedClientTransport delayedTransport) {
    Preconditions.checkState(reconnectTask == null, "Should have no reconnectTask scheduled");

    if (nextAddressIndex == 0) {
      connectingTimer.reset().start();
    }
    List<SocketAddress> addrs = addressGroup.getAddresses();
    final SocketAddress address = addrs.get(nextAddressIndex++);
    if (nextAddressIndex >= addrs.size()) {
      nextAddressIndex = 0;
    }

    ConnectionClientTransport transport =
        transportFactory.newClientTransport(address, authority, userAgent);
    if (log.isLoggable(Level.FINE)) {
      log.log(Level.FINE, "[{0}] Created {1} for {2}",
          new Object[] {getLogId(), transport.getLogId(), address});
    }
    pendingTransport = transport;
    transports.add(transport);
    return transport.start(new TransportListener(transport, delayedTransport, address));
  }

  /**
   * Only called after all addresses attempted and failed (TRANSIENT_FAILURE).
   * @param status the causal status when the channel begins transition to
   *     TRANSIENT_FAILURE.
   */
  private void scheduleBackoff(
      final DelayedClientTransport delayedTransport, final Status status) {
    // This must be run outside of lock. The TransportSet lock is a channel level lock.
    // startBackoff() will acquire the delayed transport lock, which is a transport level
    // lock. Our lock ordering mandates transport lock > channel lock.  Otherwise a deadlock
    // could happen (https://github.com/grpc/grpc-java/issues/2152).
    delayedTransport.startBackoff(status);

    class EndOfCurrentBackoff implements Runnable {
      @Override
      public void run() {
        try {
          delayedTransport.endBackoff();
          Runnable runnable = null;
          synchronized (lock) {
            reconnectTask = null;
            if (!shutdown) {
              stateManager.gotoState(ConnectivityState.CONNECTING);
            }
            runnable = startNewTransport(delayedTransport);
          }
          if (runnable != null) {
            runnable.run();
          }
        } catch (Throwable t) {
          log.log(Level.WARNING, "Exception handling end of backoff", t);
        }
      }
    }

    synchronized (lock) {
      if (shutdown) {
        return;
      }
      stateManager.gotoState(ConnectivityState.TRANSIENT_FAILURE);
      if (reconnectPolicy == null) {
        reconnectPolicy = backoffPolicyProvider.get();
      }
      long delayMillis =
          reconnectPolicy.nextBackoffMillis() - connectingTimer.elapsed(TimeUnit.MILLISECONDS);
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] Scheduling backoff for {1} ms",
            new Object[]{getLogId(), delayMillis});
      }
      Preconditions.checkState(reconnectTask == null, "previous reconnectTask is not done");
      reconnectTask = scheduledExecutor.schedule(
          new LogExceptionRunnable(new EndOfCurrentBackoff()),
          delayMillis,
          TimeUnit.MILLISECONDS);
    }
  }

  @Override
  public ManagedChannel shutdown() {
    ManagedClientTransport savedActiveTransport;
    ConnectionClientTransport savedPendingTransport;
    boolean runCallback = false;
    synchronized (lock) {
      if (shutdown) {
        return this;
      }
      stateManager.gotoState(ConnectivityState.SHUTDOWN);
      shutdown = true;
      savedActiveTransport = activeTransport;
      savedPendingTransport = pendingTransport;
      activeTransport = null;
      if (transports.isEmpty()) {
        runCallback = true;
        terminatedLatch.countDown();
        if (log.isLoggable(Level.FINE)) {
          log.log(Level.FINE, "[{0}] Terminated in shutdown()", getLogId());
        }
        Preconditions.checkState(reconnectTask == null, "Should have no reconnectTask scheduled");
      }  // else: the callback will be run once all transports have been terminated
    }
    if (savedActiveTransport != null) {
      savedActiveTransport.shutdown();
    }
    if (savedPendingTransport != null) {
      savedPendingTransport.shutdown();
    }
    if (runCallback) {
      callback.onTerminated(this);
    }
    return this;
  }

  void shutdownNow(Status reason) {
    shutdown();
    Collection<ManagedClientTransport> transportsCopy;
    synchronized (lock) {
      transportsCopy = new ArrayList<ManagedClientTransport>(transports);
    }
    for (ManagedClientTransport transport : transportsCopy) {
      transport.shutdownNow(reason);
    }
  }

  @Override
  public ManagedChannel shutdownNow() {
    shutdownNow(Status.UNAVAILABLE.withDescription("TransportSet shutdown as ManagedChannel"));
    return this;
  }

  @GuardedBy("lock")
  private void cancelReconnectTask() {
    if (reconnectTask != null) {
      reconnectTask.cancel(false);
      reconnectTask = null;
    }
  }

  @Override
  public LogId getLogId() {
    return logId;
  }

  @Override
  public final <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
      MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions) {
    return new ClientCallImpl<RequestT, ResponseT>(methodDescriptor,
        new SerializingExecutor(appExecutor), callOptions, StatsTraceContext.NOOP,
        new ClientTransportProvider() {
          @Override
          public ClientTransport get(CallOptions callOptions) {
            return obtainActiveTransport();
          }
        },
        scheduledExecutor);
  }

  @Override
  public boolean isShutdown() {
    synchronized (lock) {
      return shutdown;
    }
  }

  @Override
  public boolean isTerminated() {
    return terminatedLatch.getCount() == 0;
  }

  @Override
  public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException {
    return terminatedLatch.await(timeout, unit);
  }

  @Override
  public String authority() {
    return authority;
  }

  @Override
  public ConnectivityState getState(boolean requestConnection) {
    if (requestConnection) {
      boolean connect = false;
      synchronized (lock) {
        connect = stateManager.getState() == ConnectivityState.IDLE;
      }
      if (connect) {
        obtainActiveTransport();
      }
    }
    synchronized (lock) {
      return stateManager.getState();
    }
  }

  @Override
  public void notifyWhenStateChanged(ConnectivityState source, Runnable callback) {
    synchronized (lock) {
      stateManager.notifyWhenStateChanged(callback, appExecutor, source);
    }
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
    public void transportInUse(boolean inUse) {
      inUseStateAggregator.updateObjectInUse(transport, inUse);
    }

    @Override
    public void transportShutdown(Status status) {}

    @Override
    public void transportTerminated() {
      boolean runCallback = false;
      inUseStateAggregator.updateObjectInUse(transport, false);
      synchronized (lock) {
        transports.remove(transport);
        if (shutdown && transports.isEmpty()) {
          if (log.isLoggable(Level.FINE)) {
            log.log(Level.FINE, "[{0}] Terminated in transportTerminated()", getLogId());
          }
          terminatedLatch.countDown();
          runCallback = true;
          cancelReconnectTask();
        }
      }
      if (runCallback) {
        callback.onTerminated(TransportSet.this);
      }
    }
  }

  /** Listener for real transports. */
  private class TransportListener extends BaseTransportListener {
    private final SocketAddress address;
    private final DelayedClientTransport delayedTransport;

    public TransportListener(ManagedClientTransport transport,
        DelayedClientTransport delayedTransport, SocketAddress address) {
      super(transport);
      this.address = address;
      this.delayedTransport = delayedTransport;
    }

    @Override
    public void transportReady() {
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is ready",
            new Object[] {getLogId(), transport.getLogId(), address});
      }
      super.transportReady();
      boolean savedShutdown;
      synchronized (lock) {
        savedShutdown = shutdown;
        reconnectPolicy = null;
        nextAddressIndex = 0;
        if (shutdown) {
          // If TransportSet already shutdown, transport is only to take care of pending
          // streams in delayedTransport, but will not serve new streams, and it will be shutdown
          // as soon as it's set to the delayedTransport.
          // activeTransport should have already been set to null by shutdown(). We keep it null.
          Preconditions.checkState(activeTransport == null,
              "Unexpected non-null activeTransport");
        } else if (activeTransport == delayedTransport) {
          stateManager.gotoState(ConnectivityState.READY);
          Preconditions.checkState(pendingTransport == transport, "transport mismatch");
          activeTransport = transport;
          pendingTransport = null;
        }
      }
      delayedTransport.setTransport(transport);
      // This delayed transport will terminate and be removed from transports.
      delayedTransport.shutdown();
      if (savedShutdown) {
        // See comments in the synchronized block above on why we shutdown here.
        transport.shutdown();
      }
      loadBalancer.handleTransportReady(addressGroup);
    }

    @Override
    public void transportShutdown(Status s) {
      boolean allAddressesFailed = false;
      boolean closedByServer = false;
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is being shutdown with status {3}",
            new Object[] {getLogId(), transport.getLogId(), address, s});
      }
      super.transportShutdown(s);
      Runnable runnable = null;
      synchronized (lock) {
        if (activeTransport == transport) {
          // This is true only if the transport was ready.
          // shutdown() should have set activeTransport to null
          Preconditions.checkState(!shutdown, "unexpected shutdown state");
          stateManager.gotoState(ConnectivityState.IDLE);
          activeTransport = null;
          closedByServer = true;
        } else if (activeTransport == delayedTransport) {
          // shutdown() should have set activeTransport to null
          Preconditions.checkState(!shutdown, "unexpected shutdown state");
          // Continue reconnect if there are still addresses to try.
          if (nextAddressIndex == 0) {
            allAddressesFailed = true;
          } else {
            Preconditions.checkState(stateManager.getState() == ConnectivityState.CONNECTING,
                "Expected state is CONNECTING, actual state is %s", stateManager.getState());
            runnable = startNewTransport(delayedTransport);
          }
        }
      }
      if (allAddressesFailed) {
        // Initiate backoff
        // Transition to TRANSIENT_FAILURE
        scheduleBackoff(delayedTransport, s);
      }
      if (runnable != null) {
        runnable.run();
      }
      loadBalancer.handleTransportShutdown(addressGroup, s);
      if (allAddressesFailed) {
        callback.onAllAddressesFailed();
      }
      if (closedByServer) {
        callback.onConnectionClosedByServer(s);
      }
    }

    @Override
    public void transportTerminated() {
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is terminated",
            new Object[] {getLogId(), transport.getLogId(), address});
      }
      super.transportTerminated();
      Preconditions.checkState(activeTransport != transport,
          "activeTransport still points to the delayedTransport. "
          + "Seems transportShutdown() was not called.");
    }
  }

  abstract static class Callback {
    /**
     * Called when the TransportSet is terminated, which means it's shut down and all transports
     * have been terminated.
     */
    public void onTerminated(TransportSet ts) { }

    /**
     * Called when all addresses have failed to connect.
     */
    public void onAllAddressesFailed() { }

    /**
     * Called when a once-live connection is shut down by server-side.
     */
    public void onConnectionClosedByServer(Status status) { }

    /**
     * Called when the TransportSet's in-use state has changed to true, which means at least one
     * transport is in use. This method is called under a lock thus externally synchronized.
     */
    public void onInUse(TransportSet ts) { }

    /**
     * Called when the TransportSet's in-use state has changed to false, which means no transport is
     * in use. This method is called under a lock thus externally synchronized.
     */
    public void onNotInUse(TransportSet ts) { }
  }
}
